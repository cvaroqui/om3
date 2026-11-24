package object

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/opensvc/om3/v3/core/actioncontext"
	"github.com/opensvc/om3/v3/core/env"
	"github.com/opensvc/om3/v3/core/instance"
	"github.com/opensvc/om3/v3/core/resource"
	"github.com/opensvc/om3/v3/core/resourceselector"
	"github.com/opensvc/om3/v3/core/status"
)

// headsFollower is an object following the heads of the runs of its source.
type headsFollower interface {
	followHeads(context.Context) error
}

// SyncFollow brings a replica to the latest run of the object it is a
// replica of: the configuration the run saved, then its heads, the instance
// left stopped.
//
// The configuration comes first, and outside the lock of the action: the
// daemon replaces it, judging it under the grants recorded when the replica
// was created, and takes the lock of the object to write it. The heads are
// then restored by the object of the configuration replaced, under the lock,
// as a restore does.
func (t *actor) SyncFollow(ctx context.Context) error {
	ctx = actioncontext.WithProps(ctx, actioncontext.SyncFollow)
	if err := t.validateAction(); err != nil {
		return err
	}
	if !t.IsReplica() {
		return fmt.Errorf("%s is not a replica, it follows no object: a replica is created with 'deploy --store <store> --snapshot <id> --replica'", t.path)
	}
	t.setenv("sync_follow", false)
	if st, err := t.FreshStatus(ctx); err != nil {
		return err
	} else if err := t.checkFollowStopped(st); err != nil {
		if errors.Is(err, errReplicaStarted) && env.HasDaemonSchedulerOrigin() {
			// A replica an operator started is started until
			// stopped: its schedule failing at every period would
			// say nothing new. Its resource warns of it.
			t.log.Infof("skip the scheduled follow: %s", err)
			return nil
		}
		return err
	}
	changed, err := t.followConfig(ctx)
	if err != nil {
		// No action ran, whose end evaluates the status: the resource
		// warns of the configuration it refused from now, not from
		// the next scheduled status.
		if _, serr := t.FreshStatus(ctx); serr != nil {
			t.log.Debugf("evaluate the status after a configuration not followed: %s", serr)
		}
		return err
	}
	if !changed {
		return t.followHeads(ctx)
	}
	// The resources restoring the heads are the ones of the
	// configuration followed, which this object was not loaded from.
	o, err := New(t.path)
	if err != nil {
		return fmt.Errorf("load the configuration followed: %w", err)
	}
	follower, ok := o.(headsFollower)
	if !ok {
		return fmt.Errorf("the configuration followed makes of %s an object following no heads", t.path)
	}
	return follower.followHeads(ctx)
}

// followers returns the selected resources following the runs of a source.
func (t *actor) followers(ctx context.Context) []resource.Follower {
	l := make([]resource.Follower, 0)
	for _, r := range resourceselector.FromContext(ctx, t).Resources() {
		if r.IsDisabled() || r.IsActionDisabled() {
			continue
		}
		if follower, ok := r.(resource.Follower); ok {
			l = append(l, follower)
		}
	}
	return l
}

// followConfig has the selected resources apply the configuration of the
// latest run of the source, and says whether one changed the configuration
// of the object.
func (t *actor) followConfig(ctx context.Context) (bool, error) {
	changed := false
	var errs error
	for _, follower := range t.followers(ctx) {
		v, err := follower.FollowConfig(ctx)
		if err != nil {
			errs = errors.Join(errs, err)
		}
		changed = changed || v
	}
	return changed, errs
}

// followHeads restores the heads from the latest run of the source, under
// the lock of the action. The instance is readied for it as a restore --stop
// does, when a resource has heads to restore: flagged stopped, the resources
// using the heads stopped, the heads started. A follow with nothing to
// restore starts nothing.
func (t *actor) followHeads(ctx context.Context) error {
	ctx = actioncontext.WithProps(ctx, actioncontext.SyncFollow)
	unlock, err := t.lockAction(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	pending := false
	for _, follower := range t.followers(ctx) {
		v, err := follower.FollowPending(ctx)
		if err != nil {
			return err
		}
		pending = pending || v
	}
	follow := func() error {
		return t.action(ctx, func(ctx context.Context, r resource.Driver) error {
			return resource.Follow(ctx, r)
		})
	}
	if !pending {
		return follow()
	}
	before, err := t.lockedStatusBeforeProvision(ctx)
	if err != nil {
		return err
	}
	if err := t.checkFollowStopped(before); err != nil {
		return err
	}
	if err := t.lockedRestoreReady(ctx); err != nil {
		return errors.Join(err, t.lockedFollowStop(ctx, before))
	}
	// A replica is stopped between two follows: the heads the follow
	// started to restore into are stopped once restored, or once the
	// restore failed, and the ones it found up are left up.
	return errors.Join(follow(), t.lockedFollowStop(ctx, before))
}

// lockedFollowStop stops the resources a follow started on an instance it
// found stopped, and leaves alone the ones that were up before it. It is a
// step of the follow, which flagged the instance stopped already.
func (t *actor) lockedFollowStop(ctx context.Context, before instance.Status) error {
	ctx = actioncontext.WithoutResourceSelector(ctx)
	ctx = actioncontext.WithProps(ctx, actioncontext.RestoreStop)
	return t.action(ctx, func(ctx context.Context, r resource.Driver) error {
		if rs, ok := before.Resources[r.RID()]; ok && rs.Status.Is(status.Up, status.StandbyUp) {
			t.log.Attr("rid", r.RID()).Tracef("%s: leave started, it was %s before the follow", r.RID(), rs.Status)
			return nil
		}
		return resource.Stop(ctx, r)
	})
}

// errReplicaStarted is the refusal of the follow of a replica started.
var errReplicaStarted = errors.New("the replica is started")

// checkFollowStopped refuses the follow of a replica whose instance, of the
// status st, runs a resource using the heads.
//
// A replica started, as forced by an operator taking over from a source
// lost, holds the data its resources write since: a follow would stop them
// and write the last run of the source over it.
//
// A standby resource is another matter: it is up on every node by design,
// which a stop leaves up, so the follow can not ready the instance. The
// designer of the object decides what is standby on the replica, with the
// standby keyword scoped for its nodes, and the refusal says so.
func (t *actor) checkFollowStopped(st instance.Status) error {
	started := make([]string, 0)
	standby := make([]string, 0)
	for _, r := range t.ResourcesByDrivergroups(restoreConsumerGroups) {
		if r.IsDisabled() {
			continue
		}
		rs, ok := st.Resources[r.RID()]
		switch {
		case !ok:
		case rs.Status.Is(status.StandbyUp), r.IsStandby() && rs.Status.Is(status.Up):
			standby = append(standby, r.RID())
		case rs.Status.Is(status.Up):
			started = append(started, r.RID())
		}
	}
	slices.Sort(started)
	slices.Sort(standby)
	switch {
	case len(started) > 0:
		return fmt.Errorf("%w, %s up: it no longer follows its source, whose runs would be written over the data it runs on: stop it with 'om %s instance stop' to follow again", errReplicaStarted, strings.Join(started, " "), t.path)
	case len(standby) > 0:
		return fmt.Errorf("%s up, standby on this node, using the data a follow would write under: a replica follows with the resources using its heads down, set 'standby@drpnodes = false' on %s in the configuration of the source", strings.Join(standby, " "), strings.Join(standby, " "))
	}
	return nil
}
