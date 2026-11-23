package resource

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/opensvc/om3/v3/util/xsession"
)

// A stage of a head is switched to in several steps, none of which a kill
// spares: each stager journals the step it is at in the var directory of its
// resource, so the next start of the resource, or the next stage, finishes
// the commit or discards the stage, as the step says.

type (
	// HeadStageJournal is what a stager records of its stage.
	HeadStageJournal struct {
		// Name is the name of the stage, as the run restored into it.
		Name string `json:"name"`

		// SessionID is the session of the action staging.
		SessionID string `json:"session_id"`

		// Step is how far the stage went: HeadStageStaged, which a
		// recovery discards, HeadStageCommit, which it finishes, and
		// HeadStageCleanup, the head switched, what is left of the
		// former generation still to drop.
		Step string `json:"step"`

		// PID is the process staging, and PIDStart when it started, in
		// clock ticks since the boot, telling it from another process
		// given its pid since: a recovery leaves the stage of a process
		// still running alone.
		PID      int    `json:"pid,omitempty"`
		PIDStart uint64 `json:"pid_start,omitempty"`

		// Props are what the stager needs to finish its commit, read
		// while the former generation was there to read them from.
		Props map[string]string `json:"props,omitempty"`
	}
)

const (
	HeadStageStaged  = "staged"
	HeadStageCommit  = "commit"
	HeadStageCleanup = "cleanup"

	// headStageName is the name of the journal, in the var directory of
	// the resource holding the head.
	headStageName = "head_stage.json"
)

// ErrHeadStageBusy is returned by a recovery finding the stage of a process
// still running.
var ErrHeadStageBusy = errors.New("the head is being staged by a running action")

// NewHeadStageJournal returns the journal of a stage named name this process
// opens, at the step HeadStageStaged.
func NewHeadStageJournal(name string) *HeadStageJournal {
	pid := os.Getpid()
	return &HeadStageJournal{
		Name:      name,
		SessionID: xsession.SessionID().String(),
		Step:      HeadStageStaged,
		PID:       pid,
		PIDStart:  processStart(pid),
	}
}

// ReadHeadStageJournal returns the journal in varDir, nil when there is none.
func ReadHeadStageJournal(varDir string) (*HeadStageJournal, error) {
	p := filepath.Join(varDir, headStageName)
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var j HeadStageJournal
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return &j, nil
}

// Write records the journal in varDir. It is on the disk when Write returns:
// the step it says is the one a recovery acts on.
func (t *HeadStageJournal) Write(varDir string) error {
	if err := os.MkdirAll(varDir, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return writeFileDurably(filepath.Join(varDir, headStageName), append(b, '\n'))
}

// RemoveHeadStageJournal removes the journal in varDir: nothing is left of
// the stage.
func RemoveHeadStageJournal(varDir string) error {
	err := os.Remove(filepath.Join(varDir, headStageName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return syncDir(varDir)
}

// Busy says the stage belongs to another process, still running: the one
// that wrote the journal, not one given its pid since.
func (t *HeadStageJournal) Busy() bool {
	if t.PID <= 0 || t.PID == os.Getpid() {
		return false
	}
	start := processStart(t.PID)
	return start != 0 && start == t.PIDStart
}

// Adopt makes this process the one recovering the stage, as one read from
// where another node left it: the process the journal names ran there.
func (t *HeadStageJournal) Adopt() {
	t.PID = os.Getpid()
	t.PIDStart = processStart(t.PID)
}

// processStart returns when the process pid started, in clock ticks since
// the boot, as /proc tells, and zero when it does not run, or where nothing
// tells: a stage is then taken as left by a process gone.
func processStart(pid int) uint64 {
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0
	}
	// The command name, between parentheses, may hold spaces and
	// parentheses: the fields are counted from the last one closing.
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0
	}
	fields := strings.Fields(s[i+1:])
	// starttime is the 22nd field of the line, the 20th after the name.
	if len(fields) < 20 {
		return 0
	}
	n, _ := strconv.ParseUint(fields[19], 10, 64)
	return n
}
