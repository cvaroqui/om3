package resfshost

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
	"golang.org/x/sys/unix"

	"github.com/opensvc/om3/v3/core/resource"
	"github.com/opensvc/om3/v3/util/command"
	"github.com/opensvc/om3/v3/util/lvm2"
	"github.com/opensvc/om3/v3/util/plog"
)

// A filesystem on a device stages its head where its storage clones it for
// free, as it takes its snapshots:
//
// A btrfs mounting a subvolume by its path stages a writable snapshot of the
// subvolume, made beside it in the top level of the filesystem, mounted in
// the var directory of the resource for the time of the stage. The commit
// exchanges the names of the two in one rename, atomic, and mounts the head
// again: the path it mounts is then the snapshot.
//
// A filesystem on an LVM thin volume stages a thin snapshot of the volume,
// mounted in the var directory of the resource. The commit renames the
// volume aside and the snapshot to its name, and mounts the head again: the
// device it mounts is then the snapshot.
//
// Both hold what the head held when staged, shared with it: a writer writes
// the differences. Both journal their steps in the var directory of the
// resource, for the next start of the resource to finish a commit an action
// killed left, or to discard its stage. Other storage stages none.

type (
	// volumeStager stages a head whose storage clones it: the steps
	// common to the subvolume of a btrfs and to a thin volume, which
	// gen tells apart.
	volumeStager struct {
		// varDir is the var directory of the resource, where the
		// journal is.
		varDir string

		// head is what is staged, for the messages: the subvolume or
		// the volume.
		head string

		log *plog.Logger
		gen generations

		// mountLive and umountLive mount and unmount the head as the
		// resource does, and liveMounted says it is mounted.
		mountLive   func(ctx context.Context) error
		umountLive  func(ctx context.Context) error
		liveMounted func(ctx context.Context) (bool, error)

		// killWriters ends the processes working in the directory of
		// a stage held for a transfer, before it is discarded.
		killWriters func(dir string) error

		// step is called before each step changing something, with
		// its name: the tests kill the stager there.
		step func(string)

		// unmounted says a commit of this stager unmounted the head,
		// which a discard mounts back.
		unmounted bool
	}

	// generations is what a storage does of the generations of a head:
	// the live one, the stage, and the former one a commit sets aside.
	generations interface {
		// open readies the storage for the calls below, as a mount of
		// the top level of a btrfs does, and close undoes it.
		open(ctx context.Context) error
		close(ctx context.Context) error

		// id returns what tells a generation from another whatever
		// its name, of the live one, of the stage or of the former
		// one, and ok false when it does not exist.
		id(ctx context.Context, which string) (id string, ok bool, err error)

		// clone makes the stage, of the live generation, and mounts
		// it: dir is where it is written.
		clone(ctx context.Context) error
		dir() string

		// release unmounts the stage, if mounted, before it is
		// switched to or removed.
		release(ctx context.Context) error

		// atomic says swap exchanges the live generation and the
		// stage in one step, the stage taking the place of the former
		// generation. The live generation is else renamed aside, then
		// the stage to it, by two calls of rename.
		atomic() bool
		swap(ctx context.Context) error
		rename(ctx context.Context, from, to string) error

		// remove removes the stage or the former generation.
		remove(ctx context.Context, which string) error
	}
)

const (
	genLive  = "live"
	genStage = "stage"
	genOld   = "old"

	// stagedID is the journal property holding the id of the stage,
	// which tells, once the commit is decided, whether the head was
	// switched: the live generation is then of this id.
	stagedID = "stage_id"

	// stageSuffix and oldSuffix name the stage and the former generation
	// after the head.
	stageSuffix = ".om-stage"
	oldSuffix   = ".om-old"
)

func (t *volumeStager) at(name string) {
	if t.step != nil {
		t.step(name)
	}
}

// stage opens a stage of the head: a clone of it, mounted apart.
func (t *volumeStager) stage(ctx context.Context, name string) (resource.HeadStage, bool, error) {
	none := resource.HeadStage{}
	if err := t.recover(ctx); err != nil {
		return none, false, err
	}
	if v, err := t.liveMounted(ctx); err != nil {
		return none, false, err
	} else if !v {
		return none, false, fmt.Errorf("%s is not mounted", t.head)
	}
	j := resource.NewHeadStageJournal(name)
	if err := j.Write(t.varDir); err != nil {
		return none, false, err
	}
	fail := func(err error) (resource.HeadStage, bool, error) {
		return none, false, errors.Join(err, t.discard(context.WithoutCancel(ctx), j))
	}
	t.at("open")
	if err := t.gen.open(ctx); err != nil {
		return fail(err)
	}
	if _, ok, err := t.gen.id(ctx, genOld); err != nil {
		return fail(err)
	} else if ok {
		// No journal says what it is: not removed unasked.
		err := fmt.Errorf("the former generation of %s exists, left by a former stage: remove it once checked", t.head)
		return none, false, errors.Join(err, t.gen.close(ctx), resource.RemoveHeadStageJournal(t.varDir))
	}
	// A stage with no journal is what a former stage left, its journal
	// lost with the var directory.
	if _, ok, err := t.gen.id(ctx, genStage); err != nil {
		return fail(err)
	} else if ok {
		if err := t.gen.release(ctx); err != nil {
			return fail(err)
		}
		if err := t.gen.remove(ctx, genStage); err != nil {
			return fail(err)
		}
	}
	t.at("clone")
	t.log.Infof("stage %s in %s", t.head, t.gen.dir())
	if err := t.gen.clone(ctx); err != nil {
		return fail(err)
	}
	return t.handle(j), true, nil
}

// handle returns the stage whose journal is j.
func (t *volumeStager) handle(j *resource.HeadStageJournal) resource.HeadStage {
	return resource.HeadStage{
		Dir:    t.gen.dir(),
		Seeded: true,
		Seal:   func(ctx context.Context) error { return t.seal(ctx, j) },
		Commit: func(ctx context.Context) error {
			if err := t.seal(ctx, j); err != nil {
				return err
			}
			return t.finish(ctx, j)
		},
		Discard: func(ctx context.Context) error { return t.discard(ctx, j) },
		Hold: func(context.Context) error {
			j.Hold()
			return j.Write(t.varDir)
		},
	}
}

// resume returns the stage named name a former process left held.
func (t *volumeStager) resume(ctx context.Context, name string) (resource.HeadStage, error) {
	j, err := resource.ResumeHeadStageJournal(t.varDir, name)
	if err != nil {
		return resource.HeadStage{}, err
	}
	if err := t.gen.open(ctx); err != nil {
		return resource.HeadStage{}, err
	}
	return t.handle(j), nil
}

// seal journals the decision to commit, with the id of the stage.
func (t *volumeStager) seal(ctx context.Context, j *resource.HeadStageJournal) error {
	if j.Step != resource.HeadStageStaged {
		return nil
	}
	id, ok, err := t.gen.id(ctx, genStage)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("the stage of %s is gone", t.head)
	}
	sealed := *j
	sealed.Step = resource.HeadStageCommit
	sealed.Props = map[string]string{stagedID: id}
	t.at("seal")
	if err := sealed.Write(t.varDir); err != nil {
		return err
	}
	*j = sealed
	return nil
}

// switched says the live generation is the stage the journal sealed.
func (t *volumeStager) switched(ctx context.Context, j *resource.HeadStageJournal) (bool, error) {
	id, ok, err := t.gen.id(ctx, genLive)
	return ok && id == j.Props[stagedID], err
}

// finish does what is left of a sealed commit, each step done or not as the
// generations tell: the switch, then the mount of the head and the removal
// of the former generation.
func (t *volumeStager) finish(ctx context.Context, j *resource.HeadStageJournal) error {
	if err := t.gen.open(ctx); err != nil {
		return err
	}
	if j.Step == resource.HeadStageCommit {
		if err := t.switchHead(ctx, j); err != nil {
			return err
		}
		done := *j
		done.Step = resource.HeadStageCleanup
		t.at("switched")
		if err := done.Write(t.varDir); err != nil {
			return err
		}
		*j = done
	}
	if v, err := t.liveMounted(ctx); err != nil {
		return err
	} else if !v {
		t.at("mount-live")
		if err := t.mountLive(ctx); err != nil {
			return fmt.Errorf("mount %s, switched to its stage %s: %w", t.head, j.Name, err)
		}
	}
	t.unmounted = false
	// What the switch left of the former generation: under its own name,
	// or under the one of the stage when the two were exchanged.
	former := genOld
	if t.gen.atomic() {
		former = genStage
	}
	if _, ok, err := t.gen.id(ctx, former); err != nil {
		return err
	} else if ok {
		t.at("remove-old")
		if err := t.gen.remove(ctx, former); err != nil {
			return fmt.Errorf("remove the former generation of %s: %w", t.head, err)
		}
	}
	t.at("close")
	if err := t.gen.close(ctx); err != nil {
		return err
	}
	t.at("done")
	return resource.RemoveHeadStageJournal(t.varDir)
}

// switchHead makes the stage the live generation, the head unmounted: it is
// mounted again on what its name then says.
func (t *volumeStager) switchHead(ctx context.Context, j *resource.HeadStageJournal) error {
	if v, err := t.switched(ctx, j); err != nil || v {
		return err
	}
	_, staged, err := t.gen.id(ctx, genStage)
	if err != nil {
		return err
	}
	if !staged {
		return fmt.Errorf("the stage of %s is gone, and %s is not it", t.head, t.head)
	}
	_, live, err := t.gen.id(ctx, genLive)
	if err != nil {
		return err
	}
	_, old, err := t.gen.id(ctx, genOld)
	if err != nil {
		return err
	}
	if live && old {
		return fmt.Errorf("%s, its stage and its former generation all exist: none can be told the head", t.head)
	}
	if !live && !old {
		return fmt.Errorf("%s does not exist, nor its former generation", t.head)
	}
	if err := t.gen.release(ctx); err != nil {
		return err
	}
	// The head is unmounted before it is switched: one still used
	// refuses, while the stage can still be discarded.
	if v, err := t.liveMounted(ctx); err != nil {
		return err
	} else if v {
		t.at("umount-live")
		t.unmounted = true
		if err := t.umountLive(ctx); err != nil {
			return fmt.Errorf("unmount %s to switch it to its stage: %w", t.head, err)
		}
	}
	t.log.Infof("switch %s to its stage %s", t.head, j.Name)
	if t.gen.atomic() {
		t.at("swap")
		return t.gen.swap(ctx)
	}
	if live {
		t.at("rename-old")
		if err := t.gen.rename(ctx, genLive, genOld); err != nil {
			return err
		}
	}
	t.at("rename-stage")
	return t.gen.rename(ctx, genStage, genLive)
}

// discard removes the stage, the head left as it is, mounted back when a
// commit failing unmounted it. A head switched, or set aside for its stage
// to take its name, is not discarded: its commit is to finish.
func (t *volumeStager) discard(ctx context.Context, j *resource.HeadStageJournal) error {
	if err := t.gen.open(ctx); err != nil {
		return err
	}
	if j.Step != resource.HeadStageStaged {
		if v, err := t.switched(ctx, j); err != nil {
			return err
		} else if v {
			return fmt.Errorf("%s is switched to its stage already: nothing to discard", t.head)
		}
		if _, live, err := t.gen.id(ctx, genLive); err != nil {
			return err
		} else if !live {
			return fmt.Errorf("%s is set aside for its stage already: the commit is to finish, not to discard", t.head)
		}
	}
	if _, ok, err := t.gen.id(ctx, genStage); err != nil {
		return err
	} else if ok {
		t.log.Infof("discard the stage of %s", t.head)
		if j.Held && t.killWriters != nil {
			// The receiver of a transfer keeps the stage busy.
			if err := t.killWriters(t.gen.dir()); err != nil {
				return err
			}
		}
		if err := t.gen.release(ctx); err != nil {
			return err
		}
		if err := t.gen.remove(ctx, genStage); err != nil {
			return err
		}
	}
	if t.unmounted {
		if v, err := t.liveMounted(ctx); err != nil {
			return err
		} else if !v {
			if err := t.mountLive(ctx); err != nil {
				return fmt.Errorf("mount %s back: %w", t.head, err)
			}
		}
		t.unmounted = false
	}
	if err := t.gen.close(ctx); err != nil {
		return err
	}
	return resource.RemoveHeadStageJournal(t.varDir)
}

// recover finishes the commit, or discards the stage, a killed action left.
func (t *volumeStager) recover(ctx context.Context) error {
	j, err := resource.ReadHeadStageJournal(t.varDir)
	if err != nil || j == nil {
		return err
	}
	if j.Busy() {
		return fmt.Errorf("%s: %w, pid %d", t.head, resource.ErrHeadStageBusy, j.PID)
	}
	if j.Step == resource.HeadStageStaged {
		t.log.Infof("the stage %s of %s was left unfinished by the session %s", j.Name, t.head, j.SessionID)
		return t.discard(ctx, j)
	}
	t.log.Infof("finish the switch of %s to its stage %s, the session %s left undone", t.head, j.Name, j.SessionID)
	return t.finish(ctx, j)
}

// output runs the command name with args and returns what it prints, logged
// at info level when it changes something.
func (t *T) output(ctx context.Context, changes bool, name string, args ...string) (string, error) {
	level := zerolog.TraceLevel
	if changes {
		level = zerolog.InfoLevel
	}
	cmd := command.New(
		command.WithContext(ctx),
		command.WithName(name),
		command.WithArgs(args),
		command.WithLogger(t.Log()),
		command.WithBufferedStdout(),
		command.WithBufferedStderr(),
		command.WithCommandLogLevel(level),
		command.WithStdoutLogLevel(zerolog.TraceLevel),
		command.WithStderrLogLevel(level),
		command.WithErrorExitCodeLogLevel(level),
	)
	b, err := cmd.Output()
	if err != nil {
		if s := strings.TrimSpace(string(cmd.Stderr())); s != "" {
			err = fmt.Errorf("%s %s: %w: %s", name, args[0], err, s)
		}
		return "", err
	}
	return string(b), nil
}

type (
	// btrfsGenerations are the generations of a subvolume, in the top
	// level of its filesystem, mounted in top for the time of a stage.
	btrfsGenerations struct {
		// dev is the device of the filesystem, loop saying it is a
		// file, and subvol the path of the subvolume in its top level.
		dev    string
		loop   bool
		subvol string

		// top is where the top level is mounted.
		top string

		run func(ctx context.Context, changes bool, name string, args ...string) (string, error)
	}

	// thinGenerations are the generations of a thin volume: volumes of
	// its group, the stage mounted in mnt.
	thinGenerations struct {
		vg, lv string

		// fsType is the type of the filesystem, and mnt where the
		// stage is mounted.
		fsType string
		mnt    string

		run func(ctx context.Context, changes bool, name string, args ...string) (string, error)
	}
)

func (t *btrfsGenerations) path(which string) string {
	switch which {
	case genStage:
		return filepath.Join(t.top, t.subvol+stageSuffix)
	case genOld:
		return filepath.Join(t.top, t.subvol+oldSuffix)
	default:
		return filepath.Join(t.top, t.subvol)
	}
}

func (t *btrfsGenerations) dir() string  { return t.path(genStage) }
func (t *btrfsGenerations) atomic() bool { return true }

func (t *btrfsGenerations) open(ctx context.Context) error {
	if v, err := resource.IsMountRoot(t.top); err == nil && v {
		return nil
	}
	if err := os.MkdirAll(t.top, 0700); err != nil {
		return err
	}
	options := "subvolid=5"
	if t.loop {
		options += ",loop"
	}
	_, err := t.run(ctx, true, "mount", "-t", "btrfs", "-o", options, t.dev, t.top)
	return err
}

func (t *btrfsGenerations) close(ctx context.Context) error {
	if v, err := resource.IsMountRoot(t.top); err == nil && v {
		if _, err := t.run(ctx, true, "umount", t.top); err != nil {
			return err
		}
	}
	if err := os.Remove(t.top); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (t *btrfsGenerations) id(ctx context.Context, which string) (string, bool, error) {
	p := t.path(which)
	if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	} else if err != nil {
		return "", false, err
	}
	s, err := t.run(ctx, false, "btrfs", "inspect-internal", "rootid", p)
	return strings.TrimSpace(s), err == nil, err
}

func (t *btrfsGenerations) clone(ctx context.Context) error {
	_, err := t.run(ctx, true, "btrfs", "subvolume", "snapshot", t.path(genLive), t.path(genStage))
	return err
}

// release has nothing to unmount: the stage is written through the mount of
// the top level.
func (t *btrfsGenerations) release(context.Context) error { return nil }

func (t *btrfsGenerations) swap(context.Context) error {
	if err := unix.Renameat2(unix.AT_FDCWD, t.path(genStage), unix.AT_FDCWD, t.path(genLive), unix.RENAME_EXCHANGE); err != nil {
		return fmt.Errorf("exchange %s and %s: %w", t.path(genStage), t.path(genLive), err)
	}
	// The exchange is of the transaction a sync of the filesystem
	// commits.
	if d, err := os.Open(t.top); err == nil {
		_ = unix.Syncfs(int(d.Fd()))
		_ = d.Close()
	}
	return nil
}

func (t *btrfsGenerations) rename(context.Context, string, string) error {
	return errors.New("the generations of a subvolume are exchanged, not renamed")
}

func (t *btrfsGenerations) remove(ctx context.Context, which string) error {
	_, err := t.run(ctx, true, "btrfs", "subvolume", "delete", t.path(which))
	return err
}

func (t *thinGenerations) name(which string) string {
	switch which {
	case genStage:
		return t.lv + stageSuffix
	case genOld:
		return t.lv + oldSuffix
	default:
		return t.lv
	}
}

func (t *thinGenerations) fqn(which string) string { return t.vg + "/" + t.name(which) }
func (t *thinGenerations) dir() string             { return t.mnt }
func (t *thinGenerations) atomic() bool            { return false }

func (t *thinGenerations) open(context.Context) error { return nil }

func (t *thinGenerations) close(context.Context) error {
	if err := os.Remove(t.mnt); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// id returns the uuid of the volume, which its renaming keeps.
func (t *thinGenerations) id(ctx context.Context, which string) (string, bool, error) {
	s, err := t.run(ctx, false, "lvs", "--noheadings", "-o", "lv_uuid", t.fqn(which))
	if err != nil {
		// Asked about a volume its group does not hold, lvs fails,
		// as it does of a group gone: the group is asked then.
		if _, vgErr := t.run(ctx, false, "vgs", "--noheadings", "-o", "vg_name", t.vg); vgErr != nil {
			return "", false, err
		}
		return "", false, nil
	}
	return strings.TrimSpace(s), true, nil
}

// clone snapshots the volume, the snapshot activated, which a thin one is
// not by default, and mounts it: taking the snapshot freezes the filesystem,
// so the stage is one clean.
func (t *thinGenerations) clone(ctx context.Context) error {
	if _, err := t.run(ctx, true, "lvcreate", "--yes", "-s", "-kn", "-n", t.name(genStage), t.fqn(genLive)); err != nil {
		return err
	}
	if err := os.MkdirAll(t.mnt, 0700); err != nil {
		return err
	}
	args := []string{"-t", t.fsType}
	if t.fsType == "xfs" {
		// xfs refuses a second mount of the uuid of the volume.
		args = append(args, "-o", "nouuid")
	}
	_, err := t.run(ctx, true, "mount", append(args, "/dev/"+t.fqn(genStage), t.mnt)...)
	return err
}

func (t *thinGenerations) release(ctx context.Context) error {
	if v, err := resource.IsMountRoot(t.mnt); err != nil || !v {
		return nil
	}
	_, err := t.run(ctx, true, "umount", t.mnt)
	return err
}

func (t *thinGenerations) swap(context.Context) error {
	return errors.New("the generations of a thin volume are renamed, not exchanged")
}

func (t *thinGenerations) rename(ctx context.Context, from, to string) error {
	_, err := t.run(ctx, true, "lvrename", t.vg, t.name(from), t.name(to))
	return err
}

func (t *thinGenerations) remove(ctx context.Context, which string) error {
	if which == genLive {
		return errors.New("the live generation is not removed")
	}
	_, err := t.run(ctx, true, "lvremove", "-f", t.fqn(which))
	return err
}

// stagedSubvol returns the path of the subvolume the btrfs mounts, as its
// options name it, and empty when they name none, or name it by its id: the
// head mounted again after a commit is what its path then names, which an id
// or the default subvolume of the filesystem would not follow.
func (t *T) stagedSubvol() string {
	subvol := ""
	for _, option := range strings.Split(t.mountOptions(), ",") {
		switch {
		case strings.HasPrefix(option, "subvolid="):
			return ""
		case strings.HasPrefix(option, "subvol="):
			subvol = cleanSubvol(strings.TrimPrefix(option, "subvol="))
		}
	}
	return subvol
}

// headStager returns the stager of the head of the resource, and why it has
// none when it has none.
func (t *T) headStager(ctx context.Context) (*volumeStager, string) {
	if t.Zone != "" {
		return nil, "it is mounted under the root of a container"
	}
	if t.isBind() || t.fs().IsNetworked() || t.fs().IsVirtual() || t.fs().IsFileBacked() {
		return nil, "its storage clones no filesystem"
	}
	stager := &volumeStager{
		varDir: t.VarDir(),
		log:    t.Log(),
		mountLive: func(ctx context.Context) error {
			if err := t.createMountPoint(ctx); err != nil {
				return err
			}
			return t.mountFS(ctx)
		},
		// Never forced: a head still used refuses the switch, and
		// its stage is discarded.
		umountLive:  t.umountFS,
		liveMounted: t.isMounted,
		killWriters: func(dir string) error {
			return resource.KillHeadStageWriters(dir, t.Log())
		},
	}
	dev := t.devpath(ctx)
	if t.isBtrfs() {
		subvol := t.stagedSubvol()
		if subvol == "" {
			return nil, "it mounts no subvolume by its path, which a switch renames"
		}
		stager.head = dev + "[/" + subvol + "]"
		stager.gen = &btrfsGenerations{
			dev:    dev,
			loop:   t.hasMountOption("loop"),
			subvol: subvol,
			top:    filepath.Join(t.VarDir(), "head_stage_top"),
			run:    t.output,
		}
		return stager, ""
	}
	if !strings.HasPrefix(dev, "/dev/") {
		return nil, "its device is no thin volume"
	}
	lv := t.thinLV(ctx, dev)
	if lv == nil {
		return nil, "its device is no thin volume"
	}
	// The head mounted again after a commit is the volume its device
	// then names: a device named another way, as by the uuid of its
	// filesystem, which its snapshot shares, would not tell.
	if dev != lv.DevPath() && dev != lvm2.DMDevPath(lv.VGName, lv.LVName) {
		return nil, fmt.Sprintf("its device %s does not name the volume %s, which a switch renames", dev, lv.FQN())
	}
	stager.head = lv.FQN()
	stager.gen = &thinGenerations{
		vg:     lv.VGName,
		lv:     lv.LVName,
		fsType: t.Type,
		mnt:    filepath.Join(t.VarDir(), "head_stage"),
		run:    t.output,
	}
	return stager, ""
}

// journaledStager returns the stager of the stage the var directory of the
// resource journals, nil when it journals none: the storage is not asked
// about a head with no stage.
func (t *T) journaledStager(ctx context.Context) (*volumeStager, error) {
	if j, err := resource.ReadHeadStageJournal(t.VarDir()); err != nil || j == nil {
		return nil, err
	}
	stager, why := t.headStager(ctx)
	if stager == nil {
		return nil, fmt.Errorf("%s journals a stage, and stages none now: %s", t.RID(), why)
	}
	return stager, nil
}

// StageHead implements resource.HeadStager: a writable snapshot of the
// subvolume a btrfs mounts, or a snapshot of the LVM thin volume holding the
// filesystem, which the commit switches the head to. Other storage stages
// none.
func (t *T) StageHead(ctx context.Context, name string) (resource.HeadStage, bool, error) {
	stager, why := t.headStager(ctx)
	if stager == nil {
		t.Log().Debugf("no stage of %s: %s", t.mountPoint(), why)
		return resource.HeadStage{}, false, nil
	}
	if t.isBtrfs() {
		// A subvolume under the one staged is not of its snapshot.
		if v, err := t.isMounted(ctx); err != nil {
			return resource.HeadStage{}, false, err
		} else if v {
			out, err := t.output(ctx, false, "btrfs", "subvolume", "list", "-o", t.mountPoint())
			if err != nil {
				return resource.HeadStage{}, false, err
			}
			if strings.TrimSpace(out) != "" {
				t.Log().Infof("the subvolume mounted on %s holds subvolumes, a snapshot of it would not hold: no stage", t.mountPoint())
				return resource.HeadStage{}, false, nil
			}
		}
	}
	return stager.stage(ctx, name)
}

// RecoverHeadStage implements resource.HeadStager.
func (t *T) RecoverHeadStage(ctx context.Context) error {
	stager, err := t.journaledStager(ctx)
	if err != nil || stager == nil {
		return err
	}
	return stager.recover(ctx)
}

// ResumeHeadStage implements resource.HeadStager.
func (t *T) ResumeHeadStage(ctx context.Context, name string) (resource.HeadStage, error) {
	if _, err := resource.ResumeHeadStageJournal(t.VarDir(), name); err != nil {
		return resource.HeadStage{}, err
	}
	stager, err := t.journaledStager(ctx)
	if err != nil {
		return resource.HeadStage{}, err
	}
	if stager == nil {
		return resource.HeadStage{}, fmt.Errorf("%s: %w", name, resource.ErrHeadStageDiscarded)
	}
	return stager.resume(ctx, name)
}
