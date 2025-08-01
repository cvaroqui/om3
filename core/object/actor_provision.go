package object

import (
	"context"
	"fmt"

	"github.com/opensvc/om3/core/actioncontext"
	"github.com/opensvc/om3/core/resource"
	"github.com/opensvc/om3/util/key"
)

// Provision allocates and starts the local instance of the object
func (t *actor) Provision(ctx context.Context) error {
	ctx2 := actioncontext.WithProps(ctx, actioncontext.Provision)
	if err := t.validateAction(); err != nil {
		return err
	}
	provision := t.config.GetBool(key.New("", "provision"))
	if !provision {
		return fmt.Errorf("provision is disabled: make sure all resources have been provisioned by a sysadmin and execute 'instance provision --state-only")
	}
	t.setenv("provision", actioncontext.IsLeader(ctx2))
	unlock, err := t.lockAction(ctx2)
	if err != nil {
		return err
	}
	defer unlock()
	if err := t.lockedProvision(ctx2); err != nil {
		return err
	}
	if actioncontext.IsRollbackDisabled(ctx2) {
		// --disable-rollback handling
		return nil
	}
	ctx2 = actioncontext.WithProps(ctx, actioncontext.Stop)
	return t.lockedStop(ctx2)
}

func (t *actor) lockedProvision(ctx context.Context) error {
	return t.action(ctx, func(ctx context.Context, r resource.Driver) error {
		rid := r.RID()
		t.log.Attr("rid", rid).Debugf("%s: provision resource", rid)
		leader := actioncontext.IsLeader(ctx)
		return resource.Provision(ctx, r, leader)
	})
}
