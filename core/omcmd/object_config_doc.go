package omcmd

import (
	"context"
	"fmt"

	"github.com/opensvc/om3/core/commoncmd"
	"github.com/opensvc/om3/core/naming"
	"github.com/opensvc/om3/core/object"
	"github.com/opensvc/om3/core/objectaction"
)

type (
	CmdObjectConfigDoc struct {
		ObjectSelector string
		Color          string
		Output         string
		Keyword        string
		Driver         string
		Depth          int
	}
)

func (t *CmdObjectConfigDoc) Run(kind string) error {
	mergedSelector := commoncmd.MergeSelector("", t.ObjectSelector, kind, "")
	if t.ObjectSelector != "" {
		return objectaction.New(
			objectaction.LocalFirst(),
			objectaction.WithColor(t.Color),
			objectaction.WithOutput(t.Output),
			objectaction.WithObjectSelector(mergedSelector),
			objectaction.WithLocalFunc(func(ctx context.Context, p naming.Path) (interface{}, error) {
				o, err := object.New(p, object.WithConfigFile(""))
				if err != nil {
					return nil, err
				}
				c, ok := o.(object.Configurer)
				if !ok {
					return nil, fmt.Errorf("%s is not a configurer", o)
				}
				return c.Doc(t.Driver, t.Keyword, t.Depth)
			}),
		).Do()
	}
	var (
		c   object.Configurer
		err error
	)
	switch kind {
	case "svc":
		c, err = object.NewSvc(naming.Path{})
	case "vol":
		c, err = object.NewVol(naming.Path{})
	case "usr":
		c, err = object.NewUsr(naming.Path{})
	case "sec":
		c, err = object.NewSec(naming.Path{})
	case "cfg":
		c, err = object.NewCfg(naming.Path{})
	case "ccfg":
		c, err = object.NewCluster(object.WithConfigFile(""))
	default:
		return fmt.Errorf("unknown kind %s", kind)
	}

	if err != nil {
		return err
	}
	buff, err := c.Doc(t.Driver, t.Keyword, t.Depth)
	if err != nil {
		return err
	}
	fmt.Print(buff)
	return nil
}
