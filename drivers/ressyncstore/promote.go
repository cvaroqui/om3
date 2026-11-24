package ressyncstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/opensvc/om3/v3/core/backup"
	"github.com/opensvc/om3/v3/core/resource"
)

// A replica takes over from its source by its promotion, which records its
// cluster in the store as the primary of the object, the one writing its
// runs from then on. The record is what keeps the two clusters from writing
// at once with no way to reach one another: the source, back from the
// disaster that took it away, reads it before each run, and writes none.
//
// The record is kept under the identity of each of the two, the cluster id
// and the object id its runs are kept under. See backup.Primary.

// errNotPrimary is returned by an update on a cluster the store records
// another primary than, which writes no run.
var errNotPrimary = errors.New("not the primary")

// processAlive says a process of the pid runs.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// loadRestoreProgress returns the progress of the restore writing the heads,
// as file tells it, and false when none does: no file, one that can not be
// read, or one a restore killed left behind, whose process alive no longer
// finds. A restore ending removes its file, one killed with no chance to
// does not.
func loadRestoreProgress(file string, alive func(pid int) bool) (RestoreProgress, bool) {
	var p RestoreProgress
	b, err := os.ReadFile(file)
	if err != nil {
		return p, false
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return p, false
	}
	if !alive(p.PID) {
		return p, false
	}
	return p, true
}

// recordedPrimary returns the primary the store s records under the object,
// as one of the cluster clusterID, and false when it records none, or keeps
// no documents: the cluster is then the primary of its own runs.
func (t *T) recordedPrimary(ctx context.Context, s backup.Store, clusterID string) (backup.Primary, bool, error) {
	a, ok := s.(backup.Annotator)
	if !ok || clusterID == "" {
		return backup.Primary{}, false, nil
	}
	return backup.ReadPrimary(ctx, a, clusterID, t.ObjectID.String())
}

// fencedBy formats the refusal of a run by a cluster the store records the
// primary p instead of.
func (t *T) fencedBy(p backup.Primary, s backup.Store) string {
	name := p.ClusterName
	if name == "" {
		name = p.ClusterID
	}
	return fmt.Sprintf("the cluster %s is the primary of the object since its promotion, epoch %d: no run from this cluster, whose object follows it again once made its replica with 'om %s replica --of %s'", name, p.Epoch, t.Path, s.Ref())
}

// promotedHere says the store records the cluster clusterID as the primary
// taking over from the source src: the replica was promoted, and no longer
// follows, its configuration saying so or not yet. A record that can not be
// read is not one.
func promotedHere(ctx context.Context, s backup.Store, src backup.ReplicaSource, clusterID string) bool {
	a, ok := s.(backup.Annotator)
	if !ok {
		return false
	}
	p, found, err := backup.ReadPrimary(ctx, a, src.ClusterID, src.ObjectID)
	return err == nil && found && p.Names(clusterID)
}

func primaryRecord(p backup.Primary) *resource.PrimaryRecord {
	return &resource.PrimaryRecord{
		ClusterID:       p.ClusterID,
		ClusterName:     p.ClusterName,
		ObjectID:        p.ObjectID,
		Epoch:           p.Epoch,
		PromotedFromRun: p.PromotedFromRun,
		PromotedAt:      p.PromotedAt,
	}
}

// PromotionState implements resource.Promoter: the run of the source the
// heads hold, the restore writing them now, and the primary the store
// records for the source.
func (t *T) PromotionState(ctx context.Context) (resource.PromotionState, bool, error) {
	src, ok, err := t.follows()
	if err != nil || !ok {
		return resource.PromotionState{}, false, err
	}
	s, err := t.store()
	if err != nil {
		return resource.PromotionState{}, true, err
	}
	defer s.Close()
	state, err := t.promotionState(ctx, s, src, filepath.Join(t.VarDir(), followName), filepath.Join(t.VarDir(), progressName), processAlive)
	return state, true, err
}

func (t *T) promotionState(ctx context.Context, s backup.Store, src backup.ReplicaSource, followFile, progressFile string, alive func(int) bool) (resource.PromotionState, error) {
	state := resource.PromotionState{
		Store:           s.Ref(),
		SourceClusterID: src.ClusterID,
		SourceObjectID:  src.ObjectID,
	}
	if t.PromoteWait != nil {
		state.PromoteWait = *t.PromoteWait
	}
	rec := loadFollowRecord(followFile)
	state.HeldRunID, state.HeldRunCreatedAt, state.HeldRunSeenAt = rec.RunID, rec.RunCreatedAt, rec.RunSeenAt
	if p, ok := loadRestoreProgress(progressFile, alive); ok {
		state.Restore = &resource.RestoreInFlight{
			RunID:      p.RunID,
			SessionID:  p.SessionID,
			PID:        p.PID,
			StartedAt:  p.StartedAt,
			UpdatedAt:  p.UpdatedAt,
			BytesTotal: p.BytesTotal,
			BytesDone:  p.BytesDone,
		}
	}
	a, ok := s.(backup.Annotator)
	if !ok {
		// Nothing would tell the source it was taken over from, which
		// would write its runs again once back.
		return state, fmt.Errorf("store %s: %w, where a promotion records the primary of the object", s.Ref(), backup.ErrNoDocs)
	}
	p, found, err := backup.ReadPrimary(ctx, a, src.ClusterID, src.ObjectID)
	if err != nil {
		return state, err
	}
	if found {
		state.Primary = primaryRecord(p)
	}
	return state, nil
}

// RecordPrimary implements resource.Promoter: the cluster of the node
// recorded in the store as the primary of the object, taking over from the
// source the resource follows.
func (t *T) RecordPrimary(ctx context.Context, fromRunID string) (resource.PrimaryRecord, error) {
	src, ok, err := t.follows()
	switch {
	case err != nil:
		return resource.PrimaryRecord{}, err
	case !ok:
		return resource.PrimaryRecord{}, fmt.Errorf("%s follows no source to take over from", t.RID())
	}
	s, err := t.store()
	if err != nil {
		return resource.PrimaryRecord{}, err
	}
	defer s.Close()
	clusterID, clusterName := t.cluster()
	return t.recordPrimary(ctx, s, src, clusterID, clusterName, fromRunID)
}

func (t *T) recordPrimary(ctx context.Context, s backup.Store, src backup.ReplicaSource, clusterID, clusterName, fromRunID string) (resource.PrimaryRecord, error) {
	a, ok := s.(backup.Annotator)
	if !ok {
		return resource.PrimaryRecord{}, fmt.Errorf("store %s: %w, where a promotion records the primary of the object", s.Ref(), backup.ErrNoDocs)
	}
	p, err := backup.Promote(ctx, a, src.ClusterID, src.ObjectID, backup.Primary{
		ClusterID:       clusterID,
		ClusterName:     clusterName,
		ObjectID:        t.ObjectID.String(),
		PromotedFromRun: fromRunID,
	})
	if err != nil {
		return resource.PrimaryRecord{}, fmt.Errorf("record the primary of the object in store %s: %w", s.Ref(), err)
	}
	t.Log().Infof("the cluster %s is recorded in store %s as the primary of the object, epoch %d, taking over from the object %s of the cluster %s", clusterName, s.Ref(), p.Epoch, src.ObjectID, src.ClusterID)
	return *primaryRecord(p), nil
}

// mayWriteRuns says whether the cluster clusterID writes the runs of the
// object into the store s, with why it does not: errNotPrimary.
//
// The store says which cluster writes the runs of an object two sites take
// turns running: a cluster another one was promoted over writes none, the
// run of a source back from a disaster being the one its successor would be
// told to follow.
func (t *T) mayWriteRuns(ctx context.Context, s backup.Store, clusterID string) error {
	primary, recorded, err := t.recordedPrimary(ctx, s, clusterID)
	switch {
	case err != nil:
		return fmt.Errorf("read the primary of the object in store %s: %w: no run", s.Ref(), err)
	case recorded && !primary.Names(clusterID):
		return fmt.Errorf("%w: %s", errNotPrimary, t.fencedBy(primary, s))
	}
	return nil
}

// FencedBy implements resource.Fencer: the cluster the store records as the
// primary of the object, when it is another than the one of the node. A
// replica is not asked: it follows, and starts by its promotion. A store
// keeping no documents records none.
func (t *T) FencedBy(ctx context.Context) (string, error) {
	if t.isReplica() {
		return "", nil
	}
	s, err := t.store()
	if err != nil {
		return "", err
	}
	defer s.Close()
	clusterID, _ := t.cluster()
	return t.fencedReason(ctx, s, clusterID)
}

func (t *T) fencedReason(ctx context.Context, s backup.Store, clusterID string) (string, error) {
	primary, recorded, err := t.recordedPrimary(ctx, s, clusterID)
	if err != nil || !recorded || primary.Names(clusterID) {
		return "", err
	}
	name := primary.ClusterName
	if name == "" {
		name = primary.ClusterID
	}
	return fmt.Sprintf("store %s records the cluster %s as the primary of the object since its promotion, epoch %d, where it runs: make this one its replica with 'om %s replica --of %s'", s.Ref(), name, primary.Epoch, t.Path, s.Ref()), nil
}
