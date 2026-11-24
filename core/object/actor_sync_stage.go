package object

import (
	"context"
	"fmt"
	"time"

	"github.com/opensvc/om3/v3/core/actioncontext"
	"github.com/opensvc/om3/v3/core/resource"
)

const (
	// SyncStageOpen, SyncStageCommit and SyncStageDiscard are the steps
	// of the stage of a transfer.
	SyncStageOpen    = "open"
	SyncStageCommit  = "commit"
	SyncStageDiscard = "discard"

	// syncStageLockTimeout is how long a step of the stage of a transfer
	// waits for the lock of the object: an action holding it longer, as
	// a start, is one the transfer gives way to.
	syncStageLockTimeout = time.Minute
)

type (
	// SyncStageOptions is a step of the stage a transfer from another
	// node is written into.
	SyncStageOptions struct {
		// RID is the sync resource receiving the transfer.
		RID string

		// Name is the name of the transfer: the session sending it.
		Name string

		// Step is SyncStageOpen, SyncStageCommit or SyncStageDiscard.
		Step string

		// SeededOnly opens no stage but one holding the destination as
		// committed: the transfer leaves a part of it as it is.
		SeededOnly bool
	}

	// SyncStageResult is what a step of the stage of a transfer prints,
	// run as a command, as one json document: the daemon reads it, and
	// answers the node sending.
	SyncStageResult struct {
		resource.SyncStage

		// Error is why the step failed, and Discarded says it did
		// because the stage was discarded since it was opened.
		Error     string `json:"error,omitempty"`
		Discarded bool   `json:"discarded,omitempty"`
	}
)

// SyncStage does a step of the stage the transfer opts.Name of the sync
// resource opts.RID is written into, on this node, the one receiving it: it
// opens the stage, and returns where to write, commits it, or discards it.
//
// Each step is an action of its own, under the lock of the object for its
// time only. The transfer is written under no lock: a start of the instance
// in between discards the stage, and the commit then returns
// resource.ErrHeadStageDiscarded.
func (t *actor) SyncStage(ctx context.Context, opts SyncStageOptions) (resource.SyncStage, error) {
	var props actioncontext.Properties
	switch opts.Step {
	case SyncStageOpen:
		props = actioncontext.SyncStage
	case SyncStageCommit:
		props = actioncontext.SyncCommit
	case SyncStageDiscard:
		props = actioncontext.SyncDiscard
	default:
		return resource.SyncStage{}, fmt.Errorf("step %q: want %s, %s or %s", opts.Step, SyncStageOpen, SyncStageCommit, SyncStageDiscard)
	}
	if opts.Name == "" {
		return resource.SyncStage{}, fmt.Errorf("the stage of a transfer is named")
	}
	ctx = actioncontext.WithProps(ctx, props)
	if err := t.validateAction(); err != nil {
		return resource.SyncStage{}, err
	}
	// The resources are configured when listed.
	var r resource.Driver
	for _, candidate := range t.Resources() {
		if candidate.RID() == opts.RID {
			r = candidate
			break
		}
	}
	if r == nil {
		return resource.SyncStage{}, fmt.Errorf("%s has no resource %s", t.path, opts.RID)
	}
	if r.IsDisabled() {
		return resource.SyncStage{}, fmt.Errorf("%s is disabled", opts.RID)
	}
	if !actioncontext.IsLockTimeoutSet(ctx) {
		ctx = actioncontext.WithLockTimeout(ctx, syncStageLockTimeout)
	}
	unlock, err := t.lockAction(ctx)
	if err != nil {
		return resource.SyncStage{}, err
	}
	defer unlock()
	switch opts.Step {
	case SyncStageOpen:
		return resource.OpenSyncStage(ctx, t, r, opts.Name, opts.SeededOnly)
	case SyncStageCommit:
		return resource.SyncStage{}, resource.CommitSyncStage(ctx, t, r, opts.Name)
	default:
		return resource.SyncStage{}, resource.DiscardSyncStage(ctx, t, r, opts.Name)
	}
}
