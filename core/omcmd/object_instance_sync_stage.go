package omcmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/opensvc/om3/v3/core/commoncmd"
	"github.com/opensvc/om3/v3/core/object"
	"github.com/opensvc/om3/v3/core/objectselector"
	"github.com/opensvc/om3/v3/core/resource"
)

type (
	// CmdObjectInstanceSyncStage is a step of the stage a transfer from
	// another node is written into, which the daemon of the node
	// receiving it runs as a command: the stage opened, committed or
	// discarded under the lock of the object.
	//
	// It is a process of its own because of that lock: taken in the
	// daemon, it would be released by any other handle the daemon closes
	// on its file, the locks of a file being the ones of a process.
	CmdObjectInstanceSyncStage struct {
		OptsGlobal
		RID    string
		Name   string
		Step   string
		Seeded bool
	}
)

// Run does the step on the local instance, and prints its result.
func (t *CmdObjectInstanceSyncStage) Run(kind string) error {
	mergedSelector := commoncmd.MergeSelector("", t.ObjectSelector, kind, "")
	paths, err := objectselector.New(mergedSelector, objectselector.WithLocal(true)).MustExpand()
	if err != nil {
		return err
	}
	if len(paths) != 1 {
		return fmt.Errorf("the stage of a transfer is of one object, and %s selects %d", mergedSelector, len(paths))
	}
	actor, err := object.NewActor(paths[0])
	if err != nil {
		return err
	}
	var result object.SyncStageResult
	result.SyncStage, err = actor.SyncStage(context.Background(), object.SyncStageOptions{RID: t.RID, Name: t.Name, Step: t.Step, SeededOnly: t.Seeded})
	if err != nil {
		result.Error = err.Error()
		result.Discarded = errors.Is(err, resource.ErrHeadStageDiscarded)
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
