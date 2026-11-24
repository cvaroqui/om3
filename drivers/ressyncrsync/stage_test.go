package ressyncrsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opensvc/om3/v3/core/resource"
	"github.com/opensvc/om3/v3/util/hostname"
	"github.com/opensvc/om3/v3/util/plog"
)

// fakePeer is the api of a peer staging a directory head: its stager, and
// what is asked of it.
type fakePeer struct {
	stager *resource.DirStager
	calls  []string

	// answer is the stage answered instead of one of the stager, and
	// openErr the error.
	answer  *resource.SyncStage
	openErr error

	// seededOnly is what the last stage was asked with.
	seededOnly bool
}

func (t *fakePeer) Open(ctx context.Context, name string, seededOnly bool) (resource.SyncStage, error) {
	t.calls = append(t.calls, "open")
	t.seededOnly = seededOnly
	if t.openErr != nil {
		return resource.SyncStage{}, t.openErr
	}
	if t.answer != nil {
		return *t.answer, nil
	}
	stage, _, err := t.stager.Stage(ctx, name)
	if err != nil {
		return resource.SyncStage{}, err
	}
	if err := stage.Hold(ctx); err != nil {
		return resource.SyncStage{}, err
	}
	return resource.SyncStage{Staged: true, Dir: stage.Dir, LinkDest: stage.LinkDest, HeadRID: "fs#1"}, nil
}

func (t *fakePeer) Commit(ctx context.Context, name string) error {
	t.calls = append(t.calls, "commit")
	if t.stager == nil {
		return nil
	}
	stage, err := t.stager.Resume(ctx, name)
	if err != nil {
		return err
	}
	return stage.Commit(ctx)
}

func (t *fakePeer) Discard(ctx context.Context, name string) error {
	t.calls = append(t.calls, "discard")
	if t.stager == nil {
		return nil
	}
	stage, err := t.stager.Resume(ctx, name)
	if errors.Is(err, resource.ErrHeadStageDiscarded) {
		return nil
	}
	if err != nil {
		return err
	}
	return stage.Discard(ctx)
}

// newStagedSync returns a resource syncing to a peer whose head is a
// directory, staged, and the head.
func newStagedSync(t *testing.T) (*T, *fakePeer, string) {
	t.Helper()
	base := t.TempDir()
	head := filepath.Join(base, "head")
	require.NoError(t, os.MkdirAll(head, 0750))
	require.NoError(t, os.WriteFile(filepath.Join(head, "data"), []byte("old"), 0600))
	peer := &fakePeer{stager: &resource.DirStager{VarDir: filepath.Join(base, "var"), Dir: head, Log: plog.NewDefaultLogger()}}
	r := &T{Src: "/src/", Dst: head, ResetOptions: true}
	r.newPeerStager = func(string) (peerStager, error) { return peer, nil }
	return r, peer, head
}

// destination returns the directory a transfer run with args is written
// into: its last argument, the peer left out.
func destination(args []string) string {
	_, dir, _ := strings.Cut(args[len(args)-1], ":")
	return dir
}

func headData(head string) string {
	b, _ := os.ReadFile(filepath.Join(head, "data"))
	return string(b)
}

// A transfer is written into the stage the peer opened, hardlinking from
// the committed tree, and the head of the peer is switched once it is
// written whole.
func TestPeerSyncStagesThenCommits(t *testing.T) {
	r, peer, head := newStagedSync(t)
	r.runRsync = func(_ context.Context, nodename string, args []string) error {
		assert.Equal(t, "n2", nodename)
		assert.Contains(t, args, "--link-dest="+head)
		dir := destination(args)
		assert.NotEqual(t, head, dir, "not the live head")
		assert.Equal(t, "old", headData(head), "the head untouched during the transfer")
		return os.WriteFile(filepath.Join(dir, "data"), []byte("new"), 0600)
	}
	require.NoError(t, r.peerSync(context.Background(), modeIncr, "n2", []string{"/src/"}))
	assert.Equal(t, []string{"open", "commit"}, peer.calls)
	assert.Equal(t, "new", headData(head))
}

// A failover during the transfer starts the peer on the generation last
// committed, its start discarding the stage: the run fails cleanly, at the
// transfer cut short or at the commit told the stage is gone.
func TestPeerSyncFailoverMidTransfer(t *testing.T) {
	for _, cut := range []bool{true, false} {
		r, peer, head := newStagedSync(t)
		r.runRsync = func(ctx context.Context, _ string, args []string) error {
			require.NoError(t, os.WriteFile(filepath.Join(destination(args), "data"), []byte("half"), 0600))
			// The start of the head on the peer, waiting for nobody.
			require.NoError(t, peer.stager.Recover(ctx))
			assert.Equal(t, "old", headData(head), "started on the committed generation")
			if cut {
				return errors.New("rsync: connection unexpectedly closed")
			}
			return nil
		}
		err := r.peerSync(context.Background(), modeIncr, "n2", []string{"/src/"})
		require.Error(t, err)
		if cut {
			assert.Equal(t, []string{"open", "discard"}, peer.calls)
		} else {
			assert.ErrorIs(t, err, resource.ErrHeadStageDiscarded)
			assert.Equal(t, []string{"open", "commit", "discard"}, peer.calls)
		}
		assert.Equal(t, "old", headData(head))
		entries, _ := os.ReadDir(filepath.Dir(head))
		assert.Len(t, entries, 2, "the head and the var directory: no stage left")
	}
}

// A transfer failing, or cancelled, discards its stage, the head untouched.
func TestPeerSyncDiscardsOnError(t *testing.T) {
	r, peer, head := newStagedSync(t)
	ctx, cancel := context.WithCancel(context.Background())
	r.runRsync = func(ctx context.Context, _ string, args []string) error {
		require.NoError(t, os.WriteFile(filepath.Join(destination(args), "data"), []byte("half"), 0600))
		cancel()
		return ctx.Err()
	}
	require.ErrorIs(t, r.peerSync(ctx, modeIncr, "n2", []string{"/src/"}), context.Canceled)
	assert.Equal(t, []string{"open", "discard"}, peer.calls, "a transfer cancelled still discards its stage")
	assert.Equal(t, "old", headData(head))
	j, err := resource.ReadHeadStageJournal(peer.stager.VarDir)
	require.NoError(t, err)
	assert.Nil(t, j)
}

// The options writing into the files in place are refused with a stage
// hardlinking from the committed tree, before anything is transferred, and
// allowed with a stage seeded, or with none.
func TestInplaceIsRefusedWithLinkDest(t *testing.T) {
	for _, option := range []string{"--inplace", "--append", "--append-verify"} {
		r, peer, head := newStagedSync(t)
		r.Options = []string{option}
		ran := false
		r.runRsync = func(context.Context, string, []string) error {
			ran = true
			return nil
		}
		err := r.peerSync(context.Background(), modeIncr, "n2", []string{"/src/"})
		require.Error(t, err, option)
		assert.Contains(t, err.Error(), option)
		assert.Contains(t, err.Error(), "stage=never")
		assert.False(t, ran, "nothing is transferred")
		assert.Equal(t, []string{"open", "discard"}, peer.calls)
		assert.Equal(t, "old", headData(head))

		// With a stage seeded, as a clone, and with a head staging
		// none, written in place.
		for _, answer := range []resource.SyncStage{
			{Staged: true, Seeded: true, Dir: "/stage", HeadRID: "fs#1"},
			{Dir: head},
		} {
			peer.calls, peer.answer, peer.stager = nil, &answer, nil
			r.runRsync = func(_ context.Context, _ string, args []string) error {
				assert.Contains(t, args, option)
				assert.Equal(t, answer.Dir, destination(args))
				for _, arg := range args {
					assert.False(t, strings.HasPrefix(arg, "--link-dest"), arg)
				}
				return nil
			}
			require.NoError(t, r.peerSync(context.Background(), modeIncr, "n2", []string{"/src/"}))
			assert.Equal(t, []string{"open", "commit"}, peer.calls, "the commit clears the mark of a head written in place")
		}
	}
	options, err := stageOptions([]string{"-a", "--no-inplace", "--exclude=--inplace"}, resource.SyncStage{LinkDest: "/head"}, "n2")
	require.NoError(t, err)
	assert.Equal(t, []string{"-a", "--no-inplace", "--exclude=--inplace", "--link-dest=/head"}, options)
}

// stage=never asks the peer for nothing, and writes into the destination,
// as does a transfer no daemon answers: an error of the peer fails it.
func TestPeerSyncInPlace(t *testing.T) {
	r, peer, head := newStagedSync(t)
	r.runRsync = func(_ context.Context, _ string, args []string) error {
		assert.Equal(t, head, destination(args))
		return nil
	}
	r.Stage = stageNever
	r.Options = []string{"--inplace"}
	require.NoError(t, r.peerSync(context.Background(), modeIncr, "n2", []string{"/src/"}))
	assert.Empty(t, peer.calls)

	r.Stage = stageAuto
	peer.openErr = fmt.Errorf("%w: connection refused", errStageUnreachable)
	require.NoError(t, r.peerSync(context.Background(), modeIncr, "n2", []string{"/src/"}))
	assert.Equal(t, []string{"open"}, peer.calls, "neither committed nor discarded: nothing was opened")

	peer.calls, peer.openErr = nil, errors.New("stage fs#1: tank/data is not mounted")
	ran := false
	r.runRsync = func(context.Context, string, []string) error {
		ran = true
		return nil
	}
	err := r.peerSync(context.Background(), modeIncr, "n2", []string{"/src/"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not mounted")
	assert.False(t, ran, "a head not up refuses the transfer")
}

// The target local stages through the resources of the object, with no api
// call, and a peer through the api.
func TestLocalTargetStagesWithoutTheAPI(t *testing.T) {
	r := &T{}
	stager, err := r.peerStager(hostname.Hostname())
	require.NoError(t, err)
	assert.IsType(t, localStager{}, stager)
	assert.Equal(t, stageAuto, r.stageMode(), "the default")
	dst, dstFS := (&T{Dst: "/srv/data", DstFS: "/srv"}).SyncDestination()
	assert.Equal(t, "/srv/data", dst)
	assert.Equal(t, "/srv", dstFS)
}

// A transfer leaving a part of the destination as it is asks for no stage
// but one holding the destination as committed: an empty one switched to
// would lose what the transfer left.
func TestPartialTransferAsksForASeededStage(t *testing.T) {
	for _, options := range [][]string{
		{"--exclude", "logs/"},
		{"--exclude=logs/"},
		{"--filter=- *.tmp"},
		{"-f", "- *.tmp"},
		{"-avC"},
		{"-n"},
		{"--dry-run"},
		{"-au"},
		{"--max-size=1g"},
		{"--files-from=/etc/list"},
	} {
		assert.True(t, leavesOut(options), "%v", options)
	}
	for _, options := range [][]string{
		{"-HAXpogDtrlvx", "--stats", "--delete", "--force"},
		{"-e", "ssh -i /root/.ssh/f -C"},
		{"--timeout=10", "--bwlimit=500", "--inplace"},
	} {
		assert.False(t, leavesOut(options), "%v", options)
	}
	r, peer, head := newStagedSync(t)
	r.runRsync = func(context.Context, string, []string) error { return nil }
	answer := resource.SyncStage{Dir: head}
	peer.answer, peer.stager = &answer, nil
	require.NoError(t, r.peerSync(context.Background(), modeIncr, "n2", []string{"/src/"}))
	assert.False(t, peer.seededOnly)
	r.Options = []string{"--exclude=logs/"}
	require.NoError(t, r.peerSync(context.Background(), modeIncr, "n2", []string{"/src/"}))
	assert.True(t, peer.seededOnly)
}
