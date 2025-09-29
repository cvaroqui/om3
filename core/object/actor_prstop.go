package object

import (
	"context"

	"github.com/opensvc/om3/core/actioncontext"
	"github.com/opensvc/om3/core/resource"
)

// PRStop stops the exclusive write access to devices of the local instance of the object
func (t *actor) PRStop(ctx context.Context) error {
	ctx = actioncontext.WithProps(ctx, actioncontext.PRStop)
	if err := t.validateAction(); err != nil {
		return err
	}
	t.setenv("start", false)
	unlock, err := t.lockAction(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	return t.lockedPRStop(ctx)
}

func (t *actor) lockedPRStop(ctx context.Context) error {
	return t.action(ctx, func(ctx context.Context, r resource.Driver) error {
		return resource.PRStop(ctx, r)
	})
}
