package ressyncstore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opensvc/om3/v3/core/backup"
	"github.com/opensvc/om3/v3/drivers/storetar"
)

var (
	// prodOb and drpOb are the ids of the object on each of the two
	// clusters taking turns running it: each has one of its own.
	prodOb = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	drpOb  = uuid.MustParse("22222222-2222-4222-8222-222222222222")
)

// writeProgress writes the progress file a restore of the pid tells.
func writeProgress(t *testing.T, file string, pid int, done, total int64, startedAt time.Time) {
	t.Helper()
	b, err := json.Marshal(RestoreProgress{RunID: "run2", SessionID: "session", PID: pid, StartedAt: startedAt, UpdatedAt: startedAt, BytesTotal: total, BytesDone: done})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file, b, 0644))
}

// writeRunOf writes a completed run of the object objectID of the cluster
// clusterID, holding config as its configuration.
func writeRunOf(t *testing.T, s *storetar.T, clusterID string, objectID uuid.UUID, id string, createdAt time.Time, config string) backup.Run {
	t.Helper()
	ctx := context.Background()
	run := backup.Run{ID: id, Path: "ns1/svc/svc1", ObjectID: objectID.String(), ClusterID: clusterID, ClusterName: strings.TrimSuffix(clusterID, "-id"), Node: "n1", CreatedAt: createdAt}
	require.NoError(t, s.BackupStream(ctx, run, backup.MemberConfig, strings.NewReader(config)))
	require.NoError(t, s.Complete(ctx, run))
	runs, err := s.Runs(ctx, backup.RunFilter{ID: id, Cluster: clusterID})
	require.NoError(t, err)
	require.Len(t, runs, 1)
	return runs[0]
}

// The progress file tells a restore in flight while its process runs: one a
// restore killed left behind, naming a process gone, is no restore, and
// neither is a file that can not be read.
func TestLoadRestoreProgress(t *testing.T) {
	file := filepath.Join(t.TempDir(), progressName)
	alive := func(pid int) bool { return pid == 4242 }
	_, ok := loadRestoreProgress(file, alive)
	assert.False(t, ok, "no file, no restore")

	startedAt := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	writeProgress(t, file, 4242, 50, 200, startedAt)
	p, ok := loadRestoreProgress(file, alive)
	require.True(t, ok)
	assert.Equal(t, "run2", p.RunID)
	assert.EqualValues(t, 50, p.BytesDone)
	assert.True(t, p.StartedAt.Equal(startedAt))

	writeProgress(t, file, 4243, 50, 200, startedAt)
	_, ok = loadRestoreProgress(file, alive)
	assert.False(t, ok, "the file of a restore killed is ignored")

	require.NoError(t, os.WriteFile(file, []byte("{"), 0644))
	_, ok = loadRestoreProgress(file, alive)
	assert.False(t, ok)

	assert.True(t, processAlive(os.Getpid()))
	assert.False(t, processAlive(0))
}

// What a resource following says to a promotion: the run its heads hold,
// dated on both clocks, the restore in flight, its promote_wait, and the
// primary the store records for its source.
func TestPromotionState(t *testing.T) {
	ctx := context.Background()
	f := newFollowFixture(t)
	t0 := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	writeRun(t, f.s, prodID, "run1", t0, "[DEFAULT]\n")
	require.NoError(t, f.r.followHeads(ctx, f.s, f.src, f.env))
	wait := 90 * time.Second
	f.r.PromoteWait = &wait
	progressFile := filepath.Join(t.TempDir(), progressName)
	alive := func(pid int) bool { return pid == 4242 }

	state, err := f.r.promotionState(ctx, f.s, f.src, f.env.file, progressFile, alive)
	require.NoError(t, err)
	assert.Equal(t, "run1", state.HeldRunID)
	assert.True(t, state.HeldRunCreatedAt.Equal(t0), "begun on the clock of the source")
	assert.True(t, state.HeldRunSeenAt.Equal(f.now), "first listed on the clock of this node")
	assert.Equal(t, wait, state.PromoteWait)
	assert.Equal(t, prodID, state.SourceClusterID)
	assert.Nil(t, state.Restore)
	assert.Nil(t, state.Primary)

	writeProgress(t, progressFile, 4242, 50, 200, f.now)
	state, err = f.r.promotionState(ctx, f.s, f.src, f.env.file, progressFile, alive)
	require.NoError(t, err)
	require.NotNil(t, state.Restore)
	assert.Equal(t, 4242, state.Restore.PID)
	assert.Equal(t, "session", state.Restore.SessionID)

	writeProgress(t, progressFile, 1, 50, 200, f.now)
	state, err = f.r.promotionState(ctx, f.s, f.src, f.env.file, progressFile, alive)
	require.NoError(t, err)
	assert.Nil(t, state.Restore, "a stale file with a dead pid is ignored")

	// A store keeping no documents has nowhere to record the primary.
	_, err = f.r.promotionState(ctx, runsOnly{f.s}, f.src, f.env.file, progressFile, alive)
	assert.ErrorIs(t, err, backup.ErrNoDocs)
	_, err = f.r.recordPrimary(ctx, runsOnly{f.s}, f.src, drpID, "drp", "run1")
	assert.ErrorIs(t, err, backup.ErrNoDocs)
}

// Two clusters, one tar store, there and back.
//
// The prod cluster runs the object and writes its runs, the drp cluster
// follows them as a replica. The replica is promoted: the store records the
// drp cluster as the primary, under the identity of each of the two
// objects, the prod cluster writes no run from then on, and the drp cluster
// writes its own, under its own identity. The prod object is made a replica
// of the drp one, follows its runs and says so with receipts kept beside
// them, then is promoted back, one epoch later: the drp cluster is fenced
// in its turn, and the prod cluster writes runs again.
func TestTwoClustersThereAndBack(t *testing.T) {
	ctx := context.Background()
	s := newTarStore(t)
	t0 := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	config := "[DEFAULT]\nnodes = p1\ndrpnodes = d1\n"

	// The object on each cluster, with an id of its own.
	prod := newTestResource()
	prod.ObjectID = prodOb
	drp := newTestResource()
	drp.ObjectID = drpOb

	// Before any promotion: the source writes, with no record.
	require.NoError(t, prod.mayWriteRuns(ctx, s, prodID))
	writeRunOf(t, s, prodID, prodOb, "run1", t0, config)

	// The replica follows.
	follower := func(r *T, clusterID, node string, src backup.ReplicaSource) *followFixture {
		f := newFollowFixture(t)
		f.r, f.s, f.src = r, s, src
		f.env.clusterID, f.env.clusterName, f.env.node = clusterID, strings.TrimSuffix(clusterID, "-id"), node
		return f
	}
	prodSrc := backup.ReplicaSource{Store: "t1", ClusterID: prodID, ObjectID: prodOb.String()}
	drpFollow := follower(drp, drpID, "d1", prodSrc)
	require.NoError(t, drp.followHeads(ctx, s, prodSrc, drpFollow.env))
	assert.Equal(t, []string{"run1"}, drpFollow.restored)
	receipts, err := backup.ReadReceipts(ctx, s, prodID, prodOb.String())
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	assert.Equal(t, "run1", receipts[0].RunID)

	// The disaster: the replica is promoted on the run it holds.
	state, err := drp.promotionState(ctx, s, prodSrc, drpFollow.env.file, filepath.Join(t.TempDir(), progressName), processAlive)
	require.NoError(t, err)
	require.Equal(t, "run1", state.HeldRunID)
	assert.Nil(t, state.Primary)
	rec, err := drp.recordPrimary(ctx, s, prodSrc, drpID, "drp", state.HeldRunID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, rec.Epoch)
	assert.Equal(t, "run1", rec.PromotedFromRun)
	assert.Equal(t, drpOb.String(), rec.ObjectID)

	// The source, back, writes no run: by hand it is told why, on
	// schedule it is skipped.
	err = prod.mayWriteRuns(ctx, s, prodID)
	require.ErrorIs(t, err, errNotPrimary)
	assert.ErrorContains(t, err, "the cluster drp is the primary of the object")
	assert.ErrorContains(t, err, "replica --of t1")
	assert.Error(t, prod.quietly(err), "an update asked by hand says why it writes nothing")
	t.Setenv("OSVC_ACTION_ORIGIN", "daemon/scheduler")
	assert.NoError(t, prod.quietly(err), "the schedule skips it")
	t.Setenv("OSVC_ACTION_ORIGIN", "")

	// Nor does it start: the object runs on the cluster promoted, which
	// itself is told nothing of the kind.
	reason, err := prod.fencedReason(ctx, s, prodID)
	require.NoError(t, err)
	assert.Contains(t, reason, "records the cluster drp as the primary")
	assert.Contains(t, reason, "replica --of t1")
	reason, err = drp.fencedReason(ctx, s, drpID)
	require.NoError(t, err)
	assert.Empty(t, reason)
	reason, err = prod.fencedReason(ctx, runsOnly{s}, prodID)
	require.NoError(t, err)
	assert.Empty(t, reason, "a store keeping no documents fences nothing")

	// A run the source wrote before it read the record, or one it wrote
	// since by any other way, is not followed by the cluster promoted,
	// its configuration a replica's still or not.
	writeRunOf(t, s, prodID, prodOb, "run2", t0.Add(time.Hour), config)
	require.NoError(t, drp.followHeads(ctx, s, prodSrc, drpFollow.env))
	assert.Equal(t, []string{"run1"}, drpFollow.restored, "a cluster promoted follows no run")
	changed, err := drp.followConfig(ctx, s, prodSrc, drpFollow.env)
	require.NoError(t, err)
	assert.False(t, changed)

	// The cluster promoted writes the runs.
	require.NoError(t, drp.mayWriteRuns(ctx, s, drpID))
	writeRunOf(t, s, drpID, drpOb, "run3", t0.Add(2*time.Hour), config)

	// The way back: the prod object, a replica of the object the store
	// records as its primary, follows the runs of the drp cluster.
	primary, found, err := backup.ReadPrimary(ctx, s, prodID, prodOb.String())
	require.NoError(t, err)
	require.True(t, found)
	drpSrc := backup.ReplicaSource{Store: "t1", ClusterID: primary.ClusterID, ObjectID: primary.ObjectID}
	assert.Equal(t, backup.ReplicaSource{Store: "t1", ClusterID: drpID, ObjectID: drpOb.String()}, drpSrc, "the record names the object to follow")
	prodFollow := follower(prod, prodID, "p1", drpSrc)
	require.NoError(t, prod.followHeads(ctx, s, drpSrc, prodFollow.env))
	assert.Equal(t, []string{"run3"}, prodFollow.restored)
	receipts, err = backup.ReadReceipts(ctx, s, drpID, drpOb.String())
	require.NoError(t, err)
	require.Len(t, receipts, 1, "the receipt of the former source is kept beside the runs it follows")
	assert.Equal(t, prodID, receipts[0].ReplicaClusterID)
	assert.Equal(t, "run3", receipts[0].RunID)

	// Promoted back.
	state, err = prod.promotionState(ctx, s, drpSrc, prodFollow.env.file, filepath.Join(t.TempDir(), progressName), processAlive)
	require.NoError(t, err)
	require.NotNil(t, state.Primary)
	assert.Equal(t, drpID, state.Primary.ClusterID, "the source followed is its own primary")
	rec, err = prod.recordPrimary(ctx, s, drpSrc, prodID, "prod", state.HeldRunID)
	require.NoError(t, err)
	assert.EqualValues(t, 2, rec.Epoch)
	assert.Equal(t, "run3", rec.PromotedFromRun)

	require.NoError(t, prod.mayWriteRuns(ctx, s, prodID), "the prod cluster writes runs again")
	err = drp.mayWriteRuns(ctx, s, drpID)
	require.ErrorIs(t, err, errNotPrimary, "the drp cluster is fenced in its turn")
	assert.ErrorContains(t, err, "the cluster prod is the primary")
	assert.ErrorContains(t, err, "epoch 2")

	// And the drp object follows the prod one again, found the same way.
	primary, found, err = backup.ReadPrimary(ctx, s, drpID, drpOb.String())
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, prodID, primary.ClusterID)
	assert.Equal(t, prodOb.String(), primary.ObjectID)
}

// The promote_wait keyword a promotion weighs a restore against.
func TestPromoteWaitKeyword(t *testing.T) {
	found := false
	for _, kw := range kws {
		if kw.Option == "promote_wait" {
			found = true
			assert.Equal(t, "0", kw.Default)
			assert.True(t, kw.Scopable)
		}
	}
	assert.True(t, found)
}
