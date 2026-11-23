package ressyncstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/opensvc/om3/v3/core/backup"
	"github.com/opensvc/om3/v3/core/resource"
	"github.com/opensvc/om3/v3/util/xsession"
)

// A restore in place empties a head, then writes it: interrupted, it leaves
// a head holding neither what it held nor what the run saved. So a head
// whose storage makes a stage of it is not written: its member is restored
// into the stage, a generation apart, and the head is switched to it once
// every member of the run is restored, the stages of the heads one after the
// other. Until then the heads are what the last restore written whole left,
// and a restore failing, cancelled or killed discards its stages.
//
// A head with no stage is restored in place, after the stages are written,
// and marked incomplete from before it is emptied until the restore is done,
// which a start refuses to run on.

const (
	stageAuto   = "auto"
	stageAlways = "always"
	stageNever  = "never"

	// progressName is the name of the file telling how far a restore in
	// place went, in the var directory of the resource.
	progressName = "restore_progress.json"

	// progressPeriod is how often the progress file is written at most.
	progressPeriod = time.Second
)

type (
	// restoreEnv is what a restore of targets needs of the resources
	// holding the heads.
	restoreEnv struct {
		// stager returns the stager of the head of the resource rid,
		// nil when its driver stages none.
		stager func(rid string) resource.HeadStager

		// varDir returns the var directory of the resource rid, where
		// its head is marked incomplete.
		varDir func(rid string) string

		// progressFile is where the progress of the restore is told,
		// nowhere when empty.
		progressFile string
	}

	// openStage is a stage a restore opened.
	openStage struct {
		rid   string
		stage resource.HeadStage

		// dir is the directory of the stage, with no link on its path.
		dir string
	}

	// RestoreProgress is how far a restore in place went, as the file
	// restore_progress.json of the var directory of the resource tells
	// while it runs.
	RestoreProgress struct {
		RunID     string    `json:"run_id"`
		SessionID string    `json:"session_id"`
		PID       int       `json:"pid"`
		StartedAt time.Time `json:"started_at"`

		// BytesTotal is the size of the members to restore, as the run
		// counted them, and BytesDone what was read of them: a store
		// not counting as it restores is counted a member at a time.
		BytesTotal int64     `json:"bytes_total"`
		BytesDone  int64     `json:"bytes_done"`
		UpdatedAt  time.Time `json:"updated_at"`
	}

	// progress writes the progress file of a restore.
	progress struct {
		mu      sync.Mutex
		file    string
		data    RestoreProgress
		members int64
		written time.Time
	}
)

// stageMode returns how the heads are restored: as the option of the
// restore says, or else as the stage keyword does.
func (t *T) stageMode(option string) (string, error) {
	mode := option
	if mode == "" {
		mode = t.Stage
	}
	switch mode {
	case "":
		return stageAuto, nil
	case stageAuto, stageAlways, stageNever:
		return mode, nil
	default:
		return "", fmt.Errorf("stage %q: want %s, %s or %s", mode, stageAuto, stageAlways, stageNever)
	}
}

// restoreEnv returns what a restore needs of the resources of the object.
func (t *T) restoreEnv() restoreEnv {
	o := t.GetObjectDriver()
	return restoreEnv{
		stager: func(rid string) resource.HeadStager {
			stager, _ := o.ResourceByID(rid).(resource.HeadStager)
			return stager
		},
		varDir: func(rid string) string {
			r := o.ResourceByID(rid)
			if r == nil {
				return ""
			}
			return r.VarDir()
		},
		progressFile: filepath.Join(t.VarDir(), progressName),
	}
}

// restoreTargets restores into each of targets its member of run, targets[i]
// being under the head heads[i].
//
// The heads that stage, as mode allows, have their members restored into
// their stages first. The members of the other heads are then restored in
// place, each head marked incomplete before it is emptied. The stages are
// committed last, sealed all before the first is, once nothing is left that
// can fail but the commits: an error or a cancellation before discards them
// all, the heads they stage untouched. The marks are cleared once everything
// is done, the ones a former restore left on a head now switched too.
//
// It says whether a head was touched: emptied, written or switched.
func (t *T) restoreTargets(ctx context.Context, s backup.Store, run backup.Run, targets, heads, objectHeads []head, mounts []string, mode string, env restoreEnv) (touched bool, err error) {
	// An interrupt of the command cancels the restore, which then
	// discards its stages and says it failed, rather than kill it: one
	// received while the heads are switched waits for them to be.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	// The progress a killed restore left is not the one of this one.
	if env.progressFile != "" {
		_ = os.Remove(env.progressFile)
	}
	stages := make(map[string]*openStage)
	discard := func() error {
		var errs error
		// A restore cancelled still discards its stages.
		dctx := context.WithoutCancel(ctx)
		for _, rid := range slices.Sorted(maps.Keys(stages)) {
			if err := stages[rid].stage.Discard(dctx); err != nil {
				errs = errors.Join(errs, fmt.Errorf("discard the stage of %s: %w", rid, err))
			}
			delete(stages, rid)
		}
		return errs
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, discard())
		}
	}()
	if mode != stageNever {
		for i, under := range heads {
			if _, ok := stages[under.rid]; ok || slices.IndexFunc(heads[:i], func(h head) bool { return h.rid == under.rid }) >= 0 {
				continue
			}
			var members []head
			for j, h := range targets {
				if heads[j].rid == under.rid {
					members = append(members, h)
				}
			}
			st, why, err := openStageOf(ctx, env, run, under, members, objectHeads, mounts)
			switch {
			case err != nil:
				return false, fmt.Errorf("stage %s: %w", under.rid, err)
			case st != nil:
				stages[under.rid] = st
			case mode == stageAlways:
				return false, fmt.Errorf("%s: no stage to restore into, %s: restore it in place with --stage auto, emptied then written", under.rid, why)
			default:
				t.Log().Infof("restore %s in place, emptied then written: no stage, %s", under.rid, why)
			}
		}
	}
	prog := newProgress(env.progressFile, run, targets)
	defer prog.remove()
	ctx = backup.WithProgress(ctx, prog.add)
	memberBytes := func(rid string) int64 {
		m, _ := run.Member(rid)
		return m.LogicalBytes
	}

	// The members of the heads that stage, into their stages.
	for i, h := range targets {
		st, ok := stages[heads[i].rid]
		if !ok {
			continue
		}
		rel, err := filepath.Rel(heads[i].dir, h.dir)
		if err != nil {
			return touched, err
		}
		into := head{rid: h.rid, dir: filepath.Join(st.dir, rel), excludes: h.excludes}
		t.Log().Infof("restore %s of run %s from store %s into the stage %s of %s", h.rid, run.ShortID(), s.Ref(), into.dir, h.dir)
		if _, err := restoreUnder(ctx, s, run, into, head{rid: st.rid, dir: st.dir}, nil); err != nil {
			return touched, fmt.Errorf("restore %s: %w", h.rid, err)
		}
		prog.memberDone(memberBytes(h.rid))
	}

	// The members of the other heads, in place.
	marked := make([]string, 0)
	for i, h := range targets {
		under := heads[i]
		if _, ok := stages[under.rid]; ok {
			continue
		}
		if varDir := env.varDir(under.rid); varDir != "" && !slices.Contains(marked, varDir) {
			reason := fmt.Sprintf("the restore in place of run %s, by the session %s, emptied it and did not finish: restore it again", run.ShortID(), xsession.SessionID())
			if err := resource.MarkHeadIncomplete(varDir, reason); err != nil {
				return touched, fmt.Errorf("mark %s incomplete: %w", under.rid, err)
			}
			marked = append(marked, varDir)
		}
		t.Log().Infof("restore %s of run %s from store %s into %s", h.rid, run.ShortID(), s.Ref(), h.dir)
		emptied, err := restoreUnder(ctx, s, run, h, under, apartUnder(h.dir, objectHeads, mounts))
		touched = touched || emptied
		if err != nil {
			return touched, fmt.Errorf("restore %s: %w", h.rid, err)
		}
		prog.memberDone(memberBytes(h.rid))
	}

	// The commits. A cancellation is honored until the first stage is
	// sealed, and no longer: from then on the heads are being switched.
	if len(stages) > 0 {
		if err := ctx.Err(); err != nil {
			return touched, err
		}
		cctx := context.WithoutCancel(ctx)
		rids := slices.Sorted(maps.Keys(stages))
		for _, rid := range rids {
			if err := stages[rid].stage.Seal(cctx); err != nil {
				return touched, fmt.Errorf("seal the stage of %s: %w", rid, err)
			}
		}
		switched := make([]string, 0, len(rids))
		for _, rid := range rids {
			t.Log().Infof("switch %s to its stage, holding run %s", rid, run.ShortID())
			if err := stages[rid].stage.Commit(cctx); err != nil {
				if len(switched) == 0 {
					// No head switched: the stages are
					// discarded, the heads untouched, but one
					// whose switch went too far to discard.
					err = fmt.Errorf("switch %s to its stage: %w", rid, err)
					if derr := discard(); derr != nil {
						return true, errors.Join(err, derr)
					}
					return touched, err
				}
				// Heads are switched and this one is not: its
				// stage, and the ones after it, are left sealed
				// for the start of their resources to switch to,
				// rather than discarded beside heads of the run.
				left := rids[len(switched):]
				for _, rid := range left {
					delete(stages, rid)
				}
				return true, fmt.Errorf("switch %s to its stage: %w: %s switched and %s not, which the next start of the instance finishes", rid, err, strings.Join(switched, " "), strings.Join(left, " "))
			}
			switched = append(switched, rid)
			delete(stages, rid)
			touched = true
		}
	}
	// Every head restored into holds its members of the run, the ones
	// switched to their stage as the ones written in place: the marks
	// this restore made, and the ones a former restore left, are cleared.
	for _, under := range heads {
		varDir := env.varDir(under.rid)
		if varDir == "" {
			continue
		}
		if err := resource.ClearHeadIncomplete(varDir); err != nil {
			return touched, err
		}
	}
	return touched, nil
}

// openStageOf opens a stage of the head under, the members restoring under
// it, and returns nil and why when the head is restored in place instead.
func openStageOf(ctx context.Context, env restoreEnv, run backup.Run, under head, members, objectHeads []head, mounts []string) (*openStage, string, error) {
	stager := env.stager(under.rid)
	if stager == nil {
		return nil, "its driver makes none", nil
	}
	// A head switched to its stage would leave what is mounted in it, or
	// the heads of other resources in it, in the tree it switched from.
	if apart := apartUnder(under.dir, objectHeads, mounts); len(apart) > 0 {
		return nil, fmt.Sprintf("%s in it, heads or mounts of their own, a switch would take away", strings.Join(apart, " ")), nil
	}
	stage, ok, err := stager.StageHead(ctx, run.ShortID())
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return nil, "its storage makes none", nil
	}
	refuse := func(why string) (*openStage, string, error) {
		if err := stage.Discard(context.WithoutCancel(ctx)); err != nil {
			return nil, "", fmt.Errorf("discard the stage: %w", err)
		}
		return nil, why, nil
	}
	if !stage.Seeded {
		// A stage born empty holds what the restore writes and
		// nothing else: it replaces a head only when the member is
		// the whole of it, with no path excluded, which a restore
		// leaves as it is in the head.
		switch {
		case len(members) != 1 || filepath.Clean(members[0].dir) != filepath.Clean(under.dir):
			return refuse("its stage is born empty, and the run holds a part of the head only")
		case len(members[0].excludes) > 0:
			return refuse("its stage is born empty, and the paths excluded from the run are left as they are in the head")
		}
	}
	// The stage is reached as a head is, with no link on its path: the
	// var directory holding one may be reached through a link, which is
	// the node's, not a process of the object's.
	dir, err := filepath.EvalSymlinks(stage.Dir)
	if err != nil {
		_, _, _ = refuse("")
		return nil, "", err
	}
	return &openStage{rid: under.rid, stage: stage, dir: dir}, "", nil
}

// newProgress returns the progress of a restore of the members targets of
// run, told in file.
func newProgress(file string, run backup.Run, targets []head) *progress {
	now := time.Now().UTC()
	p := &progress{
		file: file,
		data: RestoreProgress{
			RunID:     run.ID,
			SessionID: xsession.SessionID().String(),
			PID:       os.Getpid(),
			StartedAt: now,
			UpdatedAt: now,
		},
	}
	for _, h := range targets {
		if m, ok := run.Member(h.rid); ok {
			p.data.BytesTotal += m.LogicalBytes
		}
	}
	p.write()
	return p
}

// add counts n more bytes restored.
func (t *progress) add(n int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data.BytesDone += n
	if time.Since(t.written) >= progressPeriod {
		t.write()
	}
}

// memberDone counts a member of n bytes restored whole: the bytes done are
// at least the ones of the members restored, which a store not counting as
// it restores leaves them at.
func (t *progress) memberDone(n int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.members += n
	if t.data.BytesDone < t.members {
		t.data.BytesDone = t.members
	}
	t.write()
}

// write writes the progress file, through a file renamed over it, so a
// reader never reads one half-written. A progress that can not be written
// is not told, and the restore goes on.
func (t *progress) write() {
	if t.file == "" {
		return
	}
	t.written = time.Now()
	t.data.UpdatedAt = t.written.UTC()
	b, err := json.Marshal(t.data)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(t.file), 0755); err != nil {
		return
	}
	tmp := t.file + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0644); err != nil {
		return
	}
	if err := os.Rename(tmp, t.file); err != nil {
		_ = os.Remove(tmp)
	}
}

// remove removes the progress file, the restore done or not.
func (t *progress) remove() {
	if t.file == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_ = os.Remove(t.file)
}
