package ressyncstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opensvc/om3/v3/core/backup"
	"github.com/opensvc/om3/v3/core/resource"
	"github.com/opensvc/om3/v3/core/status"
	"github.com/opensvc/om3/v3/core/xconfig"
	"github.com/opensvc/om3/v3/drivers/storetar"
	"github.com/opensvc/om3/v3/util/plog"
)

const (
	prodID = "prod-id"
	drpID  = "drp-id"
	testOb = "oid"
)

// runsOnly is a store keeping no documents, as one of a backup server.
type runsOnly struct {
	backup.Store
}

func newTarStore(t *testing.T) *storetar.T {
	t.Helper()
	return newTarStoreAt(t, t.TempDir())
}

func newTarStoreAt(t *testing.T, dir string) *storetar.T {
	t.Helper()
	cfg, err := xconfig.NewObject("", []byte("[store#t1]\ntype = tar\nurl = file://"+dir+"\n"))
	require.NoError(t, err)
	drv := storetar.New()
	drv.SetName("t1")
	drv.SetConfig(cfg)
	return drv.(*storetar.T)
}

// writeRun writes a completed run of the test object of the cluster
// clusterID, holding config as its configuration.
func writeRun(t *testing.T, s *storetar.T, clusterID, id string, createdAt time.Time, config string) backup.Run {
	t.Helper()
	ctx := context.Background()
	run := backup.Run{ID: id, Path: "ns1/svc/svc1", ObjectID: testOb, ClusterID: clusterID, ClusterName: strings.TrimSuffix(clusterID, "-id"), Node: "n1", CreatedAt: createdAt}
	require.NoError(t, s.BackupStream(ctx, run, backup.MemberConfig, strings.NewReader(config)))
	require.NoError(t, s.Complete(ctx, run))
	runs, err := s.Runs(ctx, backup.RunFilter{ID: id, Cluster: clusterID})
	require.NoError(t, err)
	require.Len(t, runs, 1)
	return runs[0]
}

func newTestResource() *T {
	r := &T{}
	r.SetLoggerForTest(plog.NewDefaultLogger())
	return r
}

func validConfig() (bool, []string) { return true, nil }

// A restore of a run of another cluster writes the receipt of this cluster
// beside the runs of the source, saying the run restored, the latest run the
// source completed, the digest of the configuration restored and what the
// restore came to. A restore of a run of this cluster writes none.
func TestAcknowledge(t *testing.T) {
	ctx := context.Background()
	s := newTarStore(t)
	r := newTestResource()
	t0 := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	config1 := "[DEFAULT]\nid = oid\n\n[fs#1]\ntype = flag\n"
	config2 := "[DEFAULT]\nid = oid\n\n[fs#1]\ntype = flag\n\n[fs#2]\ntype = flag\n"
	run1 := writeRun(t, s, prodID, "run1", t0, config1)
	run2 := writeRun(t, s, prodID, "run2", t0.Add(time.Hour), config2)
	// A run of the replica cluster for the same object id, which is not a
	// run of the source: the receipt does not say it seen.
	writeRun(t, s, drpID, "run3", t0.Add(2*time.Hour), config2)

	// The source cluster restores its own run.
	r.acknowledgeAs(ctx, s, run2, prodID, "prod", validConfig, true, nil)
	receipts, err := backup.ReadReceipts(ctx, s, prodID, testOb)
	require.NoError(t, err)
	assert.Empty(t, receipts, "no receipt of a restore of a run of the cluster")
	receipts, err = backup.ReadReceipts(ctx, s, drpID, testOb)
	require.NoError(t, err)
	assert.Empty(t, receipts)

	// The replica cluster restores the latest run of the source.
	r.acknowledgeAs(ctx, s, run2, drpID, "drp", validConfig, true, nil)
	receipts, err = backup.ReadReceipts(ctx, s, prodID, testOb)
	require.NoError(t, err)
	require.Len(t, receipts, 1, "under the cluster and the object of the source")
	got := receipts[0]
	digest2, err := backup.ConfigDigest([]byte(config2))
	require.NoError(t, err)
	assert.Equal(t, drpID, got.ReplicaClusterID)
	assert.Equal(t, "drp", got.ReplicaClusterName)
	assert.NotEmpty(t, got.Node)
	assert.Equal(t, uint64(1), got.Seq)
	assert.Equal(t, "run2", got.RunID)
	assert.Equal(t, "run2", got.SeenRunID)
	assert.Equal(t, digest2, got.ConfigDigest)
	assert.True(t, got.ConfigValid)
	assert.Empty(t, got.ConfigErrors)
	assert.False(t, got.Staged)
	assert.Empty(t, got.RestoreError)
	assert.False(t, got.WrittenAt.IsZero())
	receipts, err = backup.ReadReceipts(ctx, s, drpID, testOb)
	require.NoError(t, err)
	assert.Empty(t, receipts, "none under the cluster restoring")

	// It restores an older run, and fails, with a configuration that does
	// not validate there: the receipt replaces the former, and says the
	// run the source completed last as seen.
	r.acknowledgeAs(ctx, s, run1, drpID, "drp", func() (bool, []string) { return false, []string{"fs#1.type: no such driver"} }, true, errors.New("restore fs#1: no space left"))
	receipts, err = backup.ReadReceipts(ctx, s, prodID, testOb)
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	got = receipts[0]
	digest1, err := backup.ConfigDigest([]byte(config1))
	require.NoError(t, err)
	assert.Equal(t, uint64(2), got.Seq)
	assert.Equal(t, "run1", got.RunID)
	assert.Equal(t, "run2", got.SeenRunID)
	assert.Equal(t, digest1, got.ConfigDigest)
	assert.False(t, got.ConfigValid)
	assert.Equal(t, []string{"fs#1.type: no such driver"}, got.ConfigErrors)
	assert.Equal(t, "restore fs#1: no space left", got.RestoreError)

	// An interrupted restore still writes its receipt.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	r.acknowledgeAs(cancelled, s, run2, drpID, "drp", validConfig, true, cancelled.Err())
	receipts, err = backup.ReadReceipts(ctx, s, prodID, testOb)
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	assert.Equal(t, uint64(3), receipts[0].Seq)
	assert.Equal(t, "context canceled", receipts[0].RestoreError)

	// The runs of the source are the ones they were: a receipt is not one.
	runs, err := s.Runs(ctx, backup.RunFilter{Cluster: prodID})
	require.NoError(t, err)
	assert.Len(t, runs, 2)
}

// A store keeping no documents takes no receipt, and the restore is not
// failed for it.
func TestAcknowledgeWithoutDocs(t *testing.T) {
	ctx := context.Background()
	s := newTarStore(t)
	r := newTestResource()
	run := writeRun(t, s, prodID, "run1", time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC), "[DEFAULT]\n")
	r.acknowledgeAs(ctx, runsOnly{s}, run, drpID, "drp", validConfig, true, nil)
	receipts, err := backup.ReadReceipts(ctx, s, prodID, testOb)
	require.NoError(t, err)
	assert.Empty(t, receipts)
	written, err := backup.Acknowledge(ctx, runsOnly{s}, run, backup.Receipt{ReplicaClusterID: drpID})
	assert.False(t, written)
	assert.ErrorIs(t, err, backup.ErrNoDocs)
}

// The status of a resource whose store keeps no documents says nothing of
// replicas: the store can hold no receipt, and an object with no replica is
// not one to warn of.
func TestStatusReplicasWithoutDocs(t *testing.T) {
	r := newTestResource()
	st := r.statusReplicas(context.Background(), runsOnly{newTarStore(t)})
	assert.Equal(t, status.NotApplicable, st)
	assert.Empty(t, r.Replicas())
	assert.Empty(t, r.StatusLog().Entries())
}

// A restore refused before it touched a head leaves the receipt of the
// replica as it was: the instance still holds the run of its last receipt,
// which the source keeps judging it on. A restore failing once a head is
// emptied writes a receipt saying so.
func TestRefusedRestoreKeepsTheReceipt(t *testing.T) {
	ctx := context.Background()
	storeDir := t.TempDir()
	s := newTarStoreAt(t, storeDir)
	r := newTestResource()
	t0 := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)

	src := t.TempDir()
	write := func(id string, createdAt time.Time, content string) backup.Run {
		require.NoError(t, os.WriteFile(filepath.Join(src, "data"), []byte(content), 0600))
		run := backup.Run{ID: id, Path: "ns1/svc/svc1", ObjectID: testOb, ClusterID: prodID, ClusterName: "prod", Node: "n1", CreatedAt: createdAt}
		require.NoError(t, s.Backup(ctx, run, "fs#1", src, src, nil))
		require.NoError(t, s.BackupStream(ctx, run, backup.MemberConfig, strings.NewReader("[DEFAULT]\n")))
		require.NoError(t, s.Complete(ctx, run))
		runs, err := s.Runs(ctx, backup.RunFilter{ID: id})
		require.NoError(t, err)
		require.Len(t, runs, 1)
		return runs[0]
	}
	run1 := write("run1", t0, "one")
	run2 := write("run2", t0.Add(time.Hour), "two")

	base := t.TempDir()
	headDir := filepath.Join(base, "head")
	require.NoError(t, os.MkdirAll(headDir, 0755))
	require.NoError(t, os.Symlink(headDir, filepath.Join(base, "link")))
	target := head{rid: "fs#1", dir: headDir}
	receipt := func() backup.Receipt {
		t.Helper()
		l, err := backup.ReadReceipts(ctx, s, prodID, testOb)
		require.NoError(t, err)
		require.Len(t, l, 1)
		return l[0]
	}
	data := func() string {
		b, _ := os.ReadFile(filepath.Join(headDir, "data"))
		return string(b)
	}

	// A restore done.
	touched, err := restoreUnder(ctx, s, run1, target, target, nil)
	require.NoError(t, err)
	assert.True(t, touched)
	r.acknowledgeAs(ctx, s, run1, drpID, "drp", validConfig, touched, err)
	first := receipt()
	assert.Equal(t, uint64(1), first.Seq)
	assert.Equal(t, "run1", first.RunID)
	assert.Equal(t, "one", data())

	// A restore of the next run refused: the head is reached through a
	// link, which is not followed.
	linked := head{rid: "fs#1", dir: filepath.Join(base, "link")}
	touched, err = restoreUnder(ctx, s, run2, linked, linked, nil)
	require.Error(t, err)
	assert.False(t, touched)
	r.acknowledgeAs(ctx, s, run2, drpID, "drp", validConfig, touched, err)
	assert.Equal(t, first, receipt(), "the receipt of the run the instance still holds")
	assert.Equal(t, "one", data(), "the head as it was")

	// A restore of the next run failing once the head is emptied: its
	// member can not be read from the store.
	member := filepath.Join(storeDir, prodID, testOb, "run2", "fs#1.tar.gz")
	require.FileExists(t, member)
	require.NoError(t, os.WriteFile(member, []byte("not a gzip"), 0600))
	touched, err = restoreUnder(ctx, s, run2, target, target, nil)
	require.Error(t, err)
	assert.True(t, touched)
	assert.Empty(t, data(), "the head emptied")
	r.acknowledgeAs(ctx, s, run2, drpID, "drp", validConfig, touched, err)
	failed := receipt()
	assert.Equal(t, uint64(2), failed.Seq)
	assert.Equal(t, "run2", failed.RunID)
	assert.Equal(t, err.Error(), failed.RestoreError)

	// A refusal after it leaves the receipt of the failure.
	r.acknowledgeAs(ctx, s, run2, drpID, "drp", validConfig, false, errors.New("app#1 up"))
	assert.Equal(t, failed, receipt())
}

// The verdict on a replica, from its receipt and the runs of this cluster.
func TestJudgeReplicas(t *testing.T) {
	const maxDelay = 2 * time.Hour
	t0 := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	completed := func(id string, createdAt time.Time) backup.Run {
		return backup.Run{ID: id, ClusterID: prodID, ObjectID: testOb, CreatedAt: createdAt, CompletedAt: createdAt.Add(time.Minute), Members: []backup.Member{{Name: backup.MemberConfig, Kind: backup.MemberStream}}}
	}
	// run1 at t0, run2 an hour later, run3 an hour later again.
	runs := []backup.Run{completed("run1", t0), completed("run2", t0.Add(time.Hour)), completed("run3", t0.Add(2*time.Hour))}
	digests := map[string]string{"run1": "d1", "run2": "d1", "run3": "d3"}
	digestOf := func(run backup.Run) (string, error) { return digests[run.ID], nil }
	delay := func(time.Time) time.Duration { return maxDelay }
	receipt := func(runID, seenRunID, digest string) backup.Receipt {
		return backup.Receipt{ReplicaClusterID: drpID, ReplicaClusterName: "drp", RunID: runID, SeenRunID: seenRunID, ConfigDigest: digest, ConfigValid: true}
	}
	// Half an hour after run3 began.
	now := t0.Add(2*time.Hour + 30*time.Minute)

	cases := []struct {
		name     string
		receipt  backup.Receipt
		runs     []backup.Run
		now      time.Time
		delay    func(time.Time) time.Duration
		want     resource.ReplicaStatus
		warns    []string
		changeAt []time.Time
	}{
		{
			name:     "healthy",
			receipt:  receipt("run3", "run3", "d3"),
			want:     resource.ReplicaStatus{RunID: "run3", SeenRunID: "run3", ConfigInSync: true, ConfigValid: true, RPOBreachedAt: t0.Add(2*time.Hour + maxDelay)},
			changeAt: []time.Time{t0.Add(2*time.Hour + maxDelay)},
		},
		{
			name:     "healthy, restoring the run before the latest it has seen, of the same configuration",
			receipt:  receipt("run2", "run3", "d3"),
			want:     resource.ReplicaStatus{RunID: "run2", SeenRunID: "run3", ConfigInSync: true, ConfigValid: true, RPOBreachedAt: t0.Add(time.Hour + maxDelay)},
			changeAt: []time.Time{t0.Add(time.Hour + maxDelay)},
		},
		{
			name:    "lagging: the run restored is older than the max delay",
			receipt: receipt("run1", "run3", "d3"),
			want:    resource.ReplicaStatus{RunID: "run1", SeenRunID: "run3", ConfigInSync: true, ConfigValid: true, RPOBreachedAt: t0.Add(maxDelay)},
			warns:   []string{"replica drp: holds run run1, begun at 2026-10-20T02:00:00Z, more than 2h0m0s ago"},
		},
		{
			name:     "not seeing the latest run yet, completed less than the max delay ago",
			receipt:  receipt("run2", "run2", "d1"),
			want:     resource.ReplicaStatus{RunID: "run2", SeenRunID: "run2", ConfigInSync: true, ConfigValid: true, RPOBreachedAt: t0.Add(time.Hour + maxDelay)},
			changeAt: []time.Time{t0.Add(time.Hour + maxDelay), t0.Add(2*time.Hour + time.Minute + maxDelay)},
		},
		{
			name:    "no longer reading the store: the latest run completed more than the max delay ago is not seen",
			receipt: receipt("run2", "run2", "d1"),
			now:     t0.Add(5 * time.Hour),
			want:    resource.ReplicaStatus{RunID: "run2", SeenRunID: "run2", ConfigInSync: true, ConfigValid: true, RPOBreachedAt: t0.Add(time.Hour + maxDelay)},
			warns: []string{
				"replica drp: holds run run2, begun at 2026-10-20T03:00:00Z, more than 2h0m0s ago",
				"replica drp: has not seen run run3, completed at 2026-10-20T04:01:00Z, more than 2h0m0s ago: it no longer reads the store",
			},
		},
		{
			name:     "the configuration restored is not the one of the latest run seen",
			receipt:  receipt("run2", "run3", "d1"),
			want:     resource.ReplicaStatus{RunID: "run2", SeenRunID: "run3", ConfigValid: true, RPOBreachedAt: t0.Add(time.Hour + maxDelay)},
			warns:    []string{"replica drp: its configuration is not the one of run run3, the latest it listed"},
			changeAt: []time.Time{t0.Add(time.Hour + maxDelay)},
		},
		{
			name: "the configuration does not validate on the replica",
			receipt: func() backup.Receipt {
				r := receipt("run3", "run3", "d3")
				r.ConfigValid, r.ConfigErrors = false, []string{"fs#1.type: no such driver", "nodes: empty"}
				return r
			}(),
			want:     resource.ReplicaStatus{RunID: "run3", SeenRunID: "run3", ConfigInSync: true, ConfigErrors: []string{"fs#1.type: no such driver", "nodes: empty"}, RPOBreachedAt: t0.Add(2*time.Hour + maxDelay)},
			warns:    []string{"replica drp: its configuration does not validate there: fs#1.type: no such driver; nodes: empty"},
			changeAt: []time.Time{t0.Add(2*time.Hour + maxDelay)},
		},
		{
			name:    "a run this cluster does not hold, as one its retention dropped",
			receipt: receipt("run0", "run0", "d0"),
			want:    resource.ReplicaStatus{RunID: "run0", SeenRunID: "run0", ConfigValid: true, RPOBreachedAt: time.Unix(0, 0)},
			warns:   []string{"replica drp: restored the unknown run run0, not a run of the object this cluster holds, as one its retention dropped"},
			changeAt: []time.Time{
				// run3 is not seen, and late past this.
				t0.Add(2*time.Hour + time.Minute + maxDelay),
			},
		},
		{
			name: "a failed restore: the replica holds no run to date",
			receipt: func() backup.Receipt {
				r := receipt("run3", "run3", "d3")
				r.RestoreError = "restore fs#1: no space left"
				return r
			}(),
			want:  resource.ReplicaStatus{RunID: "run3", SeenRunID: "run3", ConfigInSync: true, ConfigValid: true, RPOBreachedAt: time.Unix(0, 0), RestoreError: "restore fs#1: no space left"},
			warns: []string{"replica drp: the restore of run run3 failed: restore fs#1: no space left"},
		},
		{
			name:    "no max delay and no schedule: nothing says a replica late",
			receipt: receipt("run1", "run1", "d1"),
			now:     t0.Add(1000 * time.Hour),
			delay:   func(time.Time) time.Duration { return 0 },
			want:    resource.ReplicaStatus{RunID: "run1", SeenRunID: "run1", ConfigInSync: true, ConfigValid: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.runs == nil {
				tc.runs = runs
			}
			if tc.now.IsZero() {
				tc.now = now
			}
			if tc.delay == nil {
				tc.delay = delay
			}
			tc.want.Cluster, tc.want.ClusterID = "drp", drpID
			v := judgeReplicas(tc.now, []backup.Receipt{tc.receipt}, tc.runs, tc.delay, digestOf)
			require.Len(t, v.replicas, 1)
			assert.Equal(t, tc.want, v.replicas[0])
			assert.Equal(t, tc.warns, v.warns)
			assert.Equal(t, tc.changeAt, v.changes)

			// The time the replica says it wrote the receipt at is
			// on its clock, which is not this cluster's: whatever
			// it says, the verdict is the same.
			for _, writtenAt := range []time.Time{{}, tc.now.Add(-100 * 365 * 24 * time.Hour), tc.now.Add(100 * 365 * 24 * time.Hour)} {
				skewed := tc.receipt
				skewed.WrittenAt = writtenAt
				assert.Equal(t, v, judgeReplicas(tc.now, []backup.Receipt{skewed}, tc.runs, tc.delay, digestOf), "written at %s", writtenAt)
			}
		})
	}

	// No receipt: no replica, and nothing to warn of.
	v := judgeReplicas(now, nil, runs, delay, digestOf)
	assert.Empty(t, v.replicas)
	assert.Empty(t, v.warns)
	assert.Empty(t, v.changes)

	// Each replica is judged on its own.
	other := receipt("run1", "run3", "d3")
	other.ReplicaClusterID, other.ReplicaClusterName = "drp2-id", ""
	v = judgeReplicas(now, []backup.Receipt{receipt("run3", "run3", "d3"), other}, runs, delay, digestOf)
	require.Len(t, v.replicas, 2)
	require.Len(t, v.warns, 1)
	assert.Contains(t, v.warns[0], "replica drp2-id: holds run run1", "a cluster with no name is told by its id")
}
