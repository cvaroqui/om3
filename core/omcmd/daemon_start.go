package omcmd

import (
	"context"

	"github.com/opensvc/om3/core/client"
	"github.com/opensvc/om3/daemon/daemoncmd"
)

type (
	CmdDaemonStart struct {
		OptsGlobal
		CPUProfile string
	}
)

func (t *CmdDaemonStart) Run() error {
	cli, err := client.New()
	if err != nil {
		return err
	}
	ctx := context.Background()
	return daemoncmd.NewContext(ctx, cli).StartFromCmd(ctx, false, t.CPUProfile)
}
