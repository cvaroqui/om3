package oxcmd

import (
	"github.com/opensvc/om3/core/commoncmd"
	"github.com/opensvc/om3/core/objectaction"
)

type (
	CmdObjectComplianceShowModuleset struct {
		OptsGlobal
		Moduleset string
	}
)

func (t *CmdObjectComplianceShowModuleset) Run(kind string) error {
	mergedSelector := commoncmd.MergeSelector("", t.ObjectSelector, kind, "")
	return objectaction.New(
		objectaction.WithColor(t.Color),
		objectaction.WithOutput(t.Output),
		objectaction.WithObjectSelector(mergedSelector),
	).Do()
}
