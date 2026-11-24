//go:build !linux

package resource

import "github.com/opensvc/om3/v3/util/plog"

// KillHeadStageWriters kills the processes working in the directory of a
// stage, where the system tells them: not here.
func KillHeadStageWriters(dir string, log *plog.Logger) error {
	return nil
}
