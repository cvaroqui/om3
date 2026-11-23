package resfsdir

import (
	"context"

	"github.com/opensvc/om3/v3/core/resource"
)

// StageHead implements resource.HeadStager: a sibling of the directory, on
// its filesystem, the commit exchanges with it in one rename. A directory
// given a size stages none: the project its quota counts is stamped on the
// directory, which a sibling is not of.
func (t *T) StageHead(ctx context.Context, name string) (resource.HeadStage, bool, error) {
	if t.isQuotaBacked() {
		return resource.HeadStage{}, false, nil
	}
	stager, err := t.headStager(ctx)
	if err != nil {
		return resource.HeadStage{}, false, err
	}
	return stager.Stage(ctx, name)
}

// RecoverHeadStage implements resource.HeadStager.
func (t *T) RecoverHeadStage(ctx context.Context) error {
	// No journal, nothing to recover: the volume holding the directory
	// is not asked for.
	if j, err := resource.ReadHeadStageJournal(t.VarDir()); err != nil || j == nil {
		return err
	}
	stager, err := t.headStager(ctx)
	if err != nil {
		return err
	}
	return stager.Recover(ctx)
}

// headStager returns the stager of the directory, which the volume holding
// it must be available for: the stage is in it, beside the directory.
func (t *T) headStager(ctx context.Context) (*resource.DirStager, error) {
	p, err := t.dirPath(ctx, true)
	if err != nil {
		return nil, err
	}
	return &resource.DirStager{VarDir: t.VarDir(), Dir: p, Log: t.Log()}, nil
}
