package omcmd

import (
	"strings"

	"github.com/opensvc/om3/core/commoncmd"
	"github.com/opensvc/om3/core/instance"
	"github.com/opensvc/om3/core/objectaction"
)

type (
	CmdObjectSwitch struct {
		OptsGlobal
		commoncmd.OptsAsync
		To   string
		Live bool
	}
)

func (t *CmdObjectSwitch) Run(kind string) error {
	mergedSelector := commoncmd.MergeSelector("", t.ObjectSelector, kind, "")
	target := instance.MonitorGlobalExpectPlacedAt.String()
	options := instance.MonitorGlobalExpectOptionsPlacedAt{
		Live: t.Live,
	}
	if t.To != "" {
		options.Destination = strings.Split(t.To, ",")
	} else {
		options.Destination = []string{}
	}
	return objectaction.New(
		objectaction.WithObjectSelector(mergedSelector),
		objectaction.WithOutput(t.Output),
		objectaction.WithColor(t.Color),
		objectaction.WithAsyncTarget(target),
		objectaction.WithAsyncTargetOptions(options),
		objectaction.WithAsyncTime(t.Time),
		objectaction.WithAsyncWait(t.Wait),
		objectaction.WithAsyncWatch(t.Watch),
	).Do()
}
