package ressyncstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opensvc/om3/v3/core/backup"
	"github.com/opensvc/om3/v3/core/status"
	"github.com/opensvc/om3/v3/drivers/storetar"
)

// followFixture is a replica following the test object of the prod
// cluster, its configuration, its daemon and its heads faked.
type followFixture struct {
	r   *T
	s   *storetar.T
	src backup.ReplicaSource
	env followEnv

	// drift is what the configuration of the latest run changes, until
	// applied.
	drift []string

	// refusal is why the daemon refuses the configuration, none when it
	// applies it.
	refusal []string

	applied  int
	restored []string
	now      time.Time
}

func newFollowFixture(t *testing.T) *followFixture {
	t.Helper()
	f := &followFixture{
		r:   newTestResource(),
		s:   newTarStore(t),
		src: backup.ReplicaSource{Store: "t1", ClusterID: prodID, ObjectID: testOb},
		now: time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC),
	}
	f.env = followEnv{
		clusterID:   drpID,
		clusterName: "drp",
		node:        "d1",
		file:        filepath.Join(t.TempDir(), followName),
		now:         func() time.Time { return f.now },
		validate:    validConfig,
		drift: func(context.Context, backup.Run) ([]string, []byte, error) {
			return f.drift, []byte("[DEFAULT]\n"), nil
		},
		apply: func(context.Context, []byte) ([]string, error) {
			f.applied++
			if len(f.refusal) > 0 {
				return append([]string{}, f.refusal...), nil
			}
			f.drift = nil
			return nil, nil
		},
		restore: func(_ context.Context, run backup.Run) (bool, error) {
			f.restored = append(f.restored, run.ID)
			return true, nil
		},
	}
	return f
}

func (f *followFixture) receipt(t *testing.T) (backup.Receipt, bool) {
	t.Helper()
	l, err := backup.ReadReceipts(context.Background(), f.s, prodID, testOb)
	require.NoError(t, err)
	if len(l) == 0 {
		return backup.Receipt{}, false
	}
	require.Len(t, l, 1)
	return l[0], true
}

// A follow applies the configuration of the latest run of the source, then
// restores its heads, and says both to the source. A follow with no newer
// run does nothing: no configuration applied, no head restored, no receipt.
func TestFollowAppliesTheConfigThenRestoresAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	f := newFollowFixture(t)
	t0 := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	config := "[DEFAULT]\nid = oid\n\n[fs#1]\ntype = flag\n"
	writeRun(t, f.s, prodID, "run1", t0, config)
	digest, err := backup.ConfigDigest([]byte(config))
	require.NoError(t, err)

	f.drift = []string{"fs#1.type"}
	changed, err := f.r.followConfig(ctx, f.s, f.src, f.env)
	require.NoError(t, err)
	assert.True(t, changed, "the configuration of the run is applied")
	assert.Equal(t, 1, f.applied)
	receipt, ok := f.receipt(t)
	require.True(t, ok, "a configuration applied is said to the source")
	assert.True(t, receipt.ConfigValid)
	assert.Equal(t, digest, receipt.ConfigDigest)
	assert.Equal(t, "run1", receipt.SeenRunID)
	assert.Empty(t, receipt.RunID, "the heads hold no run yet")

	require.NoError(t, f.r.followHeads(ctx, f.s, f.src, f.env))
	assert.Equal(t, []string{"run1"}, f.restored)
	receipt, _ = f.receipt(t)
	assert.Equal(t, "run1", receipt.RunID)
	assert.Equal(t, digest, receipt.ConfigDigest)
	assert.EqualValues(t, 2, receipt.Seq)
	rec := loadFollowRecord(f.env.file)
	assert.Equal(t, "run1", rec.RunID)
	assert.True(t, rec.RunCreatedAt.Equal(t0))
	assert.True(t, rec.RunSeenAt.Equal(f.now), "the run is dated by when this node first listed it too")

	// No newer run: nothing.
	f.now = f.now.Add(time.Hour)
	changed, err = f.r.followConfig(ctx, f.s, f.src, f.env)
	require.NoError(t, err)
	assert.False(t, changed)
	require.NoError(t, f.r.followHeads(ctx, f.s, f.src, f.env))
	assert.Equal(t, 1, f.applied)
	assert.Equal(t, []string{"run1"}, f.restored)
	receipt, _ = f.receipt(t)
	assert.EqualValues(t, 2, receipt.Seq, "a follow of nothing writes no receipt")

	// A newer run of the same configuration: its heads alone.
	writeRun(t, f.s, prodID, "run2", t0.Add(24*time.Hour), config)
	changed, err = f.r.followConfig(ctx, f.s, f.src, f.env)
	require.NoError(t, err)
	assert.False(t, changed)
	require.NoError(t, f.r.followHeads(ctx, f.s, f.src, f.env))
	assert.Equal(t, 1, f.applied)
	assert.Equal(t, []string{"run1", "run2"}, f.restored)
	receipt, _ = f.receipt(t)
	assert.Equal(t, "run2", receipt.RunID)
}

// A configuration the daemon refuses, as one changing a keyword the grants
// of the replica do not allow, is not applied: the heads are not restored,
// the replica holding the run it followed before, and the source is told
// why, once.
func TestFollowRefusedConfigIsReportedAndNotRestored(t *testing.T) {
	ctx := context.Background()
	f := newFollowFixture(t)
	t0 := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	config1 := "[DEFAULT]\nid = oid\n"
	writeRun(t, f.s, prodID, "run1", t0, config1)
	_, err := f.r.followConfig(ctx, f.s, f.src, f.env)
	require.NoError(t, err)
	require.NoError(t, f.r.followHeads(ctx, f.s, f.src, f.env))
	digest1, err := backup.ConfigDigest([]byte(config1))
	require.NoError(t, err)

	writeRun(t, f.s, prodID, "run2", t0.Add(24*time.Hour), "[DEFAULT]\nid = oid\nmonitor_action = reboot\n")
	f.drift = []string{"monitor_action"}
	f.refusal = []string{"monitor_action: requires the root grant"}
	changed, err := f.r.followConfig(ctx, f.s, f.src, f.env)
	require.Error(t, err)
	assert.False(t, changed)
	assert.Contains(t, err.Error(), "requires the root grant")
	receipt, ok := f.receipt(t)
	require.True(t, ok)
	assert.False(t, receipt.ConfigValid)
	require.Len(t, receipt.ConfigErrors, 1)
	assert.Contains(t, receipt.ConfigErrors[0], "the configuration of run run2 is refused: monitor_action: requires the root grant")
	assert.Equal(t, "run1", receipt.RunID, "the replica holds the run it followed before")
	assert.Equal(t, "run2", receipt.SeenRunID)
	assert.Equal(t, digest1, receipt.ConfigDigest, "and its configuration")
	seq := receipt.Seq

	// The heads of a run whose configuration is not applied are not
	// restored.
	err = f.r.followHeads(ctx, f.s, f.src, f.env)
	require.Error(t, err)
	assert.Equal(t, []string{"run1"}, f.restored)
	assert.Equal(t, "run1", loadFollowRecord(f.env.file).RunID)

	// The same refusal is not said again.
	_, err = f.r.followConfig(ctx, f.s, f.src, f.env)
	require.Error(t, err)
	receipt, _ = f.receipt(t)
	assert.Equal(t, seq, receipt.Seq)

	// The replica warns of it too.
	rec := loadFollowRecord(f.env.file)
	latest, found, err := latestOfSource(ctx, f.s, f.src)
	require.NoError(t, err)
	require.True(t, found)
	v := judgeFollow(f.now, rec, latest, found, nil, func(time.Time) time.Duration { return 48 * time.Hour }, "max_delay")
	assert.Equal(t, status.Warn, v.state)
	require.NotEmpty(t, v.warns)
	assert.Contains(t, v.warns[len(v.warns)-1], "is refused")
}

// A restore failing once a head is touched leaves the replica holding no
// run whole, which its record and its receipt say.
func TestFollowRestoreFailing(t *testing.T) {
	ctx := context.Background()
	f := newFollowFixture(t)
	writeRun(t, f.s, prodID, "run1", f.now.Add(-time.Hour), "[DEFAULT]\n")
	f.env.restore = func(context.Context, backup.Run) (bool, error) {
		return true, errors.New("no space left")
	}
	require.Error(t, f.r.followHeads(ctx, f.s, f.src, f.env))
	assert.Empty(t, loadFollowRecord(f.env.file).RunID)
	receipt, ok := f.receipt(t)
	require.True(t, ok)
	assert.Equal(t, "no space left", receipt.RestoreError)
}

// The first follow of a replica does not restore again the snapshot its
// provision restored on this node, which the receipt of the provision says.
func TestFollowAdoptsTheRunOfTheProvision(t *testing.T) {
	ctx := context.Background()
	f := newFollowFixture(t)
	run := writeRun(t, f.s, prodID, "run1", f.now.Add(-time.Hour), "[DEFAULT]\n")
	written, err := backup.Acknowledge(ctx, f.s, run, backup.Receipt{ReplicaClusterID: drpID, Node: "d1", ConfigValid: true})
	require.NoError(t, err)
	require.True(t, written)
	require.NoError(t, f.r.followHeads(ctx, f.s, f.src, f.env))
	assert.Empty(t, f.restored)
	assert.Equal(t, "run1", loadFollowRecord(f.env.file).RunID)

	// The receipt of another node says nothing of the heads of this one.
	g := newFollowFixture(t)
	g.s = f.s
	g.env.node = "d2"
	require.NoError(t, g.r.followHeads(ctx, g.s, g.src, g.env))
	assert.Equal(t, []string{"run1"}, g.restored)
}

// A resource of an object that is not a replica follows nothing: on the
// cluster the object runs on, a follow is no restore of its own runs.
func TestFollowOnTheSourceIsNothing(t *testing.T) {
	ctx := context.Background()
	r := newTestResource()
	assert.False(t, r.isReplica())
	changed, err := r.FollowConfig(ctx)
	require.NoError(t, err)
	assert.False(t, changed)
	pending, err := r.FollowPending(ctx)
	require.NoError(t, err)
	assert.False(t, pending)
	require.NoError(t, r.Follow(ctx))
}

// The recovery point of a replica is dated by the earlier of the time the
// source began the run at and the time this node first listed it: a source
// whose clock is far ahead does not make the copy look fresh.
func TestJudgeFollow(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	day := func(time.Time) time.Duration { return 24 * time.Hour }
	none := func(time.Time) time.Duration { return 0 }
	run := func(id string) backup.Run { return backup.Run{ID: id} }

	t.Run("no run followed", func(t *testing.T) {
		v := judgeFollow(now, followRecord{}, backup.Run{}, false, nil, day, "max_delay")
		assert.Equal(t, status.Warn, v.state)
		assert.True(t, v.rpoBreach.Equal(noCopyRPOBreach))
	})
	t.Run("fresh", func(t *testing.T) {
		rec := followRecord{RunID: "run1", RunCreatedAt: now.Add(-2 * time.Hour), RunSeenAt: now.Add(-time.Hour), LatestRunID: "run1"}
		v := judgeFollow(now, rec, run("run1"), true, nil, day, "max_delay")
		assert.Equal(t, status.Up, v.state)
		assert.Empty(t, v.warns)
		assert.True(t, v.rpoBreach.Equal(now.Add(22*time.Hour)), "dated by the run, begun before it was listed")
	})
	t.Run("source clock far ahead", func(t *testing.T) {
		// The source dates its run a year ahead: this node listed
		// it two days ago, and held nothing newer since.
		rec := followRecord{RunID: "run1", RunCreatedAt: now.Add(365 * 24 * time.Hour), RunSeenAt: now.Add(-48 * time.Hour), LatestRunID: "run1"}
		v := judgeFollow(now, rec, run("run1"), true, nil, day, "max_delay")
		assert.Equal(t, status.Warn, v.state)
		assert.True(t, v.rpoBreach.Equal(now.Add(-24*time.Hour)))
	})
	t.Run("source clock far behind", func(t *testing.T) {
		rec := followRecord{RunID: "run1", RunCreatedAt: now.Add(-365 * 24 * time.Hour), RunSeenAt: now.Add(-time.Hour), LatestRunID: "run1"}
		v := judgeFollow(now, rec, run("run1"), true, nil, day, "max_delay")
		assert.Equal(t, status.Warn, v.state, "the copy is said older rather than fresher")
	})
	t.Run("newer run not followed", func(t *testing.T) {
		rec := followRecord{RunID: "run1", RunCreatedAt: now.Add(-3 * time.Hour), RunSeenAt: now.Add(-3 * time.Hour), LatestRunID: "run2", LatestSeenAt: now.Add(-25 * time.Hour)}
		v := judgeFollow(now, rec, run("run2"), true, nil, day, "max_delay")
		assert.Equal(t, status.Warn, v.state)
		require.Len(t, v.warns, 1)
		assert.Contains(t, v.warns[0], "is not followed")
		// Listed within the max delay, it is to follow, not late.
		rec.LatestSeenAt = now.Add(-time.Hour)
		v = judgeFollow(now, rec, run("run2"), true, nil, day, "max_delay")
		assert.Equal(t, status.Up, v.state)
		assert.Contains(t, v.changes, now.Add(23*time.Hour))
	})
	t.Run("started", func(t *testing.T) {
		rec := followRecord{RunID: "run1", RunCreatedAt: now.Add(-2 * time.Hour), RunSeenAt: now.Add(-time.Hour), LatestRunID: "run1"}
		v := judgeFollow(now, rec, run("run1"), true, []string{"app#1"}, day, "max_delay")
		assert.Equal(t, status.Warn, v.state)
		require.Len(t, v.warns, 1)
		assert.Contains(t, v.warns[0], "the replica is started, app#1 up: it no longer follows its source")
	})
	t.Run("no schedule and no max delay", func(t *testing.T) {
		rec := followRecord{RunID: "run1", RunCreatedAt: now.Add(-3 * time.Hour), LatestRunID: "run1"}
		v := judgeFollow(now, rec, run("run1"), true, nil, none, "")
		assert.Equal(t, status.NotApplicable, v.state)
		assert.True(t, v.rpoBreach.IsZero())
	})
}

// The schedule of the resource runs the update of its source on the cluster
// the object runs on, and the follow on a replica, which is stopped: on no
// requirement of an instance started.
func TestScheduleOptionsOfAReplica(t *testing.T) {
	r := newTestResource()
	r.UpdateRequires = "fs#1(up)"
	opts := r.scheduleOptions(false, false)
	assert.Equal(t, "update", opts.Action)
	assert.True(t, opts.RequireReplicationSource)
	assert.Equal(t, "fs#1(up)", opts.Require)

	assert.False(t, opts.RequireLeader)

	opts = r.scheduleOptions(true, false)
	assert.Equal(t, "sync_follow", opts.Action)
	assert.Equal(t, "schedule", opts.Option)
	assert.False(t, opts.RequireReplicationSource)
	assert.Empty(t, opts.Require)
	assert.True(t, opts.RequireProvisioned)
	assert.True(t, opts.RequireLeader, "the nodes of a failover replica hold one copy, which its placement leader follows")

	opts = r.scheduleOptions(true, true)
	assert.Equal(t, "sync_follow", opts.Action)
	assert.False(t, opts.RequireLeader, "each instance of a flex replica follows its copy")
}

// An object whose configuration names drpnodes expects a replica: with no
// receipt, its resource warns that none acknowledged a run. An object naming
// none stays silent.
func TestStatusReplicasExpectingAReplica(t *testing.T) {
	ctx := context.Background()
	s := newTarStore(t)

	r := newTestResource()
	assert.Equal(t, status.NotApplicable, r.statusReplicasOf(ctx, s, prodID, false))
	assert.Empty(t, r.StatusLog().Entries())

	r = newTestResource()
	assert.Equal(t, status.Warn, r.statusReplicasOf(ctx, s, prodID, true))
	entries := r.StatusLog().Entries()
	require.Len(t, entries, 1)
	assert.Contains(t, entries[0].String(), "no replica acknowledged a run")

	// A store keeping no documents can hold no receipt: it is said, and
	// is no warning.
	r = newTestResource()
	assert.Equal(t, status.NotApplicable, r.statusReplicasOf(ctx, runsOnly{s}, prodID, true))
	entries = r.StatusLog().Entries()
	require.Len(t, entries, 1)
	assert.Contains(t, entries[0].String(), "replica health unknown")

	r = newTestResource()
	assert.Equal(t, status.NotApplicable, r.statusReplicasOf(ctx, runsOnly{s}, prodID, false))
	assert.Empty(t, r.StatusLog().Entries())
}

// Each instance of a flex replica follows the copy it holds, and says so in
// a receipt of its own: the source judges each node apart, and one late is
// a warning naming it, whatever the others hold.
func TestFlexReplicaFollowsAndIsJudgedPerNode(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	d1 := newFollowFixture(t)
	d1.env.perNode = true
	d2 := newFollowFixture(t)
	d2.s = d1.s
	d2.env.perNode = true
	d2.env.node = "d2"
	config := "[DEFAULT]\nid = oid\n"
	run1 := writeRun(t, d1.s, prodID, "run1", t0, config)
	require.NoError(t, d1.r.followHeads(ctx, d1.s, d1.src, d1.env))
	require.NoError(t, d2.r.followHeads(ctx, d2.s, d2.src, d2.env))
	assert.Equal(t, []string{"run1"}, d2.restored, "the receipt of d1 says nothing of the copy of d2")

	// d1 alone follows the next run.
	run2 := writeRun(t, d1.s, prodID, "run2", t0.Add(time.Hour), config)
	require.NoError(t, d1.r.followHeads(ctx, d1.s, d1.src, d1.env))

	receipts, err := backup.ReadReceipts(ctx, d1.s, prodID, testOb)
	require.NoError(t, err)
	require.Len(t, receipts, 2)
	assert.Equal(t, "d1", receipts[0].Node)
	assert.Equal(t, "run2", receipts[0].RunID)
	assert.Equal(t, "d2", receipts[1].Node)
	assert.Equal(t, "run1", receipts[1].RunID)

	digest, err := backup.ConfigDigest([]byte(config))
	require.NoError(t, err)
	hour := func(time.Time) time.Duration { return time.Hour }
	now := t0.Add(90 * time.Minute)
	v := judgeReplicas(now, receipts, []backup.Run{run1, run2}, hour, func(backup.Run) (string, error) { return digest, nil })
	require.Len(t, v.replicas, 2, "an entry per node")
	assert.Equal(t, "d1", v.replicas[0].Node)
	assert.Equal(t, "d2", v.replicas[1].Node)
	assert.True(t, v.replicas[0].RPOBreachedAt.After(now), "d1 is fresh")
	assert.True(t, v.replicas[1].RPOBreachedAt.Before(now), "d2 is late")
	require.NotEmpty(t, v.warns, "the object is as its worst copy")
	for _, w := range v.warns {
		assert.Contains(t, w, "replica drp/d2:")
	}
}
