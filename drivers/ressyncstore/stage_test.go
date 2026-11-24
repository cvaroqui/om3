package ressyncstore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opensvc/om3/v3/core/backup"
	"github.com/opensvc/om3/v3/core/resource"
	"github.com/opensvc/om3/v3/drivers/storetar"
	"github.com/opensvc/om3/v3/util/plog"
)

type (
	// fakeStager stages a directory as a copy of it, when seeded, or
	// as an empty directory, and commits by replacing it.
	fakeStager struct {
		live      string
		stageDir  string
		seeded    bool
		unable    bool
		commitErr error

		staged, sealed, committed, discarded int
	}

	// spyStore is a store calling during before each restore of a member.
	spyStore struct {
		backup.Store
		during func(member string)
	}

	// stageFixture is an object of two heads, with a run of each in a
	// tar store.
	stageFixture struct {
		t       *testing.T
		r       *T
		s       *storetar.T
		store   string
		base    string
		heads   []head
		stagers map[string]resource.HeadStager
		env     restoreEnv
		run     backup.Run
	}
)

func (t *fakeStager) ResumeHeadStage(context.Context, string) (resource.HeadStage, error) {
	return resource.HeadStage{}, resource.ErrHeadStageDiscarded
}

func (t *fakeStager) StageHead(_ context.Context, _ string) (resource.HeadStage, bool, error) {
	if t.unable {
		return resource.HeadStage{}, false, nil
	}
	t.staged++
	if err := os.MkdirAll(t.stageDir, 0755); err != nil {
		return resource.HeadStage{}, false, err
	}
	if t.seeded {
		if err := os.CopyFS(t.stageDir, os.DirFS(t.live)); err != nil {
			return resource.HeadStage{}, false, err
		}
	}
	return resource.HeadStage{
		Dir:    t.stageDir,
		Seeded: t.seeded,
		Seal: func(context.Context) error {
			t.sealed++
			return nil
		},
		Commit: func(context.Context) error {
			if t.commitErr != nil {
				return t.commitErr
			}
			t.committed++
			if err := os.RemoveAll(t.live); err != nil {
				return err
			}
			return os.Rename(t.stageDir, t.live)
		},
		Discard: func(context.Context) error {
			t.discarded++
			return os.RemoveAll(t.stageDir)
		},
	}, true, nil
}

func (t *fakeStager) RecoverHeadStage(context.Context) error { return nil }

func (t spyStore) Restore(ctx context.Context, run backup.Run, member, dst string) error {
	if t.during != nil {
		t.during(member)
	}
	return t.Store.Restore(ctx, run, member, dst)
}

// newStageFixture returns an object whose heads fs#1 and fs#2 hold "old",
// and a run of another cluster holding "new" for both.
func newStageFixture(t *testing.T) *stageFixture {
	t.Helper()
	ctx := context.Background()
	f := &stageFixture{t: t, r: newTestResource(), store: t.TempDir(), base: t.TempDir(), stagers: make(map[string]resource.HeadStager)}
	f.s = newTarStoreAt(t, f.store)
	f.run = backup.Run{ID: "run1", Path: "ns1/svc/svc1", ObjectID: testOb, ClusterID: prodID, ClusterName: "prod", Node: "n1", CreatedAt: time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)}
	for _, rid := range []string{"fs#1", "fs#2"} {
		dir := filepath.Join(f.base, "heads", strings.ReplaceAll(rid, "#", ""))
		src := filepath.Join(f.base, "src", rid)
		require.NoError(t, os.MkdirAll(filepath.Join(src, "sub"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(src, "data"), []byte("new "+rid), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(src, "sub", "more"), []byte(strings.Repeat("x", 4096)), 0600))
		require.NoError(t, f.s.Backup(ctx, f.run, rid, src, dir, nil))
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "data"), []byte("old "+rid), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "stale"), nil, 0600))
		f.heads = append(f.heads, head{rid: rid, dir: dir})
	}
	require.NoError(t, f.s.BackupStream(ctx, f.run, backup.MemberConfig, strings.NewReader("[DEFAULT]\n")))
	require.NoError(t, f.s.Complete(ctx, f.run))
	runs, err := f.s.Runs(ctx, backup.RunFilter{ID: f.run.ID})
	require.NoError(t, err)
	require.Len(t, runs, 1)
	f.run = runs[0]
	f.env = restoreEnv{
		stager: func(rid string) resource.HeadStager {
			if st, ok := f.stagers[rid]; ok {
				return st
			}
			return nil
		},
		varDir:       func(rid string) string { return filepath.Join(f.base, "var", rid) },
		progressFile: filepath.Join(f.base, "var", "sync#1", progressName),
	}
	return f
}

// dirStagers gives every head a stager exchanging directories.
func (f *stageFixture) dirStagers() {
	for _, h := range f.heads {
		f.stagers[h.rid] = dirStager{&resource.DirStager{VarDir: f.env.varDir(h.rid), Dir: h.dir, Log: plog.NewDefaultLogger()}}
	}
}

// dirStager makes a resource.DirStager the stager of a resource.
type dirStager struct {
	*resource.DirStager
}

func (t dirStager) StageHead(ctx context.Context, name string) (resource.HeadStage, bool, error) {
	return t.Stage(ctx, name)
}

func (t dirStager) RecoverHeadStage(ctx context.Context) error {
	return t.Recover(ctx)
}

func (t dirStager) ResumeHeadStage(ctx context.Context, name string) (resource.HeadStage, error) {
	return t.Resume(ctx, name)
}

func (f *stageFixture) restore(ctx context.Context, s backup.Store, mode string) (bool, error) {
	return f.r.restoreTargets(ctx, s, f.run, f.heads, f.heads, f.heads, nil, mode, f.env)
}

func (f *stageFixture) data(rid string) string {
	for _, h := range f.heads {
		if h.rid == rid {
			b, _ := os.ReadFile(filepath.Join(h.dir, "data"))
			return string(b)
		}
	}
	return ""
}

func (f *stageFixture) incomplete(rid string) bool {
	_, ok := resource.HeadIncomplete(f.env.varDir(rid))
	return ok
}

// corrupt makes the member rid of the run unreadable.
func (f *stageFixture) corrupt(rid string) {
	member := filepath.Join(f.store, prodID, testOb, f.run.ID, rid+".tar.gz")
	require.FileExists(f.t, member)
	require.NoError(f.t, os.WriteFile(member, []byte("not a gzip"), 0600))
}

// siblings returns what the directory of the heads holds beside them.
func (f *stageFixture) siblings() []string {
	entries, err := os.ReadDir(filepath.Join(f.base, "heads"))
	require.NoError(f.t, err)
	l := make([]string, 0)
	for _, e := range entries {
		if e.Name() != "fs1" && e.Name() != "fs2" {
			l = append(l, e.Name())
		}
	}
	return l
}

func ino(t *testing.T, p string) uint64 {
	t.Helper()
	info, err := os.Lstat(p)
	require.NoError(t, err)
	return info.Sys().(*syscall.Stat_t).Ino
}

// A restore of heads that stage writes their stages while the heads stay as
// they were, and switches them once every member is restored: each head is
// then another directory, holding the run and nothing else. The progress of
// the restore is told while it runs, and no longer once done. Its receipt
// says the run is in the heads.
func TestStagedRestoreSwitchesTheHeads(t *testing.T) {
	ctx := context.Background()
	f := newStageFixture(t)
	f.dirStagers()
	before := ino(t, f.heads[0].dir)
	// A former restore in place left fs#2 incomplete.
	require.NoError(t, resource.MarkHeadIncomplete(f.env.varDir("fs#2"), "a former restore"))
	var progressSeen RestoreProgress
	spy := spyStore{Store: f.s, during: func(member string) {
		assert.Equal(t, "old fs#1", f.data("fs#1"), "the heads untouched while the stages are written")
		assert.Equal(t, "old fs#2", f.data("fs#2"))
		assert.False(t, f.incomplete("fs#1"), "a head staged is not marked incomplete")
		if member == "fs#2" {
			b, err := os.ReadFile(f.env.progressFile)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(b, &progressSeen))
		}
	}}
	touched, err := f.restore(ctx, spy, stageAuto)
	require.NoError(t, err)
	assert.True(t, touched)
	assert.Equal(t, "new fs#1", f.data("fs#1"))
	assert.Equal(t, "new fs#2", f.data("fs#2"))
	assert.NoFileExists(t, filepath.Join(f.heads[0].dir, "stale"), "the head holds the run and nothing else")
	assert.NotEqual(t, before, ino(t, f.heads[0].dir), "the head is another directory")
	assert.Empty(t, f.siblings())
	assert.NoFileExists(t, f.env.progressFile)
	assert.False(t, f.incomplete("fs#2"), "a head switched to a stage written whole is complete")

	m1, _ := f.run.Member("fs#1")
	m2, _ := f.run.Member("fs#2")
	require.NotZero(t, m1.LogicalBytes)
	assert.Equal(t, f.run.ID, progressSeen.RunID)
	assert.Equal(t, os.Getpid(), progressSeen.PID)
	assert.Equal(t, m1.LogicalBytes+m2.LogicalBytes, progressSeen.BytesTotal)
	assert.Equal(t, m1.LogicalBytes, progressSeen.BytesDone, "the first member restored, the second not begun")

	f.r.acknowledgeAs(ctx, f.s, f.run, drpID, "drp", validConfig, touched, err)
	receipts, err := backup.ReadReceipts(ctx, f.s, prodID, testOb)
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	assert.Equal(t, f.run.ID, receipts[0].RunID)
	assert.False(t, receipts[0].Staged, "the stages are committed: the run is in the heads")
	assert.Empty(t, receipts[0].RestoreError)
}

// A member failing to restore discards every stage, the one already written
// too: the heads are all as they were, none touched, and no receipt says
// otherwise.
func TestStagedRestoreIsAllOrNothing(t *testing.T) {
	ctx := context.Background()
	f := newStageFixture(t)
	f.dirStagers()
	f.corrupt("fs#2")
	before := ino(t, f.heads[0].dir)
	touched, err := f.restore(ctx, f.s, stageAuto)
	require.Error(t, err)
	assert.False(t, touched)
	assert.Equal(t, "old fs#1", f.data("fs#1"))
	assert.Equal(t, "old fs#2", f.data("fs#2"))
	assert.Equal(t, before, ino(t, f.heads[0].dir))
	assert.Empty(t, f.siblings(), "no stage left")
	assert.False(t, f.incomplete("fs#1"))
	assert.False(t, f.incomplete("fs#2"))
	assert.NoFileExists(t, f.env.progressFile)

	f.r.acknowledgeAs(ctx, f.s, f.run, drpID, "drp", validConfig, touched, err)
	receipts, err := backup.ReadReceipts(ctx, f.s, prodID, testOb)
	require.NoError(t, err)
	assert.Empty(t, receipts, "the instance holds what it held: no receipt")
}

// A restore cancelled once its stages are written, before their commit,
// discards them.
func TestStagedRestoreCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := newStageFixture(t)
	f.dirStagers()
	spy := spyStore{Store: f.s, during: func(member string) {
		if member == "fs#2" {
			cancel()
		}
	}}
	touched, err := f.restore(ctx, spy, stageAuto)
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, touched)
	assert.Equal(t, "old fs#1", f.data("fs#1"))
	assert.Equal(t, "old fs#2", f.data("fs#2"))
	assert.Empty(t, f.siblings())
}

// stage=always refuses a head with no stage before anything is written, the
// stage of the other head discarded. stage=auto restores it in place.
func TestStageAlwaysRefusesAHeadWithNoStage(t *testing.T) {
	ctx := context.Background()
	f := newStageFixture(t)
	first := &fakeStager{live: f.heads[0].dir, stageDir: filepath.Join(f.base, "stage1"), seeded: true}
	f.stagers["fs#1"] = first
	f.stagers["fs#2"] = &fakeStager{unable: true}
	restores := 0
	spy := spyStore{Store: f.s, during: func(string) { restores++ }}
	touched, err := f.restore(ctx, spy, stageAlways)
	require.ErrorContains(t, err, "fs#2: no stage")
	assert.False(t, touched)
	assert.Zero(t, restores, "refused before anything is restored")
	assert.Equal(t, 1, first.discarded)
	assert.Equal(t, "old fs#1", f.data("fs#1"))
	assert.Equal(t, "old fs#2", f.data("fs#2"))

	// No stager at all, as a driver making none.
	delete(f.stagers, "fs#2")
	_, err = f.restore(ctx, spy, stageAlways)
	require.ErrorContains(t, err, "fs#2: no stage")
	assert.Zero(t, restores)

	touched, err = f.restore(ctx, spy, stageAuto)
	require.NoError(t, err)
	assert.True(t, touched)
	assert.Equal(t, "new fs#1", f.data("fs#1"))
	assert.Equal(t, "new fs#2", f.data("fs#2"))
	assert.Equal(t, 1, first.committed)
}

// A head with no stage is marked incomplete from before it is emptied: a
// restore failing leaves the mark, and a restore done clears it.
func TestRestoreInPlaceMarksTheHeadIncomplete(t *testing.T) {
	ctx := context.Background()
	f := newStageFixture(t)
	spy := spyStore{Store: f.s, during: func(member string) {
		assert.True(t, f.incomplete(member), "marked while it is written")
	}}
	touched, err := f.restore(ctx, spy, stageNever)
	require.NoError(t, err)
	assert.True(t, touched)
	assert.Equal(t, "new fs#1", f.data("fs#1"))
	assert.False(t, f.incomplete("fs#1"), "cleared once restored")
	assert.False(t, f.incomplete("fs#2"))

	f.corrupt("fs#2")
	touched, err = f.restore(ctx, f.s, stageAuto)
	require.Error(t, err)
	assert.True(t, touched)
	assert.Empty(t, f.data("fs#2"), "the head emptied")
	reason, ok := resource.HeadIncomplete(f.env.varDir("fs#2"))
	assert.True(t, ok)
	assert.Contains(t, reason, "run1")
	assert.True(t, f.incomplete("fs#1"), "restored, of a run the other head does not hold: the restore is not done")
}

// A head with no stage, restored in place beside a head that stages, keeps
// its mark when the stage can not be committed: the heads are not of one
// run.
func TestStagedRestoreCommitFailing(t *testing.T) {
	ctx := context.Background()
	f := newStageFixture(t)
	first := &fakeStager{live: f.heads[0].dir, stageDir: filepath.Join(f.base, "stage1"), seeded: true, commitErr: errors.New("target is busy")}
	f.stagers["fs#1"] = first
	touched, err := f.restore(ctx, f.s, stageAuto)
	require.ErrorContains(t, err, "target is busy")
	assert.True(t, touched, "fs#2 was restored in place")
	assert.Equal(t, 1, first.sealed)
	assert.Equal(t, 1, first.discarded)
	assert.Equal(t, "old fs#1", f.data("fs#1"))
	assert.Equal(t, "new fs#2", f.data("fs#2"))
	assert.True(t, f.incomplete("fs#2"))
}

// Heads switched before the commit of another fails are not undone: the
// stages not committed are left sealed, for the start of their resources to
// finish, and the restore says a head was touched.
func TestStagedRestoreCommitFailingHalfway(t *testing.T) {
	ctx := context.Background()
	f := newStageFixture(t)
	first := &fakeStager{live: f.heads[0].dir, stageDir: filepath.Join(f.base, "stage1"), seeded: true}
	second := &fakeStager{live: f.heads[1].dir, stageDir: filepath.Join(f.base, "stage2"), seeded: true, commitErr: errors.New("target is busy")}
	f.stagers["fs#1"], f.stagers["fs#2"] = first, second
	touched, err := f.restore(ctx, f.s, stageAuto)
	require.ErrorContains(t, err, "fs#1 switched and fs#2 not")
	assert.True(t, touched)
	assert.Equal(t, 1, first.committed)
	assert.Equal(t, 1, second.sealed)
	assert.Zero(t, second.discarded, "left sealed, to finish")
	assert.Equal(t, "new fs#1", f.data("fs#1"))
}

// A stage seeded with the head keeps the paths excluded from the run, as a
// restore in place leaves them. A stage born empty would lose them: the head
// is restored in place instead, or refused with stage=always.
func TestStagedRestoreKeepsTheExcludedPaths(t *testing.T) {
	ctx := context.Background()
	f := newStageFixture(t)
	f.heads = f.heads[:1]
	f.heads[0].excludes = []string{"/cache"}
	touch(t, filepath.Join(f.heads[0].dir, "cache", "kept"))
	seeded := &fakeStager{live: f.heads[0].dir, stageDir: filepath.Join(f.base, "stage1"), seeded: true}
	f.stagers["fs#1"] = seeded
	touched, err := f.restore(ctx, f.s, stageAlways)
	require.NoError(t, err)
	assert.True(t, touched)
	assert.Equal(t, 1, seeded.committed)
	assert.Equal(t, "new fs#1", f.data("fs#1"))
	assert.FileExists(t, filepath.Join(f.heads[0].dir, "cache", "kept"))
	assert.NoFileExists(t, filepath.Join(f.heads[0].dir, "stale"))

	empty := &fakeStager{live: f.heads[0].dir, stageDir: filepath.Join(f.base, "stage1")}
	f.stagers["fs#1"] = empty
	_, err = f.restore(ctx, f.s, stageAlways)
	require.ErrorContains(t, err, "paths excluded")
	assert.Equal(t, 1, empty.discarded)
	assert.FileExists(t, filepath.Join(f.heads[0].dir, "cache", "kept"))
	touched, err = f.restore(ctx, f.s, stageAuto)
	require.NoError(t, err)
	assert.True(t, touched)
	assert.Zero(t, empty.committed, "restored in place")
	assert.FileExists(t, filepath.Join(f.heads[0].dir, "cache", "kept"))
}

// A member restored under a head, not at it, is written at the same place
// of the stage, reached with no link followed: a link at its place in the
// stage, as one the head held when it was cloned, is refused, what it points
// at untouched, and the stage discarded.
func TestStagedRestoreFollowsNoLinkInTheStage(t *testing.T) {
	ctx := context.Background()
	f := newStageFixture(t)
	under := f.heads[0]
	outside := t.TempDir()
	touch(t, filepath.Join(outside, "keep"))
	require.NoError(t, os.Symlink(outside, filepath.Join(under.dir, "part")))
	seeded := &fakeStager{live: under.dir, stageDir: filepath.Join(f.base, "stage1"), seeded: true}
	f.stagers["fs#1"] = seeded
	target := head{rid: "fs#1", dir: filepath.Join(under.dir, "part")}
	touched, err := f.r.restoreTargets(ctx, f.s, f.run, []head{target}, []head{under}, f.heads, nil, stageAuto, f.env)
	require.Error(t, err)
	assert.False(t, touched)
	assert.FileExists(t, filepath.Join(outside, "keep"))
	assert.NoFileExists(t, filepath.Join(outside, "data"))
	assert.Equal(t, 1, seeded.discarded)

	// A directory there is restored into, in the stage, the rest of the
	// head kept.
	require.NoError(t, os.Remove(filepath.Join(under.dir, "part")))
	touched, err = f.r.restoreTargets(ctx, f.s, f.run, []head{target}, []head{under}, f.heads, nil, stageAuto, f.env)
	require.NoError(t, err)
	assert.True(t, touched)
	b, _ := os.ReadFile(filepath.Join(under.dir, "part", "data"))
	assert.Equal(t, "new fs#1", string(b))
	assert.Equal(t, "old fs#1", f.data("fs#1"), "the rest of the head as the stage was seeded")
}

// A head holding the head of another resource, or a mount, is not switched:
// what is in it would stay in the tree it is switched from.
func TestHeadHoldingAnotherIsNotStaged(t *testing.T) {
	ctx := context.Background()
	f := newStageFixture(t)
	st := &fakeStager{live: f.heads[0].dir, stageDir: filepath.Join(f.base, "stage1"), seeded: true}
	f.stagers["fs#1"] = st
	nested := head{rid: "fs#3", dir: filepath.Join(f.heads[0].dir, "nested")}
	_, why, err := openStageOf(ctx, f.env, f.run, f.heads[0], f.heads[:1], append(f.heads, nested), nil)
	require.NoError(t, err)
	assert.Contains(t, why, "nested")
	assert.Zero(t, st.staged)
	_, why, err = openStageOf(ctx, f.env, f.run, f.heads[0], f.heads[:1], f.heads, []string{filepath.Join(f.heads[0].dir, "proc")})
	require.NoError(t, err)
	assert.Contains(t, why, "proc")
}

// The stage option of a restore takes over the keyword, auto when neither
// says.
func TestStageMode(t *testing.T) {
	r := newTestResource()
	for _, c := range []struct{ kw, option, want string }{
		{"", "", stageAuto},
		{"never", "", stageNever},
		{"never", "always", stageAlways},
		{"always", "auto", stageAuto},
	} {
		r.Stage = c.kw
		mode, err := r.stageMode(c.option)
		require.NoError(t, err)
		assert.Equal(t, c.want, mode)
	}
	_, err := r.stageMode("sometimes")
	assert.Error(t, err)
}
