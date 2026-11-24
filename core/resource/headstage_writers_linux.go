package resource

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/opensvc/om3/v3/util/plog"
)

// A stage held for a transfer from another node is written by a process of
// this node, the receiver of the transfer, under no lock: the start of the
// head discarding the stage does not wait for it, nor for the node sending.
// It ends the processes working in the stage, which would otherwise keep it
// busy, or write on into what is being removed.

// stageWritersTimeout is how long the processes killed are waited for.
const stageWritersTimeout = 10 * time.Second

// KillHeadStageWriters kills the processes whose working directory, or an
// open file, is in the directory dir of a stage, and waits for them to be
// gone. It returns an error when some are still there.
func KillHeadStageWriters(dir string, log *plog.Logger) error {
	dir = filepath.Clean(dir)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	// It kills as root whatever works in dir: a dir that is no stage,
	// as the live head or the root, by a mistake of its caller, would
	// have it kill the service, or the node.
	if !IsHeadStageDir(dir) {
		return fmt.Errorf("refuse to end the processes working in %s: not the directory of a stage", dir)
	}
	deadline := time.Now().Add(stageWritersTimeout)
	for {
		pids := processesIn(dir)
		if len(pids) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("processes still work in the stage %s: pids %v", dir, pids)
		}
		for _, pid := range pids {
			if log != nil {
				log.Infof("kill the process %d, working in the discarded stage %s", pid, dir)
			}
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// processesIn returns the processes, this one apart, whose working directory
// or an open file is dir or under it, as /proc tells.
// IsHeadStageDir says dir is the directory of a stage, as the stagers name
// them: head_stage in the var directory of a resource, or a sibling of the
// head named after it with the .om-stage suffix.
func IsHeadStageDir(dir string) bool {
	if !filepath.IsAbs(dir) || dir == "/" {
		return false
	}
	base := filepath.Base(dir)
	return base == "head_stage" || (strings.HasSuffix(base, ".om-stage") && base != ".om-stage")
}

func processesIn(dir string) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	in := func(link string) bool {
		p, err := os.Readlink(link)
		if err != nil {
			return false
		}
		// The link of a file removed since says so after its path.
		p = strings.TrimSuffix(p, " (deleted)")
		return p == dir || strings.HasPrefix(p, dir+"/")
	}
	self := os.Getpid()
	pids := make([]int, 0)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == self {
			continue
		}
		proc := filepath.Join("/proc", entry.Name())
		if in(filepath.Join(proc, "cwd")) {
			pids = append(pids, pid)
			continue
		}
		fds, err := os.ReadDir(filepath.Join(proc, "fd"))
		if err != nil {
			continue
		}
		for _, fd := range fds {
			if in(filepath.Join(proc, "fd", fd.Name())) {
				pids = append(pids, pid)
				break
			}
		}
	}
	return pids
}
