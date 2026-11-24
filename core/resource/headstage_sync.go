package resource

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/opensvc/om3/v3/core/driver"
)

// A transfer from another node, as the one of a sync.rsync, is written into
// the destination of this node while nothing here holds a lock: a failover
// during the transfer would start the service on a tree half-copied. So the
// node sending asks this one for a stage of the head holding the destination
// before it sends, writes into the stage, and asks for its commit once the
// transfer is done. The stage is held meanwhile, by the session of the node
// sending, under no lock: a start of the head discards it, and starts on the
// generation last committed, and the commit asked for later is told so.
//
// A head with no stage is written in place, and marked incomplete for the
// time of the transfer, which a start refuses to run on.

type (
	// SyncDestination is a sync resource whose peers write into a
	// directory of this node.
	SyncDestination interface {
		// SyncDestination returns the directory the transfers are
		// written into, and the mount point it must be in, if any, as
		// configured for this node.
		SyncDestination() (dir, fs string)
	}

	// SyncStage is the stage a transfer is written into.
	SyncStage struct {
		// Staged says the transfer is written into a stage, Dir, apart
		// from the destination, which its commit switches to. The
		// transfer is else written into the destination itself, its
		// head marked incomplete until its commit.
		Staged bool `json:"staged"`

		// Dir is where the transfer is written: the destination, in
		// the stage of its head or in place.
		Dir string `json:"dir"`

		// Seeded says Dir holds the destination as committed, for a
		// transfer of the differences.
		Seeded bool `json:"seeded"`

		// LinkDest is the destination as committed, on the filesystem
		// of Dir, which is empty: a transfer hardlinks from it what
		// did not change. It is empty for a stage seeded, or none.
		LinkDest string `json:"link_dest"`

		// HeadRID is the resource holding the destination, empty when
		// none of the object does.
		HeadRID string `json:"head_rid"`
	}

	// syncHead is the resource of an object holding the destination of
	// a sync resource.
	syncHead struct {
		// r is the resource, nil when none holds the destination.
		r Driver

		// dir is its head, and dst the destination.
		dir, dst string

		// markDir is the var directory the destination is marked
		// incomplete in: the one of the resource holding it, or of the
		// sync resource.
		markDir string
	}
)

// syncHeadGroups are the driver groups whose resources hold heads.
var syncHeadGroups = []driver.Group{driver.GroupFS, driver.GroupVolume}

// syncMarkReason is why a head written in place by the transfer name of the
// resource rid is marked incomplete.
func syncMarkReason(rid, name string) string {
	return fmt.Sprintf("the transfer of %s is not finished (session %s)", rid, name)
}

// isSyncMark says reason is the mark of a transfer of the resource rid.
func isSyncMark(reason, rid string) bool {
	return strings.HasPrefix(reason, fmt.Sprintf("the transfer of %s is not finished ", rid))
}

// syncHeadOf returns the resource of the object o holding the destination of
// the sync resource r, the deepest when heads are under heads.
func syncHeadOf(o ObjectDriver, r Driver) (syncHead, error) {
	sd, ok := r.(SyncDestination)
	if !ok {
		return syncHead{}, fmt.Errorf("%s receives no transfer into a directory", r.RID())
	}
	dst, dstFS := sd.SyncDestination()
	if dst == "" {
		return syncHead{}, fmt.Errorf("%s has no destination on this node", r.RID())
	}
	dst = filepath.Clean(dst)
	if dstFS != "" {
		if root, err := IsMountRoot(dstFS); err != nil || !root {
			return syncHead{}, fmt.Errorf("the destination fs %s is not mounted. refuse to sync %s to protect parent fs", dstFS, dst)
		}
	}
	found := syncHead{dst: dst, markDir: r.VarDir()}
	for _, candidate := range o.ResourcesByDrivergroups(syncHeadGroups) {
		if candidate.IsDisabled() {
			continue
		}
		h, ok := candidate.(interface{ Head() string })
		if !ok {
			continue
		}
		dir := h.Head()
		if dir == "" {
			continue
		}
		dir = filepath.Clean(dir)
		rel, err := filepath.Rel(dir, dst)
		if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
			continue
		}
		if found.r == nil || len(dir) > len(found.dir) {
			found.r, found.dir, found.markDir = candidate, dir, candidate.VarDir()
		}
	}
	return found, nil
}

// OpenSyncStage opens the stage the transfer name is written into, for the
// sync resource r of the object o: a stage of the head holding its
// destination, held for the session to commit or discard, or the destination
// itself, marked incomplete, when its head stages none.
//
// A stage not seeded is a tree of its own, switched to whole, holding what
// the transfer wrote and nothing else. It is opened for a destination that
// is the head itself only, a transfer into a part of the head leaving the
// rest of it out of the stage, and not with seededOnly, which the node
// sending sets for a transfer leaving a part of the destination as it is,
// as one excluding files does.
//
// A head with a filesystem mounted under it stages none: the mount is not of
// its stage, and keeps the head from being switched.
func OpenSyncStage(ctx context.Context, o ObjectDriver, r Driver, name string, seededOnly bool) (SyncStage, error) {
	head, err := syncHeadOf(o, r)
	if err != nil {
		return SyncStage{}, err
	}
	answer := SyncStage{Dir: head.dst}
	if head.r != nil {
		answer.HeadRID = head.r.RID()
	}
	stager, _ := head.r.(HeadStager)
	if stager != nil {
		if mounts, err := mountsUnder(head.dir); err != nil {
			return SyncStage{}, err
		} else if len(mounts) > 0 {
			r.Log().Infof("%s is mounted under the head %s of %s: no stage", strings.Join(mounts, ", "), head.dir, head.r.RID())
			stager = nil
		}
	}
	if stager != nil {
		stage, ok, err := stager.StageHead(ctx, name)
		if err != nil {
			return SyncStage{}, fmt.Errorf("stage %s: %w", head.r.RID(), err)
		}
		if ok && !stage.Seeded && (seededOnly || head.dst != head.dir) {
			if seededOnly {
				r.Log().Infof("the transfer leaves a part of %s as it is, which the stage of %s, a tree of its own, would not hold: no stage", head.dst, head.r.RID())
			} else {
				r.Log().Infof("%s is a part of the head %s of %s, whose stage is a tree of its own: no stage", head.dst, head.dir, head.r.RID())
			}
			if err := stage.Discard(ctx); err != nil {
				return SyncStage{}, fmt.Errorf("discard the stage of %s: %w", head.r.RID(), err)
			}
			ok = false
		}
		if ok {
			if err := stage.Hold(ctx); err != nil {
				return SyncStage{}, errors.Join(err, stage.Discard(context.WithoutCancel(ctx)))
			}
			rel, _ := filepath.Rel(head.dir, head.dst)
			answer.Staged = true
			answer.Dir = filepath.Join(stage.Dir, rel)
			answer.Seeded = stage.Seeded
			if stage.LinkDest != "" {
				answer.LinkDest = filepath.Join(stage.LinkDest, rel)
			}
			return answer, nil
		}
	}
	// Marked before the first byte is written, by this transfer as the
	// reason: its commit clears the mark it finds its own.
	if err := MarkHeadIncomplete(head.markDir, syncMarkReason(r.RID(), name)); err != nil {
		return SyncStage{}, err
	}
	return answer, nil
}

// CommitSyncStage ends the transfer name of the sync resource r, written
// whole: the head is switched to its stage, or the mark of a destination
// written in place is cleared. It returns ErrHeadStageDiscarded when the
// stage was discarded since, as by a start of the head, or the mark cleared
// by a forced one: the transfer is then to do again.
func CommitSyncStage(ctx context.Context, o ObjectDriver, r Driver, name string) error {
	head, err := syncHeadOf(o, r)
	if err != nil {
		return err
	}
	reason, marked := HeadIncomplete(head.markDir)
	if stager, ok := head.r.(HeadStager); ok {
		stage, err := stager.ResumeHeadStage(ctx, name)
		switch {
		case err == nil:
			if err := stage.Commit(ctx); err != nil {
				return fmt.Errorf("commit the stage of %s: %w", head.r.RID(), err)
			}
			// The mark a former transfer of this resource left on
			// the head, written in place then, is of a tree this
			// one wrote whole over.
			if marked && isSyncMark(reason, r.RID()) && head.dst == head.dir {
				return ClearHeadIncomplete(head.markDir)
			}
			return nil
		case !errors.Is(err, ErrHeadStageDiscarded):
			return err
		}
	}
	if !marked || reason != syncMarkReason(r.RID(), name) {
		return fmt.Errorf("%s: %w", name, ErrHeadStageDiscarded)
	}
	return ClearHeadIncomplete(head.markDir)
}

// DiscardSyncStage ends the transfer name of the sync resource r, failed or
// cancelled: its stage is dropped, the head left as last committed. A
// destination written in place stays marked incomplete, holding a part of
// the transfer, until a transfer is written whole. A stage discarded already
// is no error.
func DiscardSyncStage(ctx context.Context, o ObjectDriver, r Driver, name string) error {
	head, err := syncHeadOf(o, r)
	if err != nil {
		return err
	}
	stager, ok := head.r.(HeadStager)
	if !ok {
		return nil
	}
	stage, err := stager.ResumeHeadStage(ctx, name)
	if errors.Is(err, ErrHeadStageDiscarded) {
		return nil
	}
	if err != nil {
		return err
	}
	return stage.Discard(ctx)
}
