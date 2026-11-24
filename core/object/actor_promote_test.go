package object

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opensvc/om3/v3/core/naming"
	"github.com/opensvc/om3/v3/core/resource"
)

// promoteFixture is a replica asked its promotion, its follow, its lock and
// its store faked.
type promoteFixture struct {
	env   promoteEnv
	state resource.PromotionState
	now   time.Time

	// held says the process of the restore holds the lock of the
	// object, and endsAfter how many polls later it ends by itself,
	// never when zero.
	held      bool
	endsAfter int
	polls     int

	signaled []int
	locked   int
	marked   []string
	cleared  bool
	recorded []string
}

func newPromoteFixture() *promoteFixture {
	f := &promoteFixture{
		now: time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC),
		state: resource.PromotionState{
			Store:           "drp",
			SourceClusterID: "prod-id",
			SourceObjectID:  "oa",
			HeldRunID:       "run1",
		},
	}
	end := func() {
		// The follow done, or ended: its progress file gone, the
		// lock released.
		f.state.Restore = nil
		f.held = false
	}
	f.env = promoteEnv{
		clusterID: "drp-id",
		now:       func() time.Time { return f.now },
		state: func(context.Context) (resource.PromotionState, error) {
			return f.state, nil
		},
		holds: func(resource.RestoreInFlight) bool { return f.held },
		signal: func(pid int) error {
			f.signaled = append(f.signaled, pid)
			// A staged restore ended discards its stages: the
			// heads hold the run they held.
			end()
			return nil
		},
		sleep: func(context.Context) error {
			f.polls++
			f.now = f.now.Add(time.Second)
			if f.endsAfter > 0 && f.polls >= f.endsAfter {
				f.state.HeldRunID = "run2"
				end()
			}
			return nil
		},
		lock: func(context.Context) (func(), error) {
			f.locked++
			return func() {}, nil
		},
		marks: func() []string { return f.marked },
		clear: func() error {
			f.cleared = true
			f.marked = nil
			return nil
		},
		record: func(_ context.Context, fromRunID string) (resource.PrimaryRecord, error) {
			f.recorded = append(f.recorded, fromRunID)
			return resource.PrimaryRecord{ClusterID: "drp-id", Epoch: 1, PromotedFromRun: fromRunID}, nil
		},
		logf: func(string, ...any) {},
	}
	return f
}

// restoring has a follow restoring run2, done bytes of total in elapsed.
func (f *promoteFixture) restoring(done, total int64, elapsed time.Duration) {
	f.state.Restore = &resource.RestoreInFlight{
		RunID:      "run2",
		SessionID:  "session",
		PID:        4242,
		StartedAt:  f.now.Add(-elapsed),
		UpdatedAt:  f.now,
		BytesTotal: total,
		BytesDone:  done,
	}
	f.held = true
}

func (f *promoteFixture) promote(opts PromoteOptions) (PromoteReport, error) {
	return runPromote(context.Background(), f.env, opts, PromoteReport{Path: "test/svc/replica", Node: "d1"})
}

const mib = int64(1 << 20)

// The table of what a promotion makes of a restore in flight: what is left
// of it, at the throughput it had since it began, against promote_wait, and
// the options taking over.
func TestDecidePromote(t *testing.T) {
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	// 100 MiB of 200 in 100s: 1 MiB/s, 100s left.
	slow := resource.RestoreInFlight{RunID: "run2", PID: 1, StartedAt: now.Add(-100 * time.Second), BytesTotal: 200 * mib, BytesDone: 100 * mib}
	// 190 MiB of 200 in 19s: 10 MiB/s, 1s left.
	short := resource.RestoreInFlight{RunID: "run2", PID: 1, StartedAt: now.Add(-19 * time.Second), BytesTotal: 200 * mib, BytesDone: 190 * mib}
	// Nothing restored yet: nothing says what is left.
	unknown := resource.RestoreInFlight{RunID: "run2", PID: 1, StartedAt: now.Add(-time.Second), BytesTotal: 200 * mib}

	w := weighRestore(slow, now, false)
	require.True(t, w.RemainingKnown)
	assert.Equal(t, 100*time.Second, w.Remaining.Round(time.Second))
	assert.InDelta(t, float64(mib), w.BytesPerSecond, 1)
	assert.InDelta(t, 50, w.Percent(), 0.1)
	assert.False(t, weighRestore(unknown, now, false).RemainingKnown)

	cases := []struct {
		name        string
		restore     *resource.RestoreInFlight
		inPlace     bool
		promoteWait time.Duration
		opts        PromoteOptions
		action      string
		waitFor     time.Duration
		thenAbort   bool
	}{
		{name: "no restore", action: PromoteActionProceed},
		{name: "no restore, options ignored", opts: PromoteOptions{AbortRestore: true}, action: PromoteActionProceed},
		{name: "short within promote_wait", restore: &short, promoteWait: time.Minute, action: PromoteActionWait, waitFor: time.Minute, thenAbort: true},
		{name: "long beyond promote_wait", restore: &slow, promoteWait: time.Minute, action: PromoteActionAbort},
		{name: "default promote_wait aborts", restore: &short, action: PromoteActionAbort},
		{name: "unknown remaining aborts", restore: &unknown, promoteWait: time.Hour, action: PromoteActionAbort},
		{name: "abort flag over a short one", restore: &short, promoteWait: time.Minute, opts: PromoteOptions{AbortRestore: true}, action: PromoteActionAbort},
		{name: "wait flag over a long one", restore: &slow, opts: PromoteOptions{WaitRestore: true}, action: PromoteActionWait},
		{name: "bounded wait flag", restore: &slow, opts: PromoteOptions{WaitRestore: true, WaitRestoreFor: 5 * time.Minute}, action: PromoteActionWait, waitFor: 5 * time.Minute, thenAbort: true},
		{name: "in place, long", restore: &slow, inPlace: true, promoteWait: time.Minute, action: PromoteActionRefuse},
		{name: "in place, abort flag", restore: &slow, inPlace: true, opts: PromoteOptions{AbortRestore: true}, action: PromoteActionRefuse},
		{name: "in place, short", restore: &short, inPlace: true, promoteWait: time.Minute, action: PromoteActionWait, waitFor: time.Minute},
		{name: "in place, wait flag", restore: &slow, inPlace: true, opts: PromoteOptions{WaitRestore: true}, action: PromoteActionWait},
		{name: "in place, bounded wait flag", restore: &slow, inPlace: true, opts: PromoteOptions{WaitRestore: true, WaitRestoreFor: time.Minute}, action: PromoteActionWait, waitFor: time.Minute},
		{name: "in place, forced", restore: &slow, inPlace: true, opts: PromoteOptions{Force: true}, action: PromoteActionAbort},
		{name: "in place, forced bounded wait", restore: &slow, inPlace: true, opts: PromoteOptions{Force: true, WaitRestore: true, WaitRestoreFor: time.Minute}, action: PromoteActionWait, waitFor: time.Minute, thenAbort: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var r *PromoteRestore
			if tc.restore != nil {
				r = weighRestore(*tc.restore, now, tc.inPlace)
			}
			action, waitFor, thenAbort, reason, override := decidePromote(r, tc.promoteWait, tc.opts)
			assert.Equal(t, tc.action, action, reason)
			assert.Equal(t, tc.waitFor, waitFor)
			assert.Equal(t, tc.thenAbort, thenAbort)
			assert.NotEmpty(t, reason)
			if action == PromoteActionWait || action == PromoteActionAbort {
				assert.NotEmpty(t, override, "how to have it do otherwise")
			}
		})
	}
}

// With no restore in flight, a promotion takes the lock and records the
// primary, on the run the heads hold.
func TestPromoteWithNoRestore(t *testing.T) {
	f := newPromoteFixture()
	report, err := f.promote(PromoteOptions{})
	require.NoError(t, err)
	assert.Equal(t, PromoteActionProceed, report.Action)
	assert.Nil(t, report.Restore)
	assert.Empty(t, f.signaled)
	assert.Equal(t, 1, f.locked)
	assert.Equal(t, []string{"run1"}, f.recorded)
	assert.EqualValues(t, 1, report.Epoch)
	assert.Equal(t, "run1", report.FromRunID)
}

// A progress file a restore killed left behind names a process that is not
// the one holding the lock of the object, or none: it is ignored.
func TestPromoteIgnoresAStaleProgress(t *testing.T) {
	f := newPromoteFixture()
	f.restoring(10*mib, 200*mib, time.Minute)
	f.held = false
	report, err := f.promote(PromoteOptions{})
	require.NoError(t, err)
	assert.Equal(t, PromoteActionProceed, report.Action)
	assert.Nil(t, report.Restore)
	assert.Empty(t, f.signaled, "the pid of a stale file is never signaled")
	assert.Equal(t, []string{"run1"}, f.recorded)
}

// A restore with more left than promote_wait is ended at once, and the
// replica promoted on the run it held before.
func TestPromoteAbortsALongRestore(t *testing.T) {
	f := newPromoteFixture()
	f.state.PromoteWait = 30 * time.Second
	f.restoring(100*mib, 200*mib, 100*time.Second)
	report, err := f.promote(PromoteOptions{})
	require.NoError(t, err)
	assert.Equal(t, PromoteActionAbort, report.Action)
	assert.True(t, report.Aborted)
	assert.Equal(t, []int{4242}, f.signaled)
	assert.Zero(t, f.polls)
	assert.Equal(t, []string{"run1"}, f.recorded)
	require.NotNil(t, report.Restore)
	assert.Equal(t, "run2", report.Restore.RunID)
}

// A restore ending within promote_wait is waited for, and the replica
// promoted on the run it was restoring.
func TestPromoteWaitsForAShortRestore(t *testing.T) {
	f := newPromoteFixture()
	f.state.PromoteWait = 30 * time.Second
	f.restoring(190*mib, 200*mib, 19*time.Second)
	f.endsAfter = 2
	report, err := f.promote(PromoteOptions{})
	require.NoError(t, err)
	assert.Equal(t, PromoteActionWait, report.Action)
	assert.Equal(t, 30*time.Second, report.WaitFor)
	assert.False(t, report.Aborted)
	assert.Empty(t, f.signaled)
	assert.Equal(t, 2*time.Second, report.Waited)
	assert.Equal(t, []string{"run2"}, f.recorded, "promoted on the run the restore wrote")
	assert.Equal(t, "run1", report.HeldRunID, "the run held when asked is told too")
}

// A restore waited for that does not end in time is ended then.
func TestPromoteAbortsARestoreWaitedForTooLong(t *testing.T) {
	f := newPromoteFixture()
	f.restoring(100*mib, 200*mib, 100*time.Second)
	report, err := f.promote(PromoteOptions{WaitRestore: true, WaitRestoreFor: 5 * time.Second})
	require.NoError(t, err)
	assert.Equal(t, PromoteActionWait, report.Action)
	assert.True(t, report.Aborted)
	assert.Equal(t, []int{4242}, f.signaled)
	assert.Equal(t, 5*time.Second, report.Waited)
	assert.Equal(t, []string{"run1"}, f.recorded)
}

// --wait-restore with no duration waits for the end of the restore, however
// long, and --abort-restore ends one promote_wait would wait for.
func TestPromoteOptionsTakeOver(t *testing.T) {
	f := newPromoteFixture()
	f.restoring(100*mib, 200*mib, 100*time.Second)
	f.endsAfter = 500
	report, err := f.promote(PromoteOptions{WaitRestore: true})
	require.NoError(t, err)
	assert.Empty(t, f.signaled)
	assert.Equal(t, 500*time.Second, report.Waited)
	assert.Equal(t, []string{"run2"}, f.recorded)

	f = newPromoteFixture()
	f.state.PromoteWait = time.Hour
	f.restoring(190*mib, 200*mib, 19*time.Second)
	report, err = f.promote(PromoteOptions{AbortRestore: true})
	require.NoError(t, err)
	assert.True(t, report.Aborted)
	assert.Equal(t, []string{"run1"}, f.recorded)

	_, err = newPromoteFixture().promote(PromoteOptions{AbortRestore: true, WaitRestore: true})
	assert.ErrorIs(t, err, ErrPromoteRefused)
}

// A restore writing a head in place is not ended: its head would be left
// incomplete. The promotion is refused, the restore untouched and nothing
// recorded, unless told to wait for its end, or forced.
func TestPromoteDuringARestoreInPlace(t *testing.T) {
	f := newPromoteFixture()
	f.restoring(100*mib, 200*mib, 100*time.Second)
	f.marked = []string{"fs#1"}
	report, err := f.promote(PromoteOptions{})
	require.ErrorIs(t, err, ErrPromoteRefused)
	assert.ErrorContains(t, err, "--wait-restore")
	assert.ErrorContains(t, err, "--force")
	assert.Equal(t, PromoteActionRefuse, report.Action)
	assert.True(t, report.Restore.InPlace)
	assert.Empty(t, f.signaled, "the restore goes on")
	assert.Zero(t, f.locked)
	assert.Empty(t, f.recorded)

	_, err = f.promote(PromoteOptions{AbortRestore: true})
	require.ErrorIs(t, err, ErrPromoteRefused)
	assert.Empty(t, f.signaled)

	// A bounded wait it outlasts is refused too, the restore going on.
	_, err = f.promote(PromoteOptions{WaitRestore: true, WaitRestoreFor: 3 * time.Second})
	require.ErrorIs(t, err, ErrPromoteRefused)
	assert.Empty(t, f.signaled)
	assert.Empty(t, f.recorded)

	// Waited for to its end, which clears the mark.
	f = newPromoteFixture()
	f.restoring(100*mib, 200*mib, 100*time.Second)
	f.marked = []string{"fs#1"}
	f.endsAfter = 3
	end := f.env.sleep
	f.env.sleep = func(ctx context.Context) error {
		err := end(ctx)
		if f.state.Restore == nil {
			f.marked = nil
		}
		return err
	}
	_, err = f.promote(PromoteOptions{WaitRestore: true})
	require.NoError(t, err)
	assert.Empty(t, f.signaled)
	assert.Equal(t, []string{"run2"}, f.recorded)

	// Forced: the restore is ended, and the heads taken as they are.
	f = newPromoteFixture()
	f.restoring(100*mib, 200*mib, 100*time.Second)
	f.marked = []string{"fs#1"}
	f.state.HeldRunID = ""
	report, err = f.promote(PromoteOptions{Force: true})
	require.NoError(t, err)
	assert.True(t, report.Aborted)
	assert.True(t, f.cleared, "the marks are cleared, as a forced start does")
	assert.Equal(t, []string{""}, f.recorded)
}

// A head a former restore left incomplete, or heads holding no run whole,
// refuse the promotion with no restore in flight too.
func TestPromoteOnHeadsHoldingNoRun(t *testing.T) {
	f := newPromoteFixture()
	f.marked = []string{"fs#1"}
	_, err := f.promote(PromoteOptions{})
	require.ErrorIs(t, err, ErrPromoteRefused)
	assert.ErrorContains(t, err, "fs#1: incomplete head")
	assert.Empty(t, f.recorded)

	f = newPromoteFixture()
	f.state.HeldRunID = ""
	_, err = f.promote(PromoteOptions{})
	require.ErrorIs(t, err, ErrPromoteRefused)
	assert.Empty(t, f.recorded)
	_, err = f.promote(PromoteOptions{Force: true})
	require.NoError(t, err)
	assert.Equal(t, []string{""}, f.recorded)
}

// A dry run says what the promotion would do, and changes nothing: no
// signal, no lock, no record.
func TestPromoteDryRun(t *testing.T) {
	f := newPromoteFixture()
	f.state.PromoteWait = 30 * time.Second
	f.restoring(100*mib, 200*mib, 100*time.Second)
	report, err := f.promote(PromoteOptions{DryRun: true})
	require.NoError(t, err)
	assert.True(t, report.DryRun)
	assert.Equal(t, PromoteActionAbort, report.Action)
	assert.Contains(t, report.Reason, "beyond the promote_wait of 30s")
	assert.Contains(t, report.Override, "--wait-restore")
	assert.Empty(t, f.signaled)
	assert.Zero(t, f.locked)
	assert.Empty(t, f.recorded)
	assert.Zero(t, report.Epoch)

	// A refusal is told by a dry run, not returned as its failure.
	f.marked = []string{"fs#1"}
	report, err = f.promote(PromoteOptions{DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, PromoteActionRefuse, report.Action)
}

// A source another replica was promoted over is not taken over from, unless
// forced: the cluster to take over from is the one promoted.
func TestPromoteOverAnotherPromotion(t *testing.T) {
	f := newPromoteFixture()
	f.state.Primary = &resource.PrimaryRecord{ClusterID: "third-id", ClusterName: "third", Epoch: 3}
	_, err := f.promote(PromoteOptions{})
	require.ErrorIs(t, err, ErrPromoteRefused)
	assert.ErrorContains(t, err, "third")
	assert.Empty(t, f.recorded)

	// The source its own primary, as after a way back, and a promotion
	// of this cluster asked again, are let through.
	for _, id := range []string{"prod-id", "drp-id"} {
		f = newPromoteFixture()
		f.state.Primary = &resource.PrimaryRecord{ClusterID: id, Epoch: 2}
		_, err = f.promote(PromoteOptions{})
		require.NoError(t, err, id)
	}
}

// A requester gone while the promotion waits leaves everything as it was.
func TestPromoteWaitCancelled(t *testing.T) {
	f := newPromoteFixture()
	f.restoring(100*mib, 200*mib, 100*time.Second)
	f.env.sleep = func(context.Context) error { return context.Canceled }
	_, err := f.promote(PromoteOptions{WaitRestore: true})
	assert.True(t, errors.Is(err, context.Canceled))
	assert.Empty(t, f.signaled)
	assert.Empty(t, f.recorded)
}

// A node asked to settle only waits for its restore or ends it, checks its
// heads, and records no primary: the node promoting does, once every node
// of a replica keeping a copy per node settled.
func TestPromoteSettleOnly(t *testing.T) {
	f := newPromoteFixture()
	f.restoring(100*mib, 200*mib, 100*time.Second)
	report, err := f.promote(PromoteOptions{SettleOnly: true})
	require.NoError(t, err)
	assert.True(t, report.Aborted)
	assert.Equal(t, []int{4242}, f.signaled)
	assert.Equal(t, 1, f.locked, "the lock tells the follow done with the heads")
	assert.Empty(t, f.recorded)
	assert.Zero(t, report.Epoch)
	assert.Equal(t, "run1", report.FromRunID)

	f = newPromoteFixture()
	f.marked = []string{"fs#1"}
	_, err = f.promote(PromoteOptions{SettleOnly: true})
	require.ErrorIs(t, err, ErrPromoteRefused, "a node with an incomplete head refuses the promotion of the object")
	assert.True(t, PromoteRefusal(err))
}

// The wait for the restore, as an option gives it and as it is handed to
// the command settling.
func TestPromoteWaitRestoreValue(t *testing.T) {
	for _, s := range []string{"", "end", "1m30s"} {
		var opts PromoteOptions
		require.NoError(t, opts.SetWaitRestore(s), s)
		assert.Equal(t, s, opts.WaitRestoreValue())
		assert.Equal(t, s != "", opts.WaitRestore)
	}
	var opts PromoteOptions
	assert.Error(t, opts.SetWaitRestore("soon"))
	assert.Error(t, opts.SetWaitRestore("-1s"))
}

type fakeFencer struct {
	reason string
	err    error
}

func (t fakeFencer) FencedBy(context.Context) (string, error) { return t.reason, t.err }

// An object whose store records another cluster as its primary does not
// start, unless forced: it runs there. A store recording none, or this
// cluster, refuses nothing, and neither does one that does not answer.
func TestAFencedObjectDoesNotStart(t *testing.T) {
	ctx := context.Background()
	p, err := naming.ParsePath("test/svc/src")
	require.NoError(t, err)
	warned := 0
	warnf := func(string, ...any) { warned++ }
	fenced := fakeFencer{reason: "store drp records the cluster drp as the primary of the object since its promotion, epoch 1, where it runs: make this one its replica with 'om test/svc/src replica --of drp'"}

	require.NoError(t, fencedStart(ctx, p, nil, false, warnf))
	require.NoError(t, fencedStart(ctx, p, []resource.Fencer{fakeFencer{}}, false, warnf))
	assert.Zero(t, warned)

	err = fencedStart(ctx, p, []resource.Fencer{fakeFencer{}, fenced}, false, warnf)
	require.ErrorIs(t, err, ErrFenced)
	assert.ErrorContains(t, err, "the cluster drp")
	assert.ErrorContains(t, err, "replica --of drp")
	assert.ErrorContains(t, err, "--force")

	require.NoError(t, fencedStart(ctx, p, []resource.Fencer{fenced}, true, warnf), "forced, it starts beside the primary")
	assert.Equal(t, 1, warned)

	require.NoError(t, fencedStart(ctx, p, []resource.Fencer{fakeFencer{err: errors.New("no route to host")}}, false, warnf), "a store not answering keeps no object down")
	assert.Equal(t, 2, warned)
}
