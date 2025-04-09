package restask

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/opensvc/om3/core/actioncontext"
	"github.com/opensvc/om3/core/env"
	"github.com/opensvc/om3/core/resource"
	"github.com/opensvc/om3/core/status"
	"github.com/opensvc/om3/util/confirmation"
	"github.com/opensvc/om3/util/retcodes"
	"github.com/opensvc/om3/util/runfiles"
	"github.com/opensvc/om3/util/xsession"
)

const (
	lockName = "run.lock"
)

type BaseTask struct {
	resource.T
	Check        string
	Confirmation bool
	LogOutputs   bool
	MaxParallel  int
	OnErrorCmd   string
	RetCodes     string
	RunTimeout   *time.Duration
	Schedule     string
	Snooze       *time.Duration
}

func (t *BaseTask) ScheduleOptions() resource.ScheduleOptions {
	return resource.ScheduleOptions{
		Action:              "run",
		MaxParallel:         t.MaxParallel,
		Option:              "schedule",
		Base:                "",
		Require:             t.RunRequires,
		RequireConfirmation: t.Confirmation,
		RequireProvisioned:  true,
		RequireCollector:    false,
		RunDir:              filepath.Join(t.VarDir(), "run"),
	}
}

// notifyRunDone is a noop here as for now the daemon api has no support for
// POST /run_done, and may not need one.
func (t *BaseTask) notifyRunDone() error {
	return nil
}

func (t *BaseTask) handleConfirmation(ctx context.Context) error {
	if !t.Confirmation {
		return nil
	}
	if actioncontext.IsConfirm(ctx) {
		t.Log().Infof("run confirmed by --confirm command line option")
		return nil
	}
	if actioncontext.IsCron(ctx) {
		// as set by the daemon scheduler subsystem
		return fmt.Errorf("run aborted (--cron)")
	}
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return fmt.Errorf("run aborted (stdin is not a tty)")
	}
	description := fmt.Sprintf(`The resource %s requires a run confirmation.
Please make sure you fully understand its role and effects before confirming the run.
Enter "yes" if you really want to run.`, t.RID())
	s, err := confirmation.ReadLn(description, time.Second*30)
	if err != nil {
		return fmt.Errorf("read confirmation: %w", err)
	}
	if s == "yes" {
		t.Log().Infof("run confirmed interactively")
		return nil
	}
	return fmt.Errorf("run aborted")
}

func (t *BaseTask) lastRunFile() string {
	return filepath.Join(t.VarDir(), "last_run_retcode")
}

func (t *BaseTask) StatusInfo(ctx context.Context) map[string]any {
	m := make(map[string]any)
	if i, err := t.readLastRun(); err == nil {
		m["last_run_exitcode"] = i
	}
	if i, err := t.readLastRunAt(); err == nil {
		m["last_run_at"] = i
	}
	return m
}

func (t *BaseTask) statusLastRunWarn(ctx context.Context) status.T {
	if err := resource.StatusCheckRequires(ctx, actioncontext.Run.Name, t); err != nil {
		t.StatusLog().Info("requirements not met")
		return status.NotApplicable
	}
	if i, err := t.readLastRun(); err != nil {
		t.StatusLog().Info("never run")
		return status.NotApplicable
	} else {
		s, err := t.ExitCodeToStatus(i)
		if err != nil {
			t.StatusLog().Info("%s", err)
		}
		if s != status.Up {
			t.StatusLog().Warn("last run failed (%d)", i)
		}
		return status.NotApplicable
	}
}

func (t *BaseTask) statusLastRun(ctx context.Context) status.T {
	if err := resource.StatusCheckRequires(ctx, actioncontext.Run.Name, t); err != nil {
		t.StatusLog().Info("requirements not met")
		return status.NotApplicable
	}
	if i, err := t.readLastRun(); err != nil {
		t.StatusLog().Info("never run")
		return status.NotApplicable
	} else {
		s, err := t.ExitCodeToStatus(i)
		if err != nil {
			t.StatusLog().Info("%s", err)
		}
		if s != status.Up {
			t.StatusLog().Info("last run failed (%d)", i)
		}
		return s
	}
}

func (t *BaseTask) readLastRunAt() (time.Time, error) {
	p := t.lastRunFile()
	if stat, err := os.Stat(p); err != nil {
		return time.Time{}, err
	} else {
		return stat.ModTime(), nil
	}
}

func (t *BaseTask) readLastRun() (int, error) {
	p := t.lastRunFile()
	if b, err := os.ReadFile(p); err != nil {
		return 0, err
	} else {
		return strconv.Atoi(strings.TrimSpace(string(b)))
	}
}

func (t *BaseTask) WriteLastRun(retcode int) error {
	p := t.lastRunFile()
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(f, "%d\n", retcode)
	return nil
}

func (t *BaseTask) IsRunning() bool {
	hasRunning, err := t.RunDir().HasRunning()
	if err != nil {
		return false
	}
	return hasRunning
}

func (t *BaseTask) RunDir() runfiles.Dir {
	return runfiles.Dir{
		Path: filepath.Join(t.VarDir(), "run"),
		Log:  t.Log(),
	}
}

func (t *BaseTask) RunIf(ctx context.Context, fn func(context.Context) error) error {
	runDir := t.RunDir()
	canRun := func() error {
		disable := actioncontext.IsLockDisabled(ctx)
		timeout := actioncontext.LockTimeout(ctx)
		unlock, err := t.Lock(disable, timeout, lockName)
		if err != nil {
			return err
		}
		n, err := runDir.CountAndClean()
		if err != nil {
			return err
		}
		defer unlock()
		if n >= t.MaxParallel {
			return fmt.Errorf("task is already running %d times", n)
		}
		if err := runDir.Create(xsession.ID[:]); err != nil {
			return err
		}
		return nil
	}
	if err := canRun(); err != nil {
		return err
	}
	defer runDir.Remove()

	if !env.HasDaemonOrigin() {
		defer t.notifyRunDone()
	}
	if err := t.handleConfirmation(ctx); err != nil {
		return err
	}
	if err := t.ApplyPGChain(ctx); err != nil {
		return err
	}

	return fn(ctx)
}

func (t *BaseTask) ExitCodeToStatus(exitCode int) (status.T, error) {
	m, err := retcodes.Parse(t.RetCodes)
	if err != nil {
		return status.Warn, err
	}
	return m.Status(exitCode), nil
}

func (t *BaseTask) Status(ctx context.Context) status.T {
	n, err := t.RunDir().Count()
	if err != nil {
		t.StatusLog().Error("%s", err)
	} else if n >= t.MaxParallel {
		t.StatusLog().Info("%d/%d max parallel runs reached", n, t.MaxParallel)
	}
	switch t.Check {
	case "last_run":
		return t.statusLastRun(ctx)
	case "last_run_warn":
		return t.statusLastRunWarn(ctx)
	default:
		return status.NotApplicable
	}
}
