package omcmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/opensvc/om3/v3/core/commoncmd"
	"github.com/opensvc/om3/v3/core/naming"
	"github.com/opensvc/om3/v3/core/object"
	"github.com/opensvc/om3/v3/core/objectselector"
)

type (
	// CmdObjectInstancePromote is the part of the promotion of a replica
	// a node does by itself, which the daemon runs as a command: the
	// restore in flight settled under the lock of the object, and the
	// primary recorded in the store.
	//
	// It is a process of its own because of that lock: taken in the
	// daemon, it would be released by any other handle the daemon closes
	// on its file, the locks of a file being the ones of a process.
	CmdObjectInstancePromote struct {
		OptsGlobal
		AbortRestore bool
		WaitRestore  string
		DryRun       bool
		Force        bool
		SettleOnly   bool
	}
)

// Run promotes the local instance, and prints what it found and did, with
// why it stopped, as one json document: the daemon reads it, and goes on to
// what makes of the replica an object of the cluster, or answers the
// refusal.
func (t *CmdObjectInstancePromote) Run(kind string) error {
	opts := object.PromoteOptions{
		AbortRestore: t.AbortRestore,
		DryRun:       t.DryRun,
		Force:        t.Force,
		SettleOnly:   t.SettleOnly,
	}
	if err := opts.SetWaitRestore(t.WaitRestore); err != nil {
		return err
	}
	mergedSelector := commoncmd.MergeSelector("", t.ObjectSelector, kind, "")
	paths, err := objectselector.New(mergedSelector, objectselector.WithLocal(true)).MustExpand()
	if err != nil {
		return err
	}
	if len(paths) != 1 {
		return fmt.Errorf("the promotion of an instance is of one object, and %s selects %d", mergedSelector, len(paths))
	}
	return promoteInstance(paths[0], opts)
}

func promoteInstance(p naming.Path, opts object.PromoteOptions) error {
	actor, err := object.NewActor(p)
	if err != nil {
		return err
	}
	var result object.PromoteResult
	result.Report, err = actor.Promote(context.Background(), opts)
	if err != nil {
		result.Error = err.Error()
		result.Refused = object.PromoteRefusal(err)
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

// WaitRestoreEnd is the value of --wait-restore given with no duration.
const WaitRestoreEnd = object.WaitRestoreEnd
