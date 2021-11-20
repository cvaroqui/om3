package commands

import (
	"github.com/spf13/cobra"
	"opensvc.com/opensvc/core/entrypoints/nodeaction"
	"opensvc.com/opensvc/core/flag"
	"opensvc.com/opensvc/core/object"
)

type (
	// NodeDoc is the cobra flag set of the node doc command.
	NodeDoc struct {
		object.OptsDoc
	}
)

// Init configures a cobra command and adds it to the parent command.
func (t *NodeDoc) Init(parent *cobra.Command) {
	cmd := t.cmd()
	parent.AddCommand(cmd)
	flag.Install(cmd, &t.OptsDoc)
}

func (t *NodeDoc) cmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doc",
		Short: "print the documentation of the selected keywords",
		Run: func(_ *cobra.Command, _ []string) {
			t.run()
		},
	}
}

func (t *NodeDoc) run() {
	nodeaction.New(
		nodeaction.WithFormat(t.Global.Format),
		nodeaction.WithColor(t.Global.Color),
		nodeaction.WithServer(t.Global.Server),

		nodeaction.WithRemoteNodes(t.Global.NodeSelector),
		nodeaction.WithRemoteAction("node doc"),
		nodeaction.WithRemoteOptions(map[string]interface{}{
			"kw":     t.Keyword,
			"driver": t.Driver,
		}),

		nodeaction.WithLocal(t.Global.Local),
		nodeaction.WithLocalRun(func() (interface{}, error) {
			return object.NewNode().Doc(t.OptsDoc)
		}),
	).Do()
}