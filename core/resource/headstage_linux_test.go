package resource

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opensvc/om3/v3/util/plog"
)

// A head is marked incomplete with the reason it is, until cleared.
func TestHeadIncompleteMark(t *testing.T) {
	varDir := filepath.Join(t.TempDir(), "fs#1")
	_, ok := HeadIncomplete(varDir)
	assert.False(t, ok, "no var directory, no mark")
	require.NoError(t, ClearHeadIncomplete(varDir), "nothing to clear")

	require.NoError(t, MarkHeadIncomplete(varDir, "emptied by run abc"))
	reason, ok := HeadIncomplete(varDir)
	assert.True(t, ok)
	assert.Equal(t, "emptied by run abc", reason)

	require.NoError(t, ClearHeadIncomplete(varDir))
	_, ok = HeadIncomplete(varDir)
	assert.False(t, ok)
}

// The journal of a stage is read back as written, and a stage is busy while
// the process that wrote it runs, not once its pid is another process's.
func TestHeadStageJournal(t *testing.T) {
	varDir := t.TempDir()
	j, err := ReadHeadStageJournal(varDir)
	require.NoError(t, err)
	assert.Nil(t, j)

	j = NewHeadStageJournal("abc")
	j.Props = map[string]string{"k": "v"}
	require.NoError(t, j.Write(varDir))
	read, err := ReadHeadStageJournal(varDir)
	require.NoError(t, err)
	assert.Equal(t, j, read)
	assert.Equal(t, HeadStageStaged, read.Step)
	assert.False(t, read.Busy(), "the stage of this process is its own")

	// Another process, running: the parent of the test.
	other := *j
	other.PID = os.Getppid()
	other.PIDStart = processStart(other.PID)
	require.NotZero(t, other.PIDStart)
	assert.True(t, other.Busy())
	other.PIDStart++
	assert.False(t, other.Busy(), "the pid was given to another process since")
	other.PID = 1 << 30
	assert.False(t, other.Busy(), "a process gone")

	require.NoError(t, RemoveHeadStageJournal(varDir))
	j, err = ReadHeadStageJournal(varDir)
	require.NoError(t, err)
	assert.Nil(t, j)
}

type killed struct{ step string }

// killAt has the stager die at the step named, as a process killed there:
// nothing of the stager runs after it.
func killAt(t *testing.T, stager *DirStager, step string, fn func()) (died bool) {
	t.Helper()
	stager.step = func(name string) {
		if name == step {
			panic(killed{name})
		}
	}
	defer func() {
		stager.step = nil
		if r := recover(); r != nil {
			if _, ok := r.(killed); !ok {
				panic(r)
			}
			died = true
		}
	}()
	fn()
	return false
}

func inode(t *testing.T, p string) uint64 {
	t.Helper()
	info, err := os.Lstat(p)
	require.NoError(t, err)
	return info.Sys().(*syscall.Stat_t).Ino
}

func newDirHead(t *testing.T) (*DirStager, string) {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, "head")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "data"), []byte("old"), 0600))
	stager := &DirStager{VarDir: filepath.Join(base, "var"), Dir: dir, Log: plog.NewDefaultLogger()}
	if !canExchange(stager.stageDir()) {
		t.Skip("the filesystem of the test directory exchanges no names")
	}
	return stager, dir
}

func fill(t *testing.T, stage HeadStage) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(stage.Dir, "data"), []byte("new"), 0600))
}

func content(dir string) string {
	if b, err := os.ReadFile(filepath.Join(dir, "data")); err == nil {
		return string(b)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "sub", "data")); err == nil {
		return string(b)
	}
	return ""
}

func leftovers(t *testing.T, stager *DirStager) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(stager.Dir))
	require.NoError(t, err)
	l := make([]string, 0)
	for _, e := range entries {
		if e.Name() != "head" && e.Name() != "var" {
			l = append(l, e.Name())
		}
	}
	if j, _ := ReadHeadStageJournal(stager.VarDir); j != nil {
		l = append(l, "journal")
	}
	return l
}

// The commit of a directory stage exchanges it with the head: the head is a
// new directory holding what the stage was written, the former tree gone.
func TestDirStagerCommit(t *testing.T) {
	ctx := context.Background()
	stager, dir := newDirHead(t)
	before := inode(t, dir)
	stage, ok, err := stager.Stage(ctx, "run1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.False(t, stage.Seeded)
	assert.Equal(t, dir, stage.LinkDest)
	info, err := os.Stat(stage.Dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0750), info.Mode().Perm(), "the stage is born with the mode of the head")
	fill(t, stage)
	assert.Equal(t, "old", content(dir), "the head as it was while its stage is written")

	require.NoError(t, stage.Commit(ctx))
	assert.Equal(t, "new", content(dir))
	assert.NotEqual(t, before, inode(t, dir), "the head is another directory")
	assert.NoDirExists(t, filepath.Join(dir, "sub"), "the former tree is gone")
	assert.Empty(t, leftovers(t, stager))
}

// A stage discarded leaves the head as it was.
func TestDirStagerDiscard(t *testing.T) {
	ctx := context.Background()
	stager, dir := newDirHead(t)
	before := inode(t, dir)
	stage, ok, err := stager.Stage(ctx, "run1")
	require.NoError(t, err)
	require.True(t, ok)
	fill(t, stage)
	require.NoError(t, stage.Discard(ctx))
	assert.Equal(t, "old", content(dir))
	assert.Equal(t, before, inode(t, dir))
	assert.Empty(t, leftovers(t, stager))
}

// A stager killed at any step leaves a head a recovery makes whole: the
// former tree when the commit was not decided, the new one from the
// decision on, and nothing of the stage in both cases.
func TestDirStagerKilledThenRecovered(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		step string
		want string
	}{
		{"mkdir", "old"},
		{"seal", "old"},
		{"exchange", "new"},
		{"remove", "new"},
		{"done", "new"},
	}
	for _, c := range cases {
		t.Run(c.step, func(t *testing.T) {
			stager, dir := newDirHead(t)
			died := killAt(t, stager, c.step, func() {
				stage, ok, err := stager.Stage(ctx, "run1")
				require.NoError(t, err)
				require.True(t, ok)
				fill(t, stage)
				require.NoError(t, stage.Commit(ctx))
			})
			require.True(t, died, "the stager goes through the step %s", c.step)
			if c.want == "old" {
				assert.Equal(t, "old", content(dir), "the head untouched before the exchange")
			}
			// Another process recovers: a stager of its own.
			recoverer := &DirStager{VarDir: stager.VarDir, Dir: dir, Log: stager.Log}
			require.NoError(t, recoverer.Recover(ctx))
			assert.Equal(t, c.want, content(dir))
			assert.Empty(t, leftovers(t, recoverer))
			require.NoError(t, recoverer.Recover(ctx), "nothing left to recover")
		})
	}
}

// A stage opened over the stage a killed action left discards it first.
func TestDirStagerStageRecoversFirst(t *testing.T) {
	ctx := context.Background()
	stager, dir := newDirHead(t)
	stage, ok, err := stager.Stage(ctx, "run1")
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, os.WriteFile(filepath.Join(stage.Dir, "half"), []byte("x"), 0600))

	again, ok, err := (&DirStager{VarDir: stager.VarDir, Dir: dir, Log: stager.Log}).Stage(ctx, "run2")
	require.NoError(t, err)
	require.True(t, ok)
	assert.NoFileExists(t, filepath.Join(again.Dir, "half"), "the stage is a new one")
	j, err := ReadHeadStageJournal(stager.VarDir)
	require.NoError(t, err)
	assert.Equal(t, "run2", j.Name)
	require.NoError(t, again.Discard(ctx))
}

// A stage of another process still running is left alone.
func TestDirStagerBusy(t *testing.T) {
	ctx := context.Background()
	stager, dir := newDirHead(t)
	stage, ok, err := stager.Stage(ctx, "run1")
	require.NoError(t, err)
	require.True(t, ok)
	j, err := ReadHeadStageJournal(stager.VarDir)
	require.NoError(t, err)
	j.PID = os.Getppid()
	j.PIDStart = processStart(j.PID)
	require.NoError(t, j.Write(stager.VarDir))

	assert.ErrorIs(t, stager.Recover(ctx), ErrHeadStageBusy)
	_, _, err = stager.Stage(ctx, "run2")
	assert.ErrorIs(t, err, ErrHeadStageBusy)
	assert.DirExists(t, stage.Dir)
	assert.Equal(t, "old", content(dir))
}

// The root of a mount is not exchanged: no stage.
func TestDirStagerRefusesAMountRoot(t *testing.T) {
	root, err := isMountRoot("/proc")
	require.NoError(t, err)
	assert.True(t, root)
	root, err = isMountRoot(t.TempDir())
	require.NoError(t, err)
	assert.False(t, root)
}
