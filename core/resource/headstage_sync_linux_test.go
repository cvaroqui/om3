package resource

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opensvc/om3/v3/core/driver"
	"github.com/opensvc/om3/v3/util/plog"
)

type (
	// syncTestObject is an object of the resources a transfer is staged
	// for.
	syncTestObject struct {
		ObjectDriver
		resources Drivers
	}

	// syncTestHead is a resource holding a head, staged by stager when
	// it has one, the rest of Driver left to the nil interface it
	// embeds.
	syncTestHead struct {
		Driver
		rid, head, varDir string
		stager            *DirStager

		// seeded makes the stage one holding the head, as a clone.
		seeded bool
	}

	// syncTestDestination is a sync resource receiving a transfer.
	syncTestDestination struct {
		Driver
		varDir, dst, dstFS string
	}
)

func (t *syncTestObject) ResourcesByDrivergroups([]driver.Group) Drivers { return t.resources }

func (t *syncTestHead) RID() string      { return t.rid }
func (t *syncTestHead) Head() string     { return t.head }
func (t *syncTestHead) VarDir() string   { return t.varDir }
func (t *syncTestHead) IsDisabled() bool { return false }

// syncTestStagingHead is a head whose resource stages.
type syncTestStagingHead struct {
	*syncTestHead
}

func (t syncTestStagingHead) StageHead(ctx context.Context, name string) (HeadStage, bool, error) {
	stage, ok, err := t.stager.Stage(ctx, name)
	if ok && t.seeded {
		if err := os.CopyFS(stage.Dir, os.DirFS(t.head)); err != nil {
			return HeadStage{}, false, err
		}
		stage.Seeded, stage.LinkDest = true, ""
	}
	return stage, ok, err
}

func (t syncTestStagingHead) RecoverHeadStage(ctx context.Context) error {
	return t.stager.Recover(ctx)
}

func (t syncTestStagingHead) ResumeHeadStage(ctx context.Context, name string) (HeadStage, error) {
	return t.stager.Resume(ctx, name)
}

func (t *syncTestDestination) RID() string                       { return "sync#1" }
func (t *syncTestDestination) VarDir() string                    { return t.varDir }
func (t *syncTestDestination) Log() *plog.Logger                 { return plog.NewDefaultLogger() }
func (t *syncTestDestination) SyncDestination() (string, string) { return t.dst, t.dstFS }

// newSyncStageFixture returns an object of a directory head that stages, and
// of a sync resource whose destination is the head.
func newSyncStageFixture(t *testing.T) (*syncTestObject, *syncTestHead, *syncTestDestination) {
	t.Helper()
	stager, dir := newDirHead(t)
	head := &syncTestHead{rid: "fs#1", head: dir, varDir: stager.VarDir, stager: stager}
	dst := &syncTestDestination{varDir: filepath.Join(filepath.Dir(dir), "var-sync"), dst: dir + "/"}
	return &syncTestObject{resources: Drivers{syncTestStagingHead{head}}}, head, dst
}

// A transfer is written into a stage of the head holding its destination,
// held by no process, and the head is what it was until the commit, which
// switches it whole.
func TestSyncStageIsCommittedOnceWrittenWhole(t *testing.T) {
	ctx := context.Background()
	o, head, dst := newSyncStageFixture(t)
	before := inode(t, head.head)

	stage, err := OpenSyncStage(ctx, o, dst, "s1", false)
	require.NoError(t, err)
	assert.True(t, stage.Staged)
	assert.False(t, stage.Seeded)
	assert.Equal(t, "fs#1", stage.HeadRID)
	assert.Equal(t, head.head, stage.LinkDest, "the committed tree, to hardlink from")
	assert.NotEqual(t, head.head, stage.Dir)
	_, marked := HeadIncomplete(head.varDir)
	assert.False(t, marked, "a head staged is not marked incomplete")

	j, err := ReadHeadStageJournal(head.varDir)
	require.NoError(t, err)
	require.NotNil(t, j)
	assert.True(t, j.Held)
	assert.Zero(t, j.PID, "the stage is of no process of this node")
	assert.False(t, j.Busy())

	require.NoError(t, os.WriteFile(filepath.Join(stage.Dir, "data"), []byte("new"), 0600))
	assert.Equal(t, "old", content(head.head), "the head untouched while the transfer is written")
	assert.Equal(t, before, inode(t, head.head))

	require.NoError(t, CommitSyncStage(ctx, o, dst, "s1"))
	assert.Equal(t, "new", content(head.head))
	assert.NotEqual(t, before, inode(t, head.head))
	assert.Empty(t, leftovers(t, head.stager))
	assert.ErrorIs(t, CommitSyncStage(ctx, o, dst, "s1"), ErrHeadStageDiscarded, "nothing left to commit")
}

// A start of the head during a transfer waits for nobody: its recovery
// discards the stage, the head being the generation last committed, and the
// commit asked for later is told so. The discard that follows is no error.
func TestSyncStageIsDiscardedByAStartMidTransfer(t *testing.T) {
	ctx := context.Background()
	o, head, dst := newSyncStageFixture(t)
	stage, err := OpenSyncStage(ctx, o, dst, "s1", false)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(stage.Dir, "data"), []byte("half"), 0600))

	// The start of the resource holding the head.
	require.NoError(t, syncTestStagingHead{head}.RecoverHeadStage(ctx), "a stage held is not one of a process running")
	assert.Equal(t, "old", content(head.head))
	assert.Empty(t, leftovers(t, head.stager))

	assert.ErrorIs(t, CommitSyncStage(ctx, o, dst, "s1"), ErrHeadStageDiscarded)
	assert.NoError(t, DiscardSyncStage(ctx, o, dst, "s1"))
	assert.Equal(t, "old", content(head.head))
}

// The process still writing into a stage discarded, as the receiver of the
// transfer, is ended: it would write on into the tree being removed.
func TestSyncStageDiscardEndsItsWriters(t *testing.T) {
	ctx := context.Background()
	o, head, dst := newSyncStageFixture(t)
	stage, err := OpenSyncStage(ctx, o, dst, "s1", false)
	require.NoError(t, err)

	writer := exec.Command("sleep", "60")
	writer.Dir = stage.Dir
	require.NoError(t, writer.Start())
	done := make(chan error, 1)
	go func() { done <- writer.Wait() }()

	require.NoError(t, syncTestStagingHead{head}.RecoverHeadStage(ctx))
	err = <-done
	require.Error(t, err, "the writer was killed")
	assert.Contains(t, err.Error(), "killed")
	assert.Empty(t, leftovers(t, head.stager))
}

// A transfer failed or cancelled discards its stage, the head untouched,
// and the next one opens its own over a stage a source gone left.
func TestSyncStageDiscardAndReopen(t *testing.T) {
	ctx := context.Background()
	o, head, dst := newSyncStageFixture(t)
	stage, err := OpenSyncStage(ctx, o, dst, "s1", false)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(stage.Dir, "data"), []byte("half"), 0600))
	require.NoError(t, DiscardSyncStage(ctx, o, dst, "s1"))
	assert.Equal(t, "old", content(head.head))
	assert.Empty(t, leftovers(t, head.stager))

	// A source gone left its stage: the next transfer discards it.
	stage, err = OpenSyncStage(ctx, o, dst, "s2", false)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(stage.Dir, "data"), []byte("half"), 0600))
	stage, err = OpenSyncStage(ctx, o, dst, "s3", false)
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(stage.Dir, "data"))
	assert.ErrorIs(t, err, os.ErrNotExist, "a stage of its own, empty")
	assert.ErrorIs(t, CommitSyncStage(ctx, o, dst, "s2"), ErrHeadStageDiscarded, "the stage is another's")
	require.NoError(t, os.WriteFile(filepath.Join(stage.Dir, "data"), []byte("new"), 0600))
	require.NoError(t, CommitSyncStage(ctx, o, dst, "s3"))
	assert.Equal(t, "new", content(head.head))
}

// A destination that is a part of its head is staged by a stage holding the
// head, as a clone does, and written in place under a stage that is a tree
// of its own, which would switch the head to that part alone.
func TestSyncStageOfAPartOfTheHead(t *testing.T) {
	ctx := context.Background()
	o, head, dst := newSyncStageFixture(t)
	dst.dst = filepath.Join(head.head, "sub")

	stage, err := OpenSyncStage(ctx, o, dst, "s1", false)
	require.NoError(t, err)
	assert.False(t, stage.Staged)
	assert.Equal(t, dst.dst, stage.Dir, "written in place")
	assert.Empty(t, stage.LinkDest)
	assert.Empty(t, leftovers(t, head.stager), "the stage opened for nothing is discarded")
	reason, marked := HeadIncomplete(head.varDir)
	assert.True(t, marked, "a head written in place is marked incomplete meanwhile")
	assert.Contains(t, reason, "sync#1")

	// A transfer failing leaves the mark, the next one clears it once
	// written whole.
	require.NoError(t, DiscardSyncStage(ctx, o, dst, "s1"))
	_, marked = HeadIncomplete(head.varDir)
	assert.True(t, marked)
	_, err = OpenSyncStage(ctx, o, dst, "s2", false)
	require.NoError(t, err)
	assert.ErrorIs(t, CommitSyncStage(ctx, o, dst, "s1"), ErrHeadStageDiscarded, "the mark is of another transfer")
	require.NoError(t, CommitSyncStage(ctx, o, dst, "s2"))
	_, marked = HeadIncomplete(head.varDir)
	assert.False(t, marked)

	// A forced start cleared the mark, taking the head as it was: the
	// transfer is told at its commit.
	_, err = OpenSyncStage(ctx, o, dst, "s3", false)
	require.NoError(t, err)
	require.NoError(t, ClearHeadIncomplete(head.varDir))
	assert.ErrorIs(t, CommitSyncStage(ctx, o, dst, "s3"), ErrHeadStageDiscarded)

	// A transfer leaving a part of the destination as it is, the head
	// itself: no stage that is a tree of its own either.
	dst.dst = head.head
	stage, err = OpenSyncStage(ctx, o, dst, "s5", true)
	require.NoError(t, err)
	assert.False(t, stage.Staged)
	assert.Empty(t, leftovers(t, head.stager))
	require.NoError(t, CommitSyncStage(ctx, o, dst, "s5"))
	dst.dst = filepath.Join(head.head, "sub")

	// A stage seeded holds the whole head: the part is written in it,
	// as is a transfer leaving a part of it as it is.
	head.seeded = true
	stage, err = OpenSyncStage(ctx, o, dst, "s4", true)
	require.NoError(t, err)
	assert.True(t, stage.Staged)
	assert.True(t, stage.Seeded)
	assert.Empty(t, stage.LinkDest)
	assert.Equal(t, filepath.Join(head.stager.stageDir(), "sub"), stage.Dir)
	require.NoError(t, os.WriteFile(filepath.Join(stage.Dir, "data"), []byte("new"), 0600))
	require.NoError(t, CommitSyncStage(ctx, o, dst, "s4"))
	assert.Equal(t, "new", content(head.head))
}

// A destination no resource of the object holds, or whose head stages none,
// is written in place, the mark kept by the sync resource or by the head. A
// destination filesystem not mounted refuses the transfer.
func TestSyncStageOfAHeadThatStagesNone(t *testing.T) {
	ctx := context.Background()
	o, head, dst := newSyncStageFixture(t)

	// The head is held by a resource that stages none.
	o.resources = Drivers{head}
	stage, err := OpenSyncStage(ctx, o, dst, "s1", false)
	require.NoError(t, err)
	assert.False(t, stage.Staged)
	assert.Equal(t, "fs#1", stage.HeadRID)
	assert.Equal(t, head.head, stage.Dir)
	_, marked := HeadIncomplete(head.varDir)
	assert.True(t, marked)
	require.NoError(t, CommitSyncStage(ctx, o, dst, "s1"))
	_, marked = HeadIncomplete(head.varDir)
	assert.False(t, marked)

	// No resource holds the destination.
	o.resources = nil
	stage, err = OpenSyncStage(ctx, o, dst, "s2", false)
	require.NoError(t, err)
	assert.False(t, stage.Staged)
	assert.Empty(t, stage.HeadRID)
	_, marked = HeadIncomplete(dst.varDir)
	assert.True(t, marked, "the sync resource keeps the mark")
	require.NoError(t, CommitSyncStage(ctx, o, dst, "s2"))
	_, marked = HeadIncomplete(dst.varDir)
	assert.False(t, marked)

	// The destination filesystem is a directory of its parent.
	dst.dstFS = head.head
	_, err = OpenSyncStage(ctx, o, dst, "s3", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not mounted")
	_, marked = HeadIncomplete(dst.varDir)
	assert.False(t, marked, "nothing is written, nothing is marked")
	dst.dstFS = "/proc"
	_, err = OpenSyncStage(ctx, o, dst, "s3", false)
	assert.NoError(t, err)
}
