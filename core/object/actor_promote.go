package object

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/opensvc/fcntllock"
	"github.com/opensvc/flock"

	"github.com/opensvc/om3/v3/core/actioncontext"
	"github.com/opensvc/om3/v3/core/backup"
	"github.com/opensvc/om3/v3/core/resource"
	"github.com/opensvc/om3/v3/util/hostname"
	"github.com/opensvc/om3/v3/util/xsession"
)

// The promotion of a replica makes of it the object running, its source
// lost. On the node asked, it is three steps, this file: settle the follow
// that may be restoring the heads, by waiting for it or ending it, record
// the cluster in the store as the primary of the object, so the source
// writes no run once back, and say what was done. The daemon then takes the
// keywords making it a replica away, and starts it.
//
// The restore in flight is the dilemma of a disaster: it writes a newer
// run than the one the heads hold, and the service is down until it ends.
// The promote_wait keyword of the resource following says how long the
// newer run is worth, the promotion weighs what is left of the restore on
// the throughput it had so far, and the options of the promotion take over.

const (
	// PromoteActionProceed, PromoteActionWait, PromoteActionAbort and
	// PromoteActionRefuse are what a promotion does of the restore in
	// flight: none to settle, wait for it, end it, or refuse to choose.
	PromoteActionProceed = "proceed"
	PromoteActionWait    = "wait"
	PromoteActionAbort   = "abort"
	PromoteActionRefuse  = "refuse"

	// promoteLockTimeout is how long a promotion waits for the lock of
	// the object once the restore ended or was told to: the time a
	// follow takes to discard its stages and stop the heads it started.
	promoteLockTimeout = 2 * time.Minute

	// promotePollPeriod is how often a promotion waiting for a restore
	// looks at it.
	promotePollPeriod = 500 * time.Millisecond
)

type (
	// PromoteOptions are the options of a promotion.
	PromoteOptions struct {
		// AbortRestore ends the restore in flight, whatever is left
		// of it.
		AbortRestore bool

		// WaitRestore waits for the restore in flight, WaitRestoreFor
		// at most, and for its end when zero.
		WaitRestore    bool
		WaitRestoreFor time.Duration

		// DryRun says what the promotion would do, and does nothing.
		DryRun bool

		// Force promotes a replica whose heads hold no run whole, as
		// they are.
		Force bool

		// SettleOnly settles the restore in flight and checks the
		// heads, and records no primary: what the promotion of a
		// replica with a copy per node asks of each node but the one
		// promoting.
		SettleOnly bool
	}

	// PromoteResult is what the promotion of an instance prints for the
	// daemon that ran it: what it found and did, and why it stopped,
	// refused when the reason is one of the replica and not a failure.
	PromoteResult struct {
		Report  PromoteReport `json:"report"`
		Error   string        `json:"error,omitempty"`
		Refused bool          `json:"refused,omitempty"`
	}

	// PromoteReport is what a promotion found, decided and did.
	PromoteReport struct {
		Path string `json:"path"`
		Node string `json:"node"`

		// Store, SourceClusterID and SourceObjectID are what the
		// replica followed.
		Store           string `json:"store"`
		SourceClusterID string `json:"source_cluster_id"`
		SourceObjectID  string `json:"source_object_id"`

		// HeldRunID is the run of the source the heads held when the
		// promotion was asked, begun at HeldRunCreatedAt on the clock
		// of the source, first listed at HeldRunSeenAt on the one of
		// this node.
		HeldRunID        string    `json:"held_run_id"`
		HeldRunCreatedAt time.Time `json:"held_run_created_at"`
		HeldRunSeenAt    time.Time `json:"held_run_seen_at"`

		// Restore is the restore found in flight, nil when none was.
		Restore *PromoteRestore `json:"restore"`

		// PromoteWait is the promote_wait of the resource following.
		PromoteWait time.Duration `json:"promote_wait"`

		// Action is what the promotion does of the restore, for
		// Reason, WaitFor the longest it waits, zero for its end, and
		// Override how to have it do otherwise.
		Action   string        `json:"action"`
		WaitFor  time.Duration `json:"wait_for"`
		Reason   string        `json:"reason"`
		Override string        `json:"override"`

		DryRun bool `json:"dry_run"`

		// Waited is how long the promotion waited for the restore,
		// and Aborted says it ended it.
		Waited  time.Duration `json:"waited"`
		Aborted bool          `json:"aborted"`

		// FromRunID is the run the heads hold as the replica is
		// promoted, and Epoch the count of promotions recorded.
		FromRunID string `json:"from_run_id"`
		Epoch     uint64 `json:"epoch"`
	}

	// PromoteRestore is a restore in flight, as a promotion weighs it,
	// on the clock of this node.
	PromoteRestore struct {
		RunID      string    `json:"run_id"`
		PID        int       `json:"pid"`
		StartedAt  time.Time `json:"started_at"`
		BytesTotal int64     `json:"bytes_total"`
		BytesDone  int64     `json:"bytes_done"`

		// BytesPerSecond is the throughput since StartedAt, and
		// Remaining what is left at that throughput: RemainingKnown
		// is false while nothing says, as before the first byte.
		BytesPerSecond float64       `json:"bytes_per_second"`
		Remaining      time.Duration `json:"remaining"`
		RemainingKnown bool          `json:"remaining_known"`

		// InPlace says the restore writes a head in place, marked
		// incomplete until done: ended, it leaves a head no start
		// runs on.
		InPlace bool `json:"in_place"`
	}

	// promoteEnv is what a promotion needs of the object and its node,
	// which the tests replace.
	promoteEnv struct {
		now func() time.Time

		// state returns what the resource following says.
		state func(context.Context) (resource.PromotionState, error)

		// holds says the restore is the one holding the lock of the
		// object: a progress file names a pid, which may be another
		// process by now.
		holds func(resource.RestoreInFlight) bool

		// signal tells the process of the restore to end.
		signal func(pid int) error

		// sleep waits a poll period, or the end of the context.
		sleep func(context.Context) error

		// lock takes the lock of the object, a bounded time.
		lock func(context.Context) (func(), error)

		// marks returns the resources whose head is marked
		// incomplete, and clear takes the marks away.
		marks func() []string
		clear func() error

		// record records the cluster as the primary in the store.
		record func(ctx context.Context, fromRunID string) (resource.PrimaryRecord, error)

		// clusterID is the cluster of the node.
		clusterID string

		logf func(format string, args ...any)
	}
)

var (
	// ErrNotReplica is the refusal of the promotion of an object that is
	// no replica.
	ErrNotReplica = errors.New("not a replica")

	// ErrPromoteRefused is the refusal of a promotion that would start
	// the replica on heads no run was restored whole into, or that the
	// options do not let choose.
	ErrPromoteRefused = errors.New("promotion refused")
)

// weighRestore returns the restore r as a promotion weighs it at now.
func weighRestore(r resource.RestoreInFlight, now time.Time, inPlace bool) *PromoteRestore {
	w := &PromoteRestore{
		RunID:      r.RunID,
		PID:        r.PID,
		StartedAt:  r.StartedAt,
		BytesTotal: r.BytesTotal,
		BytesDone:  r.BytesDone,
		InPlace:    inPlace,
	}
	elapsed := now.Sub(r.StartedAt)
	if elapsed <= 0 || r.BytesDone <= 0 {
		return w
	}
	w.BytesPerSecond = float64(r.BytesDone) / elapsed.Seconds()
	left := r.BytesTotal - r.BytesDone
	if left < 0 {
		left = 0
	}
	w.Remaining = time.Duration(float64(left) / w.BytesPerSecond * float64(time.Second))
	w.RemainingKnown = true
	return w
}

// Percent returns how much of the restore is done.
func (t PromoteRestore) Percent() float64 {
	if t.BytesTotal <= 0 {
		return 0
	}
	return min(100, float64(t.BytesDone)*100/float64(t.BytesTotal))
}

// decidePromote says what a promotion does of the restore r in flight, nil
// when none is: the action, how long a wait lasts at most, zero for the end
// of the restore, whether a wait timing out ends the restore, why, and how
// to have it do otherwise.
//
// A restore writing a head in place is not ended, unless forced: its head
// would be left incomplete, and the replica with nothing to start on. The
// promotion says so rather than choose between a service down for the time
// of the restore and a service lost.
func decidePromote(r *PromoteRestore, promoteWait time.Duration, opts PromoteOptions) (action string, waitFor time.Duration, thenAbort bool, reason, override string) {
	if r == nil {
		return PromoteActionProceed, 0, false, "no restore in flight", ""
	}
	mayAbort := !r.InPlace || opts.Force
	inPlace := "the restore in flight writes a head in place, which ending it would leave incomplete, with nothing to start on: --wait-restore waits for its end, --force ends it and starts on the heads as they are"
	left := "what is left of it can not be told yet"
	if r.RemainingKnown {
		left = fmt.Sprintf("what is left of it takes about %s at %s", r.Remaining.Round(time.Second), humanRate(r.BytesPerSecond))
	}
	switch {
	case opts.AbortRestore && !mayAbort:
		return PromoteActionRefuse, 0, false, inPlace, ""
	case opts.AbortRestore:
		return PromoteActionAbort, 0, false, "--abort-restore", "--wait-restore[=<duration>] waits for the restore instead"
	case opts.WaitRestore && opts.WaitRestoreFor > 0:
		return PromoteActionWait, opts.WaitRestoreFor, mayAbort, fmt.Sprintf("--wait-restore=%s", opts.WaitRestoreFor), "--abort-restore ends the restore instead"
	case opts.WaitRestore:
		return PromoteActionWait, 0, false, "--wait-restore", "--abort-restore ends the restore instead"
	case r.RemainingKnown && r.Remaining <= promoteWait:
		return PromoteActionWait, promoteWait, mayAbort, fmt.Sprintf("%s, within the promote_wait of %s", left, promoteWait), "--abort-restore ends the restore instead, --wait-restore[=<duration>] waits longer"
	case !mayAbort:
		return PromoteActionRefuse, 0, false, fmt.Sprintf("%s, beyond the promote_wait of %s: %s", left, promoteWait, inPlace), ""
	default:
		return PromoteActionAbort, 0, false, fmt.Sprintf("%s, beyond the promote_wait of %s", left, promoteWait), "--wait-restore[=<duration>] waits for the restore instead"
	}
}

// humanRate formats a throughput.
func humanRate(bytesPerSecond float64) string {
	return fmt.Sprintf("%.1f MiB/s", bytesPerSecond/(1<<20))
}

// runPromote is the promotion on a node, of what env gives it.
func runPromote(ctx context.Context, env promoteEnv, opts PromoteOptions, report PromoteReport) (PromoteReport, error) {
	report.DryRun = opts.DryRun
	if opts.AbortRestore && opts.WaitRestore {
		return report, fmt.Errorf("%w: --abort-restore and --wait-restore say opposite things", ErrPromoteRefused)
	}
	state, err := env.state(ctx)
	if err != nil {
		return report, err
	}
	report.Store = state.Store
	report.SourceClusterID, report.SourceObjectID = state.SourceClusterID, state.SourceObjectID
	report.HeldRunID, report.HeldRunCreatedAt, report.HeldRunSeenAt = state.HeldRunID, state.HeldRunCreatedAt, state.HeldRunSeenAt
	report.PromoteWait = state.PromoteWait
	if p := state.Primary; p != nil && p.ClusterID != state.SourceClusterID && p.ClusterID != env.clusterID && !opts.Force {
		// Another replica of the source was promoted: taking over
		// from the source would be taking over from a cluster that no
		// longer writes, beside the one that does.
		return report, fmt.Errorf("%w: store %s records the cluster %s as the primary of the object followed, epoch %d, not the cluster followed: follow that one with 'replica --of', or take over from it too with --force", ErrPromoteRefused, state.Store, p.ClusterName, p.Epoch)
	}
	inFlight := func(state resource.PromotionState) *resource.RestoreInFlight {
		if state.Restore == nil || !env.holds(*state.Restore) {
			return nil
		}
		return state.Restore
	}
	restore := inFlight(state)
	if restore != nil {
		report.Restore = weighRestore(*restore, env.now(), len(env.marks()) > 0)
	}
	var thenAbort bool
	report.Action, report.WaitFor, thenAbort, report.Reason, report.Override = decidePromote(report.Restore, state.PromoteWait, opts)
	if restore == nil {
		// What the lock will confirm, for a dry run to say already.
		if marked := env.marks(); len(marked) > 0 && !opts.Force {
			report.Action = PromoteActionRefuse
			report.Reason = incompleteRefusal(marked)
		} else if state.HeldRunID == "" && !opts.Force {
			report.Action = PromoteActionRefuse
			report.Reason = noRunRefusal
		}
	}
	if opts.DryRun {
		return report, nil
	}
	abort := func() error {
		env.logf("end the restore of run %s in flight, pid %d: %s", restore.RunID, restore.PID, report.Reason)
		report.Aborted = true
		return env.signal(restore.PID)
	}
	switch report.Action {
	case PromoteActionRefuse:
		return report, fmt.Errorf("%w: %s", ErrPromoteRefused, report.Reason)
	case PromoteActionAbort:
		if err := abort(); err != nil {
			return report, fmt.Errorf("end the restore in flight, pid %d: %w", restore.PID, err)
		}
	case PromoteActionWait:
		bound := "its end"
		if report.WaitFor > 0 {
			bound = report.WaitFor.String() + " at most"
		}
		env.logf("wait for the restore of run %s in flight, pid %d, %s: %s", restore.RunID, restore.PID, bound, report.Reason)
		began := env.now()
		for {
			if err := env.sleep(ctx); err != nil {
				// The requester gone, nothing was changed.
				return report, err
			}
			report.Waited = env.now().Sub(began)
			state, err := env.state(ctx)
			if err != nil {
				return report, err
			}
			if cur := inFlight(state); cur == nil || cur.PID != restore.PID {
				break
			}
			if report.WaitFor > 0 && report.Waited >= report.WaitFor {
				if !thenAbort {
					return report, fmt.Errorf("%w: the restore in flight did not end in %s, and writes a head in place, which ending it would leave incomplete: --wait-restore waits for its end, --force ends it and starts on the heads as they are", ErrPromoteRefused, report.WaitFor)
				}
				report.Reason = fmt.Sprintf("it did not end in %s", report.WaitFor)
				if err := abort(); err != nil {
					return report, fmt.Errorf("end the restore in flight, pid %d: %w", restore.PID, err)
				}
				break
			}
		}
	}
	// The lock of the object is the one the follow holds until it is
	// done with the heads, its stages discarded and what it started
	// stopped: nothing else writes them from here to the record.
	unlock, err := env.lock(ctx)
	if err != nil {
		return report, fmt.Errorf("the follow restoring the heads still holds the object: %w", err)
	}
	defer unlock()
	if state, err = env.state(ctx); err != nil {
		return report, err
	}
	if marked := env.marks(); len(marked) > 0 {
		if !opts.Force {
			return report, fmt.Errorf("%w: %s", ErrPromoteRefused, incompleteRefusal(marked))
		}
		env.logf("promote on the incomplete heads of %s, as forced, and take them as complete", strings.Join(marked, " "))
		if err := env.clear(); err != nil {
			return report, err
		}
	}
	if state.HeldRunID == "" && !opts.Force {
		return report, fmt.Errorf("%w: %s", ErrPromoteRefused, noRunRefusal)
	}
	report.FromRunID = state.HeldRunID
	if opts.SettleOnly {
		return report, nil
	}
	rec, err := env.record(ctx, state.HeldRunID)
	if err != nil {
		return report, err
	}
	report.Epoch = rec.Epoch
	return report, nil
}

// noRunRefusal is why a replica holding no run is not promoted.
const noRunRefusal = "the heads hold no run of the source restored whole: follow it again with 'sync follow', or promote on the heads as they are with --force"

// incompleteRefusal says why a replica with heads marked incomplete is not
// promoted.
func incompleteRefusal(rids []string) string {
	return fmt.Sprintf("%s: incomplete head, a restore in place emptied it and did not finish: follow the source again with 'sync follow', or promote on the heads as they are with --force", strings.Join(rids, " "))
}

// promoter returns the resource of the replica following its source.
func (t *actor) promoter(ctx context.Context) (resource.Promoter, error) {
	var found resource.Promoter
	var errs error
	for _, r := range t.Resources() {
		if r.IsDisabled() {
			continue
		}
		p, ok := r.(resource.Promoter)
		if !ok {
			continue
		}
		_, follows, err := p.PromotionState(ctx)
		switch {
		case !follows:
		case err != nil && found == nil:
			// The resource follows, and can not say what: its
			// error is the one of the promotion.
			found, errs = p, errors.Join(errs, err)
		case found == nil:
			found = p
		}
	}
	if found == nil {
		if errs != nil {
			return nil, errs
		}
		return nil, fmt.Errorf("%s has no resource following the store of its replica_of keyword, to take over through", t.path)
	}
	return found, nil
}

// incompleteHeads returns the resources whose head is marked incomplete,
// sorted.
func (t *actor) incompleteHeads() []string {
	l := make([]string, 0)
	for _, r := range t.Resources() {
		if _, ok := resource.HeadIncomplete(r.VarDir()); ok {
			l = append(l, r.RID())
		}
	}
	slices.Sort(l)
	return l
}

// Promote settles the restore a follow of the replica may be running on
// this node, as opts and the promote_wait keyword say, then records the
// cluster of the node in the store as the primary of the object. The object
// is a replica still when it returns: its configuration is the daemon's to
// change.
func (t *actor) Promote(ctx context.Context, opts PromoteOptions) (PromoteReport, error) {
	ctx = actioncontext.WithProps(ctx, actioncontext.Promote)
	report := PromoteReport{Path: t.path.String(), Node: hostname.Hostname()}
	if !t.IsReplica() {
		return report, fmt.Errorf("%w: %s follows no object to take over from", ErrNotReplica, t.path)
	}
	promoter, err := t.promoter(ctx)
	if err != nil {
		return report, err
	}
	env := promoteEnv{
		now: time.Now,
		state: func(ctx context.Context) (resource.PromotionState, error) {
			state, _, err := promoter.PromotionState(ctx)
			return state, err
		},
		holds: func(r resource.RestoreInFlight) bool {
			// Probed with a lock object of its own each time: a
			// probe finding the lock free removes its file.
			probe := flock.New(t.lockPath(actioncontext.Promote.LockGroup), xsession.SessionID().String(), fcntllock.New)
			holder, err := probe.Probe()
			return err == nil && holder.PID == r.PID && holder.SessionID == r.SessionID
		},
		signal: func(pid int) error {
			return syscall.Kill(pid, syscall.SIGTERM)
		},
		sleep: func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(promotePollPeriod):
				return nil
			}
		},
		lock: func(ctx context.Context) (func(), error) {
			return t.lockAction(actioncontext.WithLockTimeout(ctx, promoteLockTimeout))
		},
		marks: t.incompleteHeads,
		clear: func() error {
			for _, r := range t.Resources() {
				if err := resource.ClearHeadIncomplete(r.VarDir()); err != nil {
					return err
				}
			}
			return nil
		},
		record: promoter.RecordPrimary,
		logf:   t.log.Infof,
	}
	if n, err := t.Node(); err == nil {
		env.clusterID, _ = n.Cluster()
	}
	return runPromote(ctx, env, opts, report)
}

// WaitRestoreEnd is the value of a wait for the restore in flight asking
// for its end, however long.
const WaitRestoreEnd = "end"

// SetWaitRestore sets the wait for the restore in flight from s, as an
// option of the promotion gives it: empty for none, a duration, the longest
// to wait before ending the restore, or WaitRestoreEnd.
func (t *PromoteOptions) SetWaitRestore(s string) error {
	t.WaitRestore, t.WaitRestoreFor = s != "", 0
	if s == "" || s == WaitRestoreEnd {
		return nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return fmt.Errorf("wait for the restore %q: want a duration, or %s", s, WaitRestoreEnd)
	}
	t.WaitRestoreFor = d
	return nil
}

// WaitRestoreValue returns the wait for the restore in flight as
// SetWaitRestore reads it.
func (t PromoteOptions) WaitRestoreValue() string {
	switch {
	case !t.WaitRestore:
		return ""
	case t.WaitRestoreFor > 0:
		return t.WaitRestoreFor.String()
	default:
		return WaitRestoreEnd
	}
}

// PromoteRefusal says err is a refusal of the promotion, a state of the
// replica to fix or to force, and not a failure of it.
func PromoteRefusal(err error) bool {
	return errors.Is(err, ErrNotReplica) || errors.Is(err, ErrPromoteRefused) || errors.Is(err, backup.ErrNoDocs)
}
