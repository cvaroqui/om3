package resfshost

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opensvc/om3/v3/core/resource"
	"github.com/opensvc/om3/v3/util/plog"
)

type (
	// fakeStorage is the generations of a head, by name, each the id
	// of what it holds, and whether the head is mounted: what a btrfs
	// or a volume group keeps whatever kills the stager.
	fakeStorage struct {
		// swaps says the storage exchanges the live generation and
		// the stage in one step, as a btrfs does its subvolumes.
		swaps bool

		gens    map[string]string
		mounted bool
		opened  bool

		// stageMounted says the stage is mounted apart, and next
		// numbers the clones.
		stageMounted bool
		next         int

		// busy refuses the unmount of the head, as one still used.
		busy bool

		// killed are the directories whose writers were ended.
		killed []string
	}

	died struct{ step string }
)

func newFakeStorage(swaps bool) *fakeStorage {
	return &fakeStorage{swaps: swaps, gens: map[string]string{genLive: "gen-0"}, mounted: true}
}

func (t *fakeStorage) open(context.Context) error {
	t.opened = true
	return nil
}

func (t *fakeStorage) close(context.Context) error {
	t.opened = false
	return nil
}

func (t *fakeStorage) id(_ context.Context, which string) (string, bool, error) {
	id, ok := t.gens[which]
	return id, ok, nil
}

func (t *fakeStorage) clone(context.Context) error {
	t.next++
	t.gens[genStage] = fmt.Sprintf("gen-%d", t.next)
	t.stageMounted = true
	return nil
}

func (t *fakeStorage) dir() string  { return "/stage" }
func (t *fakeStorage) atomic() bool { return t.swaps }

func (t *fakeStorage) release(context.Context) error {
	t.stageMounted = false
	return nil
}

func (t *fakeStorage) swap(context.Context) error {
	if t.mounted {
		return errors.New("the head is mounted")
	}
	t.gens[genLive], t.gens[genStage] = t.gens[genStage], t.gens[genLive]
	return nil
}

func (t *fakeStorage) rename(_ context.Context, from, to string) error {
	if _, ok := t.gens[to]; ok {
		return fmt.Errorf("%s exists", to)
	}
	if from == genLive && t.mounted {
		return errors.New("the head is mounted")
	}
	if from == genStage && t.stageMounted {
		return errors.New("the stage is mounted")
	}
	t.gens[to] = t.gens[from]
	delete(t.gens, from)
	return nil
}

func (t *fakeStorage) remove(_ context.Context, which string) error {
	if which == genStage && t.stageMounted && !t.swaps {
		return errors.New("the stage is mounted")
	}
	delete(t.gens, which)
	return nil
}

// stager returns a stager of the storage, as a process starting has one:
// with nothing of what a former one knew.
func (t *fakeStorage) stager(varDir string) *volumeStager {
	return &volumeStager{
		varDir:      varDir,
		head:        "the head",
		log:         plog.NewDefaultLogger(),
		gen:         t,
		liveMounted: func(context.Context) (bool, error) { return t.mounted, nil },
		mountLive: func(context.Context) error {
			if _, ok := t.gens[genLive]; !ok {
				return errors.New("no head to mount")
			}
			t.mounted = true
			return nil
		},
		umountLive: func(context.Context) error {
			if t.busy {
				return errors.New("target is busy")
			}
			t.mounted = false
			return nil
		},
		killWriters: func(dir string) error {
			t.killed = append(t.killed, dir)
			return nil
		},
	}
}

// dieAt has the stager die at the step named, as a process killed there.
func dieAt(stager *volumeStager, step string, fn func()) (dead bool) {
	stager.step = func(name string) {
		if name == step {
			panic(died{name})
		}
	}
	defer func() {
		stager.step = nil
		if r := recover(); r != nil {
			if _, ok := r.(died); !ok {
				panic(r)
			}
			dead = true
		}
	}()
	fn()
	return false
}

func journaled(t *testing.T, varDir string) bool {
	t.Helper()
	j, err := resource.ReadHeadStageJournal(varDir)
	require.NoError(t, err)
	return j != nil
}

func storages(t *testing.T, fn func(t *testing.T, s *fakeStorage)) {
	t.Run("exchanged, as the subvolumes of a btrfs", func(t *testing.T) { fn(t, newFakeStorage(true)) })
	t.Run("renamed, as thin volumes", func(t *testing.T) { fn(t, newFakeStorage(false)) })
}

// The head is its former generation until the commit, then the stage, with
// nothing left of the former one, and mounted.
func TestVolumeStageCommit(t *testing.T) {
	storages(t, func(t *testing.T, s *fakeStorage) {
		ctx := context.Background()
		varDir := t.TempDir()
		stage, ok, err := s.stager(varDir).stage(ctx, "s1")
		require.NoError(t, err)
		require.True(t, ok)
		assert.True(t, stage.Seeded)
		assert.Equal(t, "/stage", stage.Dir)
		assert.Equal(t, "gen-0", s.gens[genLive])
		assert.True(t, s.mounted)
		require.NoError(t, stage.Commit(ctx))
		assert.Equal(t, map[string]string{genLive: "gen-1"}, s.gens)
		assert.True(t, s.mounted)
		assert.False(t, s.opened)
		assert.False(t, journaled(t, varDir))
		assert.Error(t, stage.Discard(ctx), "a head switched is not discarded")
	})
}

// A discard leaves the head as it was, and a stage held, as for a transfer
// from another node, has its writers ended first.
func TestVolumeStageDiscard(t *testing.T) {
	storages(t, func(t *testing.T, s *fakeStorage) {
		ctx := context.Background()
		varDir := t.TempDir()
		stage, _, err := s.stager(varDir).stage(ctx, "s1")
		require.NoError(t, err)
		require.NoError(t, stage.Discard(ctx))
		assert.Equal(t, map[string]string{genLive: "gen-0"}, s.gens)
		assert.Empty(t, s.killed, "the stage of this process has no writer to end")
		assert.False(t, journaled(t, varDir))

		stage, _, err = s.stager(varDir).stage(ctx, "s2")
		require.NoError(t, err)
		require.NoError(t, stage.Hold(ctx))
		_, err = s.stager(varDir).resume(ctx, "other")
		assert.ErrorIs(t, err, resource.ErrHeadStageDiscarded)
		// The start of the head, in another process.
		require.NoError(t, s.stager(varDir).recover(ctx))
		assert.Equal(t, []string{"/stage"}, s.killed)
		assert.Equal(t, map[string]string{genLive: "gen-0"}, s.gens)
		assert.True(t, s.mounted)
		_, err = s.stager(varDir).resume(ctx, "s2")
		assert.ErrorIs(t, err, resource.ErrHeadStageDiscarded)
	})
}

// A stage held is committed by another process than the one that opened it.
func TestVolumeStageResumedCommit(t *testing.T) {
	storages(t, func(t *testing.T, s *fakeStorage) {
		ctx := context.Background()
		varDir := t.TempDir()
		stage, _, err := s.stager(varDir).stage(ctx, "s1")
		require.NoError(t, err)
		require.NoError(t, stage.Hold(ctx))
		stage, err = s.stager(varDir).resume(ctx, "s1")
		require.NoError(t, err)
		require.NoError(t, stage.Commit(ctx))
		assert.Equal(t, map[string]string{genLive: "gen-1"}, s.gens)
		assert.True(t, s.mounted)
		assert.False(t, journaled(t, varDir))
	})
}

// A head still used refuses the switch, and its stage is discarded, the head
// mounted as it was.
func TestVolumeStageCommitOfABusyHead(t *testing.T) {
	storages(t, func(t *testing.T, s *fakeStorage) {
		ctx := context.Background()
		varDir := t.TempDir()
		stage, _, err := s.stager(varDir).stage(ctx, "s1")
		require.NoError(t, err)
		s.busy = true
		require.Error(t, stage.Commit(ctx))
		require.NoError(t, stage.Discard(ctx))
		assert.Equal(t, map[string]string{genLive: "gen-0"}, s.gens)
		assert.True(t, s.mounted)
		assert.False(t, journaled(t, varDir))
	})
}

// Whatever step kills the stager while it stages, the next start discards
// what it left, and whatever step kills it once the commit is decided, the
// next start finishes the switch: the head is never left between two
// generations, nor unmounted.
func TestVolumeStageKillPoints(t *testing.T) {
	staging := []string{"open", "clone"}
	committing := []string{"umount-live", "swap", "rename-old", "rename-stage", "switched", "mount-live", "remove-old", "close", "done"}
	storages(t, func(t *testing.T, s0 *fakeStorage) {
		ctx := context.Background()
		for _, step := range staging {
			s := newFakeStorage(s0.swaps)
			varDir := t.TempDir()
			stager := s.stager(varDir)
			require.True(t, dieAt(stager, step, func() { _, _, _ = stager.stage(ctx, "s1") }), "killed at %s", step)
			require.NoError(t, s.stager(varDir).recover(ctx), "recover from a kill at %s", step)
			assert.Equal(t, map[string]string{genLive: "gen-0"}, s.gens, "killed at %s", step)
			assert.True(t, s.mounted, "killed at %s", step)
			assert.False(t, s.opened, "killed at %s", step)
			assert.False(t, journaled(t, varDir), "killed at %s", step)
		}
		// A kill before the commit is decided leaves a stage to discard.
		s := newFakeStorage(s0.swaps)
		varDir := t.TempDir()
		stager := s.stager(varDir)
		stage, _, err := stager.stage(ctx, "s1")
		require.NoError(t, err)
		require.True(t, dieAt(stager, "seal", func() { _ = stage.Commit(ctx) }))
		require.NoError(t, s.stager(varDir).recover(ctx))
		assert.Equal(t, map[string]string{genLive: "gen-0"}, s.gens)
		assert.False(t, journaled(t, varDir))

		for _, step := range committing {
			s := newFakeStorage(s0.swaps)
			varDir := t.TempDir()
			stager := s.stager(varDir)
			stage, _, err := stager.stage(ctx, "s1")
			require.NoError(t, err)
			if !dieAt(stager, step, func() { _ = stage.Commit(ctx) }) {
				// A step of the other storage.
				continue
			}
			// The start of the head: the recovery, twice killed at
			// the same step, then left to finish.
			again := s.stager(varDir)
			dieAt(again, step, func() { _ = again.recover(ctx) })
			require.NoError(t, s.stager(varDir).recover(ctx), "recover from a kill at %s", step)
			assert.Equal(t, map[string]string{genLive: "gen-1"}, s.gens, "killed at %s", step)
			assert.True(t, s.mounted, "killed at %s", step)
			assert.False(t, s.opened, "killed at %s", step)
			assert.False(t, journaled(t, varDir), "killed at %s", step)
			_, ok, err := s.stager(varDir).stage(ctx, "s2")
			require.NoError(t, err, "the next stage after a kill at %s", step)
			assert.True(t, ok)
		}
	})
}

// A former generation with no journal telling what it is refuses a stage,
// and a stage with none is one a former stage left, replaced.
func TestVolumeStageLeftovers(t *testing.T) {
	ctx := context.Background()
	s := newFakeStorage(false)
	varDir := t.TempDir()
	s.gens[genOld] = "gen-x"
	_, _, err := s.stager(varDir).stage(ctx, "s1")
	require.Error(t, err)
	assert.False(t, journaled(t, varDir))
	assert.Equal(t, "gen-x", s.gens[genOld], "not removed unasked")

	delete(s.gens, genOld)
	s.gens[genStage] = "gen-y"
	stage, ok, err := s.stager(varDir).stage(ctx, "s1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "gen-1", s.gens[genStage])
	require.NoError(t, stage.Commit(ctx))
	assert.Equal(t, map[string]string{genLive: "gen-1"}, s.gens)
}

// The subvolume a btrfs stages is the one its options mount by its path.
func TestStagedSubvol(t *testing.T) {
	assert.Equal(t, "data", (&T{Type: "btrfs", Subvol: "/data"}).stagedSubvol())
	assert.Equal(t, "a/b", (&T{Type: "btrfs", MountOptions: "noatime,subvol=/a/b"}).stagedSubvol())
	assert.Empty(t, (&T{Type: "btrfs", MountOptions: "subvolid=256"}).stagedSubvol(), "an id does not follow a rename")
	assert.Empty(t, (&T{Type: "btrfs"}).stagedSubvol(), "the default subvolume does not either")
}
