package commands

import (
	"github.com/spf13/cobra"
	"opensvc.com/opensvc/core/entrypoints/nodeaction"
	"opensvc.com/opensvc/core/flag"
	"opensvc.com/opensvc/core/object"
)

type (
	// CmdNodeComplianceAttachRuleset is the cobra flag set of the sysreport command.
	CmdNodeComplianceAttachRuleset struct {
		object.OptsNodeComplianceAttachRuleset
	}
)

// Init configures a cobra command and adds it to the parent command.
func (t *CmdNodeComplianceAttachRuleset) Init(parent *cobra.Command) {
	cmd := t.cmd()
	parent.AddCommand(cmd)
	flag.Install(cmd, &t.OptsNodeComplianceAttachRuleset)
}

func (t *CmdNodeComplianceAttachRuleset) cmd() *cobra.Command {
	return &cobra.Command{
		Use:     "ruleset",
		Short:   "attach compliance ruleset to this node.",
		Long:    "rules of attached rulesets are made available to their module.",
		Aliases: []string{"rulese", "rules", "rule", "rul", "ru"},
		Run: func(_ *cobra.Command, _ []string) {
			t.run()
		},
	}
}

func (t *CmdNodeComplianceAttachRuleset) run() {
	nodeaction.New(
		nodeaction.WithLocal(t.Global.Local),
		nodeaction.WithRemoteNodes(t.Global.NodeSelector),
		nodeaction.WithFormat(t.Global.Format),
		nodeaction.WithColor(t.Global.Color),
		nodeaction.WithServer(t.Global.Server),
		nodeaction.WithRemoteAction("compliance attach ruleset"),
		nodeaction.WithRemoteOptions(map[string]interface{}{
			"format": t.Global.Format,
		}),
		nodeaction.WithLocalRun(func() (interface{}, error) {
			return object.NewNode().ComplianceAttachRuleset(t.OptsNodeComplianceAttachRuleset)
		}),
	).Do()
}