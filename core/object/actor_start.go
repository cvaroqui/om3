package object

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/opensvc/om3/v3/core/actioncontext"
	"github.com/opensvc/om3/v3/core/resource"
	"github.com/opensvc/om3/v3/core/resourceselector"
	"github.com/opensvc/om3/v3/util/file"
)

// Start starts the local instance of the object
func (t *actor) Start(ctx context.Context) error {
	ctx = actioncontext.WithProps(ctx, actioncontext.Start)
	if err := t.validateAction(); err != nil {
		return err
	}
	t.setenv("start", false)
	unlock, err := t.lockAction(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	return t.lockedStart(ctx)
}

func (t *actor) lockedStart(ctx context.Context) error {
	if err := file.Touch(t.lastStartFile(), time.Now()); err != nil {
		return err
	}
	// A start seeds the keys missing a source names, but generates none:
	// the resource reading a key generated fails on its absence.
	if err := t.seedKeySources(ctx, false); err != nil {
		return err
	}
	if err := t.checkIncompleteHeads(ctx); err != nil {
		return err
	}
	return t.action(ctx, resource.Start)
}

// checkIncompleteHeads refuses a start of resources using the heads while a
// head is marked incomplete: a restore in place emptied it and did not
// finish, so it holds a part of a run. A start of the heads alone is let
// through, a restore needing them up to write into them.
//
// A forced start takes the heads as they are, and clears their marks: the
// operator says they are fit to run on, as after fixing them by hand.
func (t *actor) checkIncompleteHeads(ctx context.Context) error {
	type marked struct {
		rid, varDir, reason string
	}
	l := make([]marked, 0)
	for _, r := range t.Resources() {
		if reason, ok := resource.HeadIncomplete(r.VarDir()); ok {
			l = append(l, marked{rid: r.RID(), varDir: r.VarDir(), reason: reason})
		}
	}
	if len(l) == 0 {
		return nil
	}
	// The resources the start acts on, to its barrier.
	selected := resourceselector.FromContext(ctx, t).Resources()
	barrier := selected.Barrier(actioncontext.To(ctx))
	selected = slices.Clone(selected)
	selected.Sort()
	// A barrier linked to a resource, as an ip to the container whose
	// network it is in, is started after it: the start goes to the later
	// of the two.
	last := selected.Index(barrier)
	if last >= 0 {
		if linkToer, ok := selected[last].(resource.LinkToer); ok && linkToer.LinkTo() != "" {
			if to, ok := selected.ResolveLink(linkToer.LinkTo()); ok {
				last = max(last, selected.Index(to.RID()))
			}
		}
	}
	consumers := make([]string, 0)
	for i, r := range selected {
		if barrier != "" && last >= 0 && i > last {
			break
		}
		if slices.Contains(restoreConsumerGroups, r.ID().DriverGroup()) && !r.IsDisabled() {
			consumers = append(consumers, r.RID())
		}
	}
	if len(consumers) == 0 {
		return nil
	}
	if actioncontext.IsForce(ctx) {
		for _, m := range l {
			t.log.Attr("rid", m.rid).Warnf("%s: start on an incomplete head, as forced, and take it as complete: %s", m.rid, m.reason)
			if err := resource.ClearHeadIncomplete(m.varDir); err != nil {
				return err
			}
		}
		return nil
	}
	rids := make([]string, len(l))
	for i, m := range l {
		rids[i] = m.rid
	}
	return fmt.Errorf("%s: incomplete head: %s: restore it with 'om %s sync restore', or start on it as it is with --force", strings.Join(rids, " "), l[0].reason, t.path)
}

func (t *actor) lastStartFile() string {
	return filepath.Join(t.varDir(), "last_start")
}
