package resource

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/opensvc/om3/v3/util/mountinfo"
	"github.com/opensvc/om3/v3/util/plog"
)

// DirStager stages a head that is a directory of a filesystem, not its
// root: the stage is a sibling directory, on the same filesystem, which the
// commit exchanges with the head in one rename, atomic. The head is at every
// moment the former tree whole or the new one whole.
type DirStager struct {
	// VarDir is the var directory of the resource holding the head,
	// where the journal is.
	VarDir string

	// Dir is the head.
	Dir string

	Log *plog.Logger

	// step is called before each step changing something, with its
	// name: the tests kill the stager there.
	step func(string)
}

// dirStageInode is the journal property holding the inode of the stage,
// which tells, once the commit is decided, whether the exchange was done:
// the head is then this inode.
const dirStageInode = "stage_inode"

func (t *DirStager) at(name string) {
	if t.step != nil {
		t.step(name)
	}
}

// stageDir returns the path of the stage: a hidden sibling of the head.
func (t *DirStager) stageDir() string {
	dir := filepath.Clean(t.Dir)
	return filepath.Join(filepath.Dir(dir), "."+filepath.Base(dir)+".om-stage")
}

// Stage opens a stage of the head, empty. It returns ok false for a head
// that is the root of a mount, which no rename replaces, and on a filesystem
// that exchanges no names.
func (t *DirStager) Stage(ctx context.Context, name string) (HeadStage, bool, error) {
	if err := t.Recover(ctx); err != nil {
		return HeadStage{}, false, err
	}
	dir := filepath.Clean(t.Dir)
	info, err := os.Lstat(dir)
	if err != nil {
		return HeadStage{}, false, err
	}
	if !info.IsDir() {
		return HeadStage{}, false, fmt.Errorf("%s is not a directory", dir)
	}
	if root, err := isMountRoot(dir); err != nil {
		return HeadStage{}, false, err
	} else if root {
		t.Log.Debugf("%s is the root of a mount, which no rename replaces: no stage", dir)
		return HeadStage{}, false, nil
	}
	stage := t.stageDir()
	// A tree at the name of the stage with no journal is what a former
	// stage left, its journal lost with the var directory.
	if err := removeTree(stage); err != nil {
		return HeadStage{}, false, fmt.Errorf("remove the former stage %s: %w", stage, err)
	}
	if !canExchange(stage) {
		t.Log.Debugf("the filesystem of %s exchanges no names: no stage", dir)
		return HeadStage{}, false, nil
	}
	j := NewHeadStageJournal(name)
	if err := j.Write(t.VarDir); err != nil {
		return HeadStage{}, false, err
	}
	t.at("mkdir")
	t.Log.Infof("stage %s in %s", dir, stage)
	if err := os.Mkdir(stage, 0700); err != nil {
		return HeadStage{}, false, errors.Join(err, RemoveHeadStageJournal(t.VarDir))
	}
	// The stage is born with the owner and the mode of the head, which
	// a writer restoring those of the tree it writes overwrites.
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		_ = os.Lchown(stage, int(st.Uid), int(st.Gid))
	}
	_ = os.Chmod(stage, info.Mode().Perm())
	return HeadStage{
		Dir:      stage,
		LinkDest: dir,
		Seal:     func(context.Context) error { return t.seal(j) },
		Commit: func(context.Context) error {
			if err := t.seal(j); err != nil {
				return err
			}
			return t.finish(j)
		},
		Discard: func(context.Context) error { return t.discard(j) },
	}, true, nil
}

// seal journals the decision to commit, with the inode of the stage.
func (t *DirStager) seal(j *HeadStageJournal) error {
	if j.Step != HeadStageStaged {
		return nil
	}
	ino, err := inodeOf(t.stageDir())
	if err != nil {
		return err
	}
	sealed := *j
	sealed.Step = HeadStageCommit
	sealed.Props = map[string]string{dirStageInode: strconv.FormatUint(ino, 10)}
	t.at("seal")
	if err := sealed.Write(t.VarDir); err != nil {
		return err
	}
	*j = sealed
	return nil
}

// exchanged says the head is the stage the journal sealed.
func (t *DirStager) exchanged(j *HeadStageJournal) bool {
	ino, err := inodeOf(filepath.Clean(t.Dir))
	return err == nil && strconv.FormatUint(ino, 10) == j.Props[dirStageInode]
}

// finish does what is left of a sealed commit: the exchange, when the head
// is not the stage yet, then the removal of the former tree, which the
// exchange left at the name of the stage.
func (t *DirStager) finish(j *HeadStageJournal) error {
	dir := filepath.Clean(t.Dir)
	stage := t.stageDir()
	if !t.exchanged(j) {
		t.at("exchange")
		t.Log.Infof("switch %s to its stage %s", dir, stage)
		err := unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, dir, unix.RENAME_EXCHANGE)
		if errors.Is(err, unix.ENOENT) {
			// No head to exchange with, as one removed since: the
			// stage takes its name.
			if _, statErr := os.Lstat(dir); errors.Is(statErr, os.ErrNotExist) {
				err = os.Rename(stage, dir)
			}
		}
		if err != nil {
			return fmt.Errorf("exchange %s and %s: %w", stage, dir, err)
		}
		if err := syncDir(filepath.Dir(dir)); err != nil {
			return err
		}
	}
	t.at("remove")
	if err := removeTree(stage); err != nil {
		return fmt.Errorf("remove the former tree of %s, left in %s: %w", dir, stage, err)
	}
	t.at("done")
	return RemoveHeadStageJournal(t.VarDir)
}

// discard removes the stage, the head left as it is.
func (t *DirStager) discard(j *HeadStageJournal) error {
	if j.Step != HeadStageStaged && t.exchanged(j) {
		return fmt.Errorf("%s is switched to its stage already: nothing to discard", t.Dir)
	}
	stage := t.stageDir()
	t.Log.Infof("discard the stage %s of %s", stage, t.Dir)
	if err := removeTree(stage); err != nil {
		return err
	}
	return RemoveHeadStageJournal(t.VarDir)
}

// Recover finishes the commit, or discards the stage, a killed action left.
func (t *DirStager) Recover(ctx context.Context) error {
	j, err := ReadHeadStageJournal(t.VarDir)
	if err != nil || j == nil {
		return err
	}
	if j.Busy() {
		return fmt.Errorf("%s: %w, pid %d", t.Dir, ErrHeadStageBusy, j.PID)
	}
	if j.Step == HeadStageStaged {
		t.Log.Infof("the stage %s of %s was left unfinished by the session %s", j.Name, t.Dir, j.SessionID)
		return t.discard(j)
	}
	t.Log.Infof("finish the switch of %s to its stage %s, the session %s left undone", t.Dir, j.Name, j.SessionID)
	return t.finish(j)
}

func inodeOf(p string) (uint64, error) {
	info, err := os.Lstat(p)
	if err != nil {
		return 0, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("%s: no inode", p)
	}
	return st.Ino, nil
}

// isMountRoot says dir is the root of a mount, as the kernel tells, or as
// its device, another than the one of its parent, does on a kernel not
// telling.
func isMountRoot(dir string) (bool, error) {
	var stx unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, dir, unix.AT_SYMLINK_NOFOLLOW, 0, &stx); err == nil && stx.Attributes_mask&unix.STATX_ATTR_MOUNT_ROOT != 0 {
		return stx.Attributes&unix.STATX_ATTR_MOUNT_ROOT != 0, nil
	}
	var st, pst unix.Stat_t
	if err := unix.Lstat(dir, &st); err != nil {
		return false, err
	}
	if err := unix.Lstat(filepath.Dir(dir), &pst); err != nil {
		return false, err
	}
	return st.Dev != pst.Dev || st.Ino == pst.Ino, nil
}

// canExchange says the filesystem of the directory the stage is made in
// exchanges two names, which it is asked of two directories made for it.
func canExchange(stage string) bool {
	a, b := stage+".probe-a", stage+".probe-b"
	defer func() {
		_ = os.Remove(a)
		_ = os.Remove(b)
	}()
	_ = os.Remove(a)
	_ = os.Remove(b)
	if os.Mkdir(a, 0700) != nil || os.Mkdir(b, 0700) != nil {
		return false
	}
	return unix.Renameat2(unix.AT_FDCWD, a, unix.AT_FDCWD, b, unix.RENAME_EXCHANGE) == nil
}

// removeTree removes the tree at p, through a root opened on it, which
// follows no link out of it. A tree holding a mount point is refused: what
// is mounted in it is not of the tree.
func removeTree(p string) error {
	if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	entries, err := mountinfo.Read()
	if err != nil {
		return err
	}
	for _, e := range entries {
		target := filepath.Clean(e.Target)
		if rel, err := filepath.Rel(p, target); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return fmt.Errorf("%s is mounted in it", target)
		}
	}
	root, err := os.OpenRoot(p)
	if err != nil {
		return err
	}
	defer root.Close()
	d, err := root.Open(".")
	if err != nil {
		return err
	}
	names, err := d.Readdirnames(-1)
	_ = d.Close()
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := root.RemoveAll(name); err != nil {
			return err
		}
	}
	return os.Remove(p)
}
