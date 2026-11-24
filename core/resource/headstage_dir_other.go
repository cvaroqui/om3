//go:build !linux

package resource

import (
	"context"

	"github.com/opensvc/om3/v3/util/plog"
)

// DirStager stages a head that is a directory of a filesystem, where the
// system exchanges two names in one rename: not here.
type DirStager struct {
	VarDir string
	Dir    string
	Log    *plog.Logger
}

// Stage opens no stage.
func (t *DirStager) Stage(ctx context.Context, name string) (HeadStage, bool, error) {
	return HeadStage{}, false, nil
}

// Recover finds nothing to recover.
func (t *DirStager) Recover(ctx context.Context) error {
	return nil
}

// Resume finds no stage to resume.
func (t *DirStager) Resume(ctx context.Context, name string) (HeadStage, error) {
	return HeadStage{}, ErrHeadStageDiscarded
}

// IsMountRoot says dir is the root of a mount, where the system tells: not
// here.
func IsMountRoot(dir string) (bool, error) {
	return false, nil
}

// mountsUnder returns the mount points under dir, where the system tells:
// not here.
func mountsUnder(dir string) ([]string, error) {
	return nil, nil
}
