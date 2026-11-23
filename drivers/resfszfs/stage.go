package resfszfs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rs/zerolog"

	"github.com/opensvc/om3/v3/core/resource"
	"github.com/opensvc/om3/v3/util/command"
	"github.com/opensvc/om3/v3/util/plog"
)

// The stage of a dataset is a clone of a snapshot of it, mounted in the var
// directory of the resource: it holds what the dataset held at the snapshot,
// shared with it, and a restore writes into it while the dataset is left as
// it is. The commit promotes the clone, which then owns the snapshots of the
// dataset, renames the dataset aside and the clone to its name, and destroys
// the dataset renamed: the head is then the clone, a dataset of another guid
// under the same name.
//
// A clone is a new dataset, with none of the properties set on the one it
// is cloned from: the properties set on the dataset, local or received, are
// set on the clone. The ones that would act on the stage before it is the
// head are set at the commit: where it is mounted, what it reserves beside
// the dataset still reserving it, its sharing and its being read-only.
//
// The journal of the stage is a user property of the clone, which follows
// it through its promotion and its renaming to the dataset: a pool imported
// on another node, as one failing over while a commit was under way, tells
// that node what to finish, where the journal file of the var directory of
// the node that began is not. The property is the authority, and the file
// tells the process staging, for a recovery to leave a stage of a process
// still running alone, and a stage begun before its clone was made.

type (
	// headStager stages the head of a dataset.
	headStager struct {
		// dataset is the dataset mounted as the head.
		dataset string

		// varDir is the var directory of the resource, where the
		// journal is and the stage is mounted.
		varDir string

		log *plog.Logger

		// zfs runs the zfs command with args and returns what it
		// prints. changes says it changes something.
		zfs func(ctx context.Context, changes bool, args ...string) (string, error)

		// mountLive and umountLive mount and unmount the dataset as
		// the resource does, and liveMounted says it is mounted.
		mountLive   func(ctx context.Context) error
		umountLive  func(ctx context.Context) error
		liveMounted func(ctx context.Context) (bool, error)

		// step is called before each step changing something, with
		// its name: the tests kill the stager there.
		step func(string)

		// unmounted says a commit of this stager unmounted the
		// dataset, which a discard mounts back.
		unmounted bool
	}
)

const (
	// stageSuffix and oldSuffix name the clone staging a dataset and the
	// dataset renamed aside by a commit, after it, and stageSnapshot the
	// snapshot the clone is of.
	stageSuffix   = ".om-stage"
	oldSuffix     = ".om-old"
	stageSnapshot = "om-stage"

	// propMountpoint and propCanmount are the properties the stage is
	// given values of its own, restored at the commit.
	propMountpoint = "mountpoint"
	propCanmount   = "canmount"

	// propStage is the user property holding the journal of a stage,
	// set on its clone, the dataset once the clone is renamed to it.
	propStage = "com.opensvc:stage"
)

// deferredProps are the properties set on the dataset that the stage is given
// at its commit only, after what is left of the former generation is
// destroyed: a reservation beside the one of the dataset could not fit the
// pool, a stage shared would export a tree half-written, and one read-only
// could not be written.
var deferredProps = []string{"reservation", "refreservation", "readonly", "sharenfs", "sharesmb"}

func (t *headStager) at(name string) {
	if t.step != nil {
		t.step(name)
	}
}

func (t *headStager) stageName() string { return t.dataset + stageSuffix }
func (t *headStager) oldName() string   { return t.dataset + oldSuffix }
func (t *headStager) snapName() string  { return t.dataset + "@" + stageSnapshot }
func (t *headStager) stageDir() string  { return filepath.Join(t.varDir, "head_stage") }

// exists says the dataset or snapshot name exists.
func (t *headStager) exists(ctx context.Context, name string) (bool, error) {
	_, err := t.zfs(ctx, false, "list", "-H", "-o", "name", "-t", "all", name)
	switch {
	case err == nil:
		return true, nil
	case isNoDataset(err):
		return false, nil
	default:
		return false, err
	}
}

// isNoDataset says err is the one of a zfs command asked about a dataset
// that does not exist, or of a pool not imported.
func isNoDataset(err error) bool {
	return strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "no such pool")
}

// journalOf returns the journal the dataset name carries, nil when it
// carries none, or does not exist.
func (t *headStager) journalOf(ctx context.Context, name string) (*resource.HeadStageJournal, error) {
	// Set on this dataset, not inherited from the one holding it.
	s, err := t.zfs(ctx, false, "get", "-Hp", "-o", "value", "-s", "local", propStage, name)
	if err != nil {
		if isNoDataset(err) {
			return nil, nil
		}
		return nil, err
	}
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return nil, nil
	}
	var j resource.HeadStageJournal
	if err := json.Unmarshal([]byte(s), &j); err != nil {
		return nil, fmt.Errorf("the property %s of %s: %w", propStage, name, err)
	}
	return &j, nil
}

// journal returns the journal of the stage of the dataset, as its datasets
// carry it: the clone, or the dataset once the clone is renamed to it. It
// returns nil when neither carries one.
func (t *headStager) journal(ctx context.Context) (*resource.HeadStageJournal, error) {
	if j, err := t.journalOf(ctx, t.stageName()); err != nil || j != nil {
		return j, err
	}
	return t.journalOf(ctx, t.dataset)
}

// journalValue returns the journal as the property holds it. The process
// staging is not said: it is one of this node, which the journal file of
// the node tells.
func journalValue(j *resource.HeadStageJournal) (string, error) {
	shared := *j
	shared.PID, shared.PIDStart = 0, 0
	b, err := json.Marshal(shared)
	return string(b), err
}

// record journals j on the dataset name, then in the var directory.
func (t *headStager) record(ctx context.Context, j *resource.HeadStageJournal, name string) error {
	v, err := journalValue(j)
	if err != nil {
		return err
	}
	if _, err := t.zfs(ctx, true, "set", propStage+"="+v, name); err != nil {
		return err
	}
	return j.Write(t.varDir)
}

// forget removes the journal, from the dataset and from the var directory:
// nothing is left of the stage.
func (t *headStager) forget(ctx context.Context) error {
	if j, err := t.journalOf(ctx, t.dataset); err != nil {
		return err
	} else if j != nil {
		if _, err := t.zfs(ctx, true, "inherit", propStage, t.dataset); err != nil {
			return err
		}
	}
	return resource.RemoveHeadStageJournal(t.varDir)
}

// get returns the value of the property prop of the dataset name.
func (t *headStager) get(ctx context.Context, name, prop string) (string, error) {
	s, err := t.zfs(ctx, false, "get", "-Hp", "-o", "value", prop, name)
	return strings.TrimSpace(s), err
}

func (t *headStager) mounted(ctx context.Context, name string) (bool, error) {
	v, err := t.get(ctx, name, "mounted")
	return v == "yes", err
}

// setProps returns the properties set on the dataset name, local or
// received, by name.
func (t *headStager) setProps(ctx context.Context, name string) (map[string]string, error) {
	s, err := t.zfs(ctx, false, "get", "-Hp", "-o", "property,value", "-s", "local,received", "all", name)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string)
	for _, line := range strings.Split(s, "\n") {
		prop, value, ok := strings.Cut(line, "\t")
		// The journal of a stage is its own, not a property of the
		// dataset to give its clone.
		if !ok || prop == propStage {
			continue
		}
		m[prop] = value
	}
	return m, nil
}

// stage opens a stage of the dataset: a clone of a snapshot of it, mounted
// in the var directory. It returns ok false for a dataset it can not switch
// whole: the root of a pool, which nothing renames, one with children, which
// the clone would not hold, and an encrypted one.
func (t *headStager) stage(ctx context.Context, name string) (resource.HeadStage, bool, error) {
	none := resource.HeadStage{}
	if err := t.recover(ctx); err != nil {
		return none, false, err
	}
	if !strings.Contains(t.dataset, "/") {
		t.log.Debugf("%s is the root dataset of its pool, which nothing renames: no stage", t.dataset)
		return none, false, nil
	}
	if v, err := t.liveMounted(ctx); err != nil {
		return none, false, err
	} else if !v {
		return none, false, fmt.Errorf("%s is not mounted", t.dataset)
	}
	s, err := t.zfs(ctx, false, "list", "-H", "-o", "name", "-r", "-t", "filesystem,volume", t.dataset)
	if err != nil {
		return none, false, err
	}
	if children := strings.Fields(s); len(children) > 1 {
		t.log.Infof("%s has children, %s, a clone of it would not hold: no stage", t.dataset, strings.Join(children[1:], " "))
		return none, false, nil
	}
	// A zfs with no encryption knows no such property.
	if v, err := t.get(ctx, t.dataset, "encryption"); err == nil && v != "off" && v != "-" && v != "" {
		t.log.Infof("%s is encrypted, a clone of it sharing its key: no stage", t.dataset)
		return none, false, nil
	}
	for _, leftover := range []string{t.oldName(), t.stageName()} {
		if v, err := t.exists(ctx, leftover); err != nil {
			return none, false, err
		} else if v {
			return none, false, fmt.Errorf("%s exists, left by a former stage of %s: destroy it once checked", leftover, t.dataset)
		}
	}
	props, err := t.setProps(ctx, t.dataset)
	if err != nil {
		return none, false, err
	}
	j := resource.NewHeadStageJournal(name)
	j.Props = make(map[string]string)
	args := []string{"clone", "-o", propCanmount + "=noauto", "-o", propMountpoint + "=" + t.stageDir()}
	for _, prop := range slices.Sorted(maps.Keys(props)) {
		if prop == propMountpoint || prop == propCanmount || slices.Contains(deferredProps, prop) {
			j.Props[prop] = props[prop]
			continue
		}
		args = append(args, "-o", prop+"="+props[prop])
	}
	// The clone is born carrying its journal.
	v, err := journalValue(j)
	if err != nil {
		return none, false, err
	}
	args = append(args, "-o", propStage+"="+v, t.snapName(), t.stageName())
	if err := j.Write(t.varDir); err != nil {
		return none, false, err
	}
	fail := func(err error) (resource.HeadStage, bool, error) {
		return none, false, errors.Join(err, t.discard(context.WithoutCancel(ctx), j))
	}
	t.at("snapshot")
	if v, err := t.exists(ctx, t.snapName()); err != nil {
		return fail(err)
	} else if v {
		// A snapshot of this name with no stage is one a former stage
		// left.
		if _, err := t.zfs(ctx, true, "destroy", t.snapName()); err != nil {
			return fail(err)
		}
	}
	if _, err := t.zfs(ctx, true, "snapshot", t.snapName()); err != nil {
		return fail(err)
	}
	t.at("clone")
	if _, err := t.zfs(ctx, true, args...); err != nil {
		return fail(err)
	}
	t.at("mount")
	if _, err := t.zfs(ctx, true, "mount", t.stageName()); err != nil {
		return fail(err)
	}
	return resource.HeadStage{
		Dir:    t.stageDir(),
		Seeded: true,
		Seal:   func(ctx context.Context) error { return t.seal(ctx, j) },
		Commit: func(ctx context.Context) error {
			if err := t.seal(ctx, j); err != nil {
				return err
			}
			return t.finish(ctx, j)
		},
		Discard: func(ctx context.Context) error { return t.discard(ctx, j) },
	}, true, nil
}

// seal journals the decision to commit, on the clone.
func (t *headStager) seal(ctx context.Context, j *resource.HeadStageJournal) error {
	if j.Step != resource.HeadStageStaged {
		return nil
	}
	sealed := *j
	sealed.Step = resource.HeadStageCommit
	t.at("seal")
	if err := t.record(ctx, &sealed, t.stageName()); err != nil {
		return err
	}
	*j = sealed
	return nil
}

// finish does what is left of a sealed commit, each step done or not as the
// datasets tell: the switch, which a failure leaves to finish or, the clone
// not promoted yet, to discard, then the mount of the head and the removal
// of what is left of the former generation, whose failure is only said, the
// head being the new one.
func (t *headStager) finish(ctx context.Context, j *resource.HeadStageJournal) error {
	if j.Step == resource.HeadStageCommit {
		if err := t.switchHead(ctx, j); err != nil {
			return err
		}
		// The clone is the dataset, which carries the journal now.
		done := *j
		done.Step = resource.HeadStageCleanup
		t.at("switched")
		if err := t.record(ctx, &done, t.dataset); err != nil {
			return err
		}
		*j = done
	}
	if err := t.cleanup(ctx, j); err != nil {
		t.log.Warnf("%s is switched to its stage %s, with leftovers the next start removes: %s", t.dataset, j.Name, err)
		return nil
	}
	t.at("done")
	return t.forget(ctx)
}

// switchHead makes the clone the dataset, and mounts it as the head.
func (t *headStager) switchHead(ctx context.Context, j *resource.HeadStageJournal) error {
	staged, err := t.exists(ctx, t.stageName())
	if err != nil {
		return err
	}
	live, err := t.exists(ctx, t.dataset)
	if err != nil {
		return err
	}
	if !staged && !live {
		return fmt.Errorf("neither %s nor its stage %s exist", t.dataset, t.stageName())
	}
	if staged {
		if v, err := t.mounted(ctx, t.stageName()); err != nil {
			return err
		} else if v {
			t.at("umount-stage")
			if _, err := t.zfs(ctx, true, "unmount", t.stageName()); err != nil {
				return err
			}
		}
		if live {
			if old, err := t.exists(ctx, t.oldName()); err != nil {
				return err
			} else if old {
				return fmt.Errorf("%s, %s and %s all exist: none can be told the head", t.dataset, t.stageName(), t.oldName())
			}
			// The dataset is unmounted before the clone is promoted:
			// one still used refuses, while the stage can still be
			// discarded.
			if v, err := t.liveMounted(ctx); err != nil {
				return err
			} else if v {
				t.at("umount-live")
				t.unmounted = true
				if err := t.umountLive(ctx); err != nil {
					return fmt.Errorf("unmount %s to switch it to its stage: %w", t.dataset, err)
				}
			}
			if origin, err := t.get(ctx, t.stageName(), "origin"); err != nil {
				return err
			} else if origin != "-" && origin != "" {
				t.at("promote")
				if _, err := t.zfs(ctx, true, "promote", t.stageName()); err != nil {
					return err
				}
			}
			t.at("rename-old")
			if _, err := t.zfs(ctx, true, "rename", t.dataset, t.oldName()); err != nil {
				return err
			}
		}
		t.at("rename-stage")
		if _, err := t.zfs(ctx, true, "rename", t.stageName(), t.dataset); err != nil {
			return err
		}
	}
	// Where the head is mounted, and whether it mounts with its pool,
	// are the dataset's again: as set on the former one, or as inherited
	// when it set none.
	t.at("set-mountpoint")
	if v, ok := j.Props[propMountpoint]; ok {
		if _, err := t.zfs(ctx, true, "set", propMountpoint+"="+v, t.dataset); err != nil {
			return err
		}
	} else if _, err := t.zfs(ctx, true, "inherit", propMountpoint, t.dataset); err != nil {
		return err
	}
	canmount := "on"
	if v, ok := j.Props[propCanmount]; ok {
		canmount = v
	}
	t.at("set-canmount")
	if _, err := t.zfs(ctx, true, "set", propCanmount+"="+canmount, t.dataset); err != nil {
		return err
	}
	if v, err := t.liveMounted(ctx); err != nil {
		return err
	} else if !v {
		t.at("mount-live")
		if err := t.mountLive(ctx); err != nil {
			return err
		}
	}
	t.unmounted = false
	return nil
}

// cleanup destroys what is left of the former generation, the dataset
// renamed aside, with the snapshots taken of it since the stage, and the
// snapshot the stage was cloned from, then sets the properties it deferred.
func (t *headStager) cleanup(ctx context.Context, j *resource.HeadStageJournal) error {
	if v, err := t.exists(ctx, t.oldName()); err != nil {
		return err
	} else if v {
		t.at("destroy-old")
		if _, err := t.zfs(ctx, true, "destroy", "-r", t.oldName()); err != nil {
			return err
		}
	}
	if v, err := t.exists(ctx, t.snapName()); err != nil {
		return err
	} else if v {
		t.at("destroy-snapshot")
		if _, err := t.zfs(ctx, true, "destroy", t.snapName()); err != nil {
			return err
		}
	}
	for _, prop := range deferredProps {
		v, ok := j.Props[prop]
		if !ok {
			continue
		}
		if cur, err := t.get(ctx, t.dataset, prop); err == nil && cur == v {
			continue
		}
		t.at("set-" + prop)
		if _, err := t.zfs(ctx, true, "set", prop+"="+v, t.dataset); err != nil {
			return err
		}
	}
	_ = os.Remove(t.stageDir())
	return nil
}

// discard destroys the stage, the dataset left as it is, mounted back when
// a commit failing unmounted it. A clone promoted is the head already, and
// is not discarded.
func (t *headStager) discard(ctx context.Context, j *resource.HeadStageJournal) error {
	if j.Step == resource.HeadStageCleanup {
		return fmt.Errorf("%s is switched to its stage already: nothing to discard", t.dataset)
	}
	staged, err := t.exists(ctx, t.stageName())
	if err != nil {
		return err
	}
	if staged {
		if origin, err := t.get(ctx, t.stageName(), "origin"); err != nil {
			return err
		} else if origin == "-" || origin == "" {
			return fmt.Errorf("the stage %s is promoted already: the commit is to finish, not to discard", t.stageName())
		}
		t.log.Infof("discard the stage %s of %s", t.stageName(), t.dataset)
		if v, err := t.mounted(ctx, t.stageName()); err != nil {
			return err
		} else if v {
			if _, err := t.zfs(ctx, true, "unmount", t.stageName()); err != nil {
				return err
			}
		}
		if _, err := t.zfs(ctx, true, "destroy", t.stageName()); err != nil {
			return err
		}
	} else if j.Step != resource.HeadStageStaged {
		return fmt.Errorf("the stage of %s is renamed to it already: the commit is to finish, not to discard", t.dataset)
	}
	if v, err := t.exists(ctx, t.snapName()); err != nil {
		return err
	} else if v {
		if _, err := t.zfs(ctx, true, "destroy", t.snapName()); err != nil {
			return err
		}
	}
	if t.unmounted {
		if v, err := t.liveMounted(ctx); err != nil {
			return err
		} else if !v {
			if err := t.mountLive(ctx); err != nil {
				return fmt.Errorf("mount %s back: %w", t.dataset, err)
			}
		}
		t.unmounted = false
	}
	_ = os.Remove(t.stageDir())
	return resource.RemoveHeadStageJournal(t.varDir)
}

// recover finishes the commit, or discards the stage, a killed action left,
// on this node or on the one the pool was imported on before: the journal
// the datasets carry says which, and the one of the var directory when they
// carry none, as of a stage killed before its clone was made.
func (t *headStager) recover(ctx context.Context) error {
	local, err := resource.ReadHeadStageJournal(t.varDir)
	if err != nil {
		return err
	}
	if local != nil && local.Busy() {
		return fmt.Errorf("%s: %w, pid %d", t.dataset, resource.ErrHeadStageBusy, local.PID)
	}
	j, err := t.journal(ctx)
	if err != nil {
		return err
	}
	if j == nil {
		if local == nil {
			return nil
		}
		// No dataset carries a journal: the stage has no clone, never
		// made or gone, and what is left of it is discarded.
		j = local
		j.Step = resource.HeadStageStaged
	}
	j.Adopt()
	if j.Step == resource.HeadStageStaged {
		t.log.Infof("the stage %s of %s was left unfinished by the session %s", j.Name, t.dataset, j.SessionID)
		return t.discard(ctx, j)
	}
	t.log.Infof("finish the switch of %s to its stage %s, the session %s left undone", t.dataset, j.Name, j.SessionID)
	return t.finish(ctx, j)
}

// headStager returns the stager of the head of the resource.
func (t *T) headStager() *headStager {
	log := t.Log().Attr("device", t.Device)
	return &headStager{
		dataset: t.Device,
		varDir:  t.VarDir(),
		log:     log,
		zfs: func(ctx context.Context, changes bool, args ...string) (string, error) {
			level := zerolog.TraceLevel
			if changes {
				level = zerolog.InfoLevel
			}
			cmd := command.New(
				command.WithContext(ctx),
				command.WithName("/usr/sbin/zfs"),
				command.WithArgs(args),
				command.WithLogger(log),
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
					err = fmt.Errorf("zfs %s: %w: %s", args[0], err, s)
				}
				return "", err
			}
			return string(b), nil
		},
		liveMounted: t.isMounted,
		mountLive: func(ctx context.Context) error {
			legacy, err := t.isLegacy()
			if err != nil {
				return err
			}
			if legacy {
				if err := os.MkdirAll(t.mountPoint(), defaultPerm); err != nil {
					return err
				}
			}
			return t.mountWithLegacy(legacy)
		},
		umountLive: func(ctx context.Context) error {
			legacy, err := t.isLegacy()
			if err != nil {
				return err
			}
			if legacy {
				return t.umountLegacy()
			}
			// Never forced: a head still used refuses the switch,
			// and its stage is discarded.
			return t.fs().Umount()
		},
	}
}

// StageHead implements resource.HeadStager: a clone of the dataset, which
// the commit renames to it. A zoned dataset, mounted by the agent under the
// root of a container, stages none.
func (t *T) StageHead(ctx context.Context, name string) (resource.HeadStage, bool, error) {
	if z, err := t.zoneOf(); err != nil {
		return resource.HeadStage{}, false, err
	} else if z != nil {
		return resource.HeadStage{}, false, nil
	}
	return t.headStager().stage(ctx, name)
}

// RecoverHeadStage implements resource.HeadStager.
func (t *T) RecoverHeadStage(ctx context.Context) error {
	return t.headStager().recover(ctx)
}
