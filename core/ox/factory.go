package ox

import (
	// Necessary to use go:embed
	_ "embed"

	"github.com/spf13/cobra"

	"github.com/opensvc/om3/core/commoncmd"
	commands "github.com/opensvc/om3/core/oxcmd"
	"github.com/opensvc/om3/core/tui"
)

var (
	//go:embed text/node-events/event-kind
	eventKindTemplate string
)

func newCmdAll() *cobra.Command {
	return &cobra.Command{
		Use:   "all",
		Short: "manage a mix of objects, tentatively exposing all commands",
	}
}

func newCmdCcfg() *cobra.Command {
	return &cobra.Command{
		Use:   "ccfg",
		Short: "manage the cluster shared configuration",
		Long: `The cluster nodes merge their private configuration
over the cluster shared configuration.

The shared configuration is hosted in a ccfg-kind object, and is
replicated using the same rules as other kinds of object (last write is
eventually replicated).
`,
	}
}

func newCmdCfg() *cobra.Command {
	return &cobra.Command{
		Use:   "cfg",
		Short: "manage configmaps",
		Long: `A configmap is an unencrypted key-value store.

Values can be binary or text.

A key can be installed as a file in a Vol, then exposed to apps
and containers.

A key can be exposed as a environment variable for apps and
containers.

A signal can be sent to consumer processes upon exposed key value
changes.

The key names can include the '/' character, interpreted as a path separator
when installing the key in a volume.`,
	}
}

func newCmdSec() *cobra.Command {
	return &cobra.Command{
		Use:   "sec",
		Short: "manage secrets",
		Long: `A secret is an encrypted key-value store.

Values can be binary or text.

A key can be installed as a file in a Vol, then exposed to apps
and containers.

A key can be exposed as a environment variable for apps and
containers.

A signal can be sent to consumer processes upon exposed key value
changes.

The key names can include the '/' character, interpreted as a path separator
when installing the key in a volume.`,
	}
}

func newCmdSVC() *cobra.Command {
	return &cobra.Command{
		Use:   "svc",
		Short: "manage services",
		Long: `Service objects subsystem.
	
A service is typically made of ip, app, container and task resources.

They can use support objects like volumes, secrets and configmaps to
isolate lifecycles or to abstract cluster-specific knowledge.
`,
	}
}

func newCmdVol() *cobra.Command {
	return &cobra.Command{
		Use:   "vol",
		Short: "manage volumes",
		Long: `A volume is a persistent data provider.

A volume is made of disk, fs and sync resources. It is created by a pool,
to satisfy a demand from a volume resource in a service.

Volumes and their subdirectories can be mounted inside containers.

A volume can host cfg and sec keys projections.`,
	}
}

func newCmdUsr() *cobra.Command {
	return &cobra.Command{
		Use:   "usr",
		Short: "manage users",
		Long: `A user stores the grants and credentials of user of the agent API.

User objects are not necessary with OpenID authentication, as the
grants are embedded in the trusted bearer tokens.`,
	}
}

func newCmdArrayList() *cobra.Command {
	var options commands.CmdArrayList
	cmd := &cobra.Command{
		Aliases: []string{"ls"},
		Use:     "list",
		Short:   "list the cluster-managed storage arrays",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	return cmd
}

func newCmdDaemonRestart() *cobra.Command {
	var options commands.CmdDaemonRestart
	cmd := &cobra.Command{
		Use:     "restart",
		Short:   "restart the daemon",
		Long:    "restart the daemon. Operation is asynchronous when node selector is used",
		Aliases: []string{"restart"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdDaemonShutdown() *cobra.Command {
	var options commands.CmdDaemonShutdown
	cmd := &cobra.Command{
		Use:   "shutdown",
		Short: "shutdown all local svc and vol objects then shutdown the daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagDuration(flags, &options.Timeout)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdDaemonStop() *cobra.Command {
	var options commands.CmdDaemonStop
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "stop the daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectKeyAdd(kind string) *cobra.Command {
	var options commands.CmdObjectKeyAdd
	var from, value string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "add new keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flag("from").Changed {
				options.From = &from
			}
			if cmd.Flag("value").Changed {
				options.Value = &value
			}
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagFrom(flags, &from)
	commoncmd.FlagKeyName(flags, &options.Name)
	commoncmd.FlagKeyValue(flags, &value)
	cmd.MarkFlagsMutuallyExclusive("from", "value")
	return cmd
}

func newCmdObjectKeyChange(kind string) *cobra.Command {
	var options commands.CmdObjectKeyChange
	var from, value string
	cmd := &cobra.Command{
		Use:   "change",
		Short: "change existing keys value",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flag("from").Changed {
				options.From = &from
			}
			if cmd.Flag("value").Changed {
				options.Value = &value
			}
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagFrom(flags, &from)
	commoncmd.FlagKeyName(flags, &options.Name)
	commoncmd.FlagKeyValue(flags, &value)
	cmd.MarkFlagsMutuallyExclusive("from", "value")
	return cmd
}

func newCmdObjectKeyDecode(kind string) *cobra.Command {
	var options commands.CmdObjectKeyDecode
	cmd := &cobra.Command{
		Use:   "decode",
		Short: "decode a key value",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagKeyName(flags, &options.Name)
	return cmd
}

func newCmdObjectKeyEdit(kind string) *cobra.Command {
	var options commands.CmdObjectKeyEdit
	cmd := &cobra.Command{
		Use:     "edit",
		Short:   "edit a key value",
		Aliases: []string{"ed"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagKeyName(flags, &options.Name)
	return cmd
}

func newCmdObjectKeyInstall(kind string) *cobra.Command {
	var options commands.CmdObjectKeyInstall
	cmd := &cobra.Command{
		Use:   "install",
		Short: "install keys as files in volumes",
		Long:  "Keys of sec and cfg can be projected to volumes via the configs and secrets keywords of volume resources. When a key value change all projections are automatically refreshed. This command triggers manually the same operations.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	commoncmd.FlagKeyName(flags, &options.Name)
	return cmd
}

func newCmdObjectKeyList(kind string) *cobra.Command {
	var options commands.CmdObjectKeyList
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "list the keys",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagMatch(flags, &options.Match)
	return cmd
}

func newCmdObjectKeyRemove(kind string) *cobra.Command {
	var options commands.CmdObjectKeyRemove
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "remove a key",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagKeyName(flags, &options.Name)
	return cmd
}

func newCmdObjectKeyRename(kind string) *cobra.Command {
	var options commands.CmdObjectKeyRename
	cmd := &cobra.Command{
		Use:   "rename",
		Short: "rename a key",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagKeyName(flags, &options.Name)
	commoncmd.FlagKeyTo(flags, &options.To)
	return cmd
}

func newCmdNetworkList() *cobra.Command {
	var options commands.CmdNetworkList
	cmd := &cobra.Command{
		Aliases: []string{"ls"},
		Use:     "list",
		Short:   "list the cluster networks",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	return cmd
}

func newCmdNetworkSetup() *cobra.Command {
	var options commands.CmdNetworkSetup
	cmd := &cobra.Command{
		Use:     "setup",
		Short:   "configure the cluster networks on the node",
		Long:    "Most cluster network drivers need ip routes, ip rules, tunnels and firewall rules. This command sets them up, the same as done on daemon startup and daemon reconfiguration via configuration change.",
		Aliases: []string{"set"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	return cmd
}

func newCmdNetworkIPList() *cobra.Command {
	var options commands.CmdNetworkIPList
	cmd := &cobra.Command{
		Aliases: []string{"ls"},
		Use:     "list",
		Short:   "list the ip in the cluster networks",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNetworkStatusName(flags, &options.Name)
	return cmd
}

func newCmdNodeAbort() *cobra.Command {
	var options commands.CmdNodeAbort
	cmd := &cobra.Command{
		GroupID: commoncmd.GroupIDOrchestratedActions,
		Use:     "abort",
		Short:   "abort the running orchestration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	commoncmd.FlagsAsync(flags, &options.OptsAsync)
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeCapabilitiesList() *cobra.Command {
	var options commands.CmdNodeCapabilitiesList
	cmd := &cobra.Command{
		Aliases: []string{"ls"},
		Use:     "list",
		Short:   "list the node capabilities",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeCapabilitiesScan() *cobra.Command {
	var options commands.CmdNodeCapabilitiesScan
	cmd := &cobra.Command{
		Use:     "scan",
		Short:   "scan the node capabilities",
		Aliases: []string{"sca", "sc"},
		Long: `Scan the node for capabilities.

Capabilities are normally scanned at daemon startup and when the installed 
system packages change, so admins only have to use this when they want manually 
installed software to be discovered without restarting the daemon.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeChecks() *cobra.Command {
	var options commands.CmdNodeChecks
	cmd := &cobra.Command{
		Use:     "checks",
		Short:   "run the checks, push and print the result",
		Aliases: []string{"check", "chec", "che", "ch"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeClear() *cobra.Command {
	var options commands.CmdNodeClear
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "reset the monitor state to idle",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeCollectorTagAttach() *cobra.Command {
	var attachData string
	var options commands.CmdNodeCollectorTagAttach
	cmd := &cobra.Command{
		Use:     "attach",
		Short:   "attach a tag to this node",
		Long:    "The tag must already exist in the collector.",
		Aliases: []string{"atta", "att", "at", "a"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flag("attach-data").Changed {
				options.AttachData = &attachData
			}
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	flags.StringVar(&options.Name, "name", "", "the tag name")
	flags.StringVar(&attachData, "attach-data", "", "the data stored with the tag attachment")
	return cmd
}

func newCmdNodeCollectorTagCreate() *cobra.Command {
	var (
		data    string
		exclude string
	)
	var options commands.CmdNodeCollectorTagCreate
	cmd := &cobra.Command{
		Use:   "create",
		Short: "create a new tag",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flag("data").Changed {
				options.Data = &data
			}
			if cmd.Flag("exclude").Changed {
				options.Exclude = &exclude
			}
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	flags.StringVar(&options.Name, "name", "", "the tag name")
	flags.StringVar(&data, "data", "", "the data stored with the tag")
	flags.StringVar(&exclude, "exclude", "", "a pattern to prevent attachment of incompatible tags")
	return cmd
}

func newCmdNodeCollectorTagDetach() *cobra.Command {
	var options commands.CmdNodeCollectorTagDetach
	cmd := &cobra.Command{
		Use:     "detach",
		Short:   "detach a tag from this node",
		Aliases: []string{"deta", "det", "de", "d"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	flags.StringVar(&options.Name, "name", "", "the tag name")
	return cmd
}

func newCmdNodeCollectorTagList() *cobra.Command {
	var options commands.CmdNodeCollectorTagList
	cmd := &cobra.Command{
		Aliases: []string{"ls"},
		Use:     "list",
		Short:   "list available tags",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	return cmd
}

func newCmdNodeCollectorTagShow() *cobra.Command {
	var options commands.CmdNodeCollectorTagShow
	cmd := &cobra.Command{
		Use:     "show",
		Short:   "show tags attached to this node",
		Aliases: []string{"sho", "sh", "s"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	flags.BoolVar(&options.Verbose, "verbose", false, "also show the attach data")
	return cmd
}

func newCmdNodeComplianceAttachModuleset() *cobra.Command {
	var options commands.CmdNodeComplianceAttachModuleset
	cmd := &cobra.Command{
		Use:     "moduleset",
		Short:   "attach modulesets to this node",
		Long:    "Modules of all attached modulesets are checked on schedule.",
		Aliases: []string{"modulese", "modules", "module", "modul", "modu", "mod", "mo"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagModuleset(flags, &options.Moduleset)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeComplianceAttachRuleset() *cobra.Command {
	var options commands.CmdNodeComplianceAttachRuleset
	cmd := &cobra.Command{
		Use:     "ruleset",
		Short:   "attach rulesets to this node",
		Long:    "Rules of attached rulesets are exposed to modules.",
		Aliases: []string{"rulese", "rules", "rule", "rul", "ru"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagRuleset(flags, &options.Ruleset)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeComplianceAuto() *cobra.Command {
	var options commands.CmdNodeComplianceAuto
	cmd := &cobra.Command{
		Use:   "auto",
		Short: "run modules fixes or checks",
		Long:  "If the module is has the 'autofix' property set, do a fix, else do a check.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagModule(flags, &options.Module)
	commoncmd.FlagModuleset(flags, &options.Moduleset)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	commoncmd.FlagComplianceAttach(flags, &options.Attach)
	commoncmd.FlagComplianceForce(flags, &options.Force)
	return cmd
}

func newCmdNodeComplianceCheck() *cobra.Command {
	var options commands.CmdNodeComplianceCheck
	cmd := &cobra.Command{
		Use:     "check",
		Short:   "run modules checks",
		Aliases: []string{"chec", "che", "ch"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagModule(flags, &options.Module)
	commoncmd.FlagModuleset(flags, &options.Moduleset)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	commoncmd.FlagComplianceAttach(flags, &options.Attach)
	commoncmd.FlagComplianceForce(flags, &options.Force)
	return cmd
}

func newCmdNodeComplianceFix() *cobra.Command {
	var options commands.CmdNodeComplianceFix
	cmd := &cobra.Command{
		Use:   "fix",
		Short: "run modules fixes",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagModule(flags, &options.Module)
	commoncmd.FlagModuleset(flags, &options.Moduleset)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	commoncmd.FlagComplianceAttach(flags, &options.Attach)
	commoncmd.FlagComplianceForce(flags, &options.Force)
	return cmd
}

func newCmdNodeComplianceFixable() *cobra.Command {
	var options commands.CmdNodeComplianceFixable
	cmd := &cobra.Command{
		Use:     "fixable",
		Short:   "run modules fixable-tests",
		Aliases: []string{"fixabl", "fixab", "fixa"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagModule(flags, &options.Module)
	commoncmd.FlagModuleset(flags, &options.Moduleset)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	commoncmd.FlagComplianceAttach(flags, &options.Attach)
	commoncmd.FlagComplianceForce(flags, &options.Force)
	return cmd
}

func newCmdNodeComplianceDetachModuleset() *cobra.Command {
	var options commands.CmdNodeComplianceDetachModuleset
	cmd := &cobra.Command{
		Use:     "moduleset",
		Short:   "detach modulesets from this node",
		Long:    "Modules of attached modulesets are checked on schedule.",
		Aliases: []string{"modulese", "modules", "module", "modul", "modu", "mod", "mo"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagModuleset(flags, &options.Moduleset)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeComplianceDetachRuleset() *cobra.Command {
	var options commands.CmdNodeComplianceDetachRuleset
	cmd := &cobra.Command{
		Use:     "ruleset",
		Short:   "detach rulesets from this node",
		Long:    "Rules of attached rulesets are made available to their module.",
		Aliases: []string{"rulese", "rules", "rule", "rul", "ru"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagRuleset(flags, &options.Ruleset)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeComplianceEnv() *cobra.Command {
	var options commands.CmdNodeComplianceEnv
	cmd := &cobra.Command{
		Use:     "env",
		Short:   "show the env variables set for modules run",
		Aliases: []string{"en"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagModuleset(flags, &options.Moduleset)
	commoncmd.FlagModule(flags, &options.Module)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeComplianceListModules() *cobra.Command {
	var options commands.CmdNodeComplianceListModules
	cmd := &cobra.Command{
		Use:     "modules",
		Short:   "list modules available on this node",
		Aliases: []string{"module", "modul", "modu", "mod"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeComplianceListModuleset() *cobra.Command {
	var options commands.CmdNodeComplianceListModuleset
	cmd := &cobra.Command{
		Use:     "moduleset",
		Short:   "list modulesets available to this node",
		Aliases: []string{"modulesets"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagModuleset(flags, &options.Moduleset)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeComplianceListRuleset() *cobra.Command {
	var options commands.CmdNodeComplianceListRuleset
	cmd := &cobra.Command{
		Use:     "ruleset",
		Short:   "list rulesets available to this node",
		Aliases: []string{"rulese", "rules", "rule", "rul", "ru"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagRuleset(flags, &options.Ruleset)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeComplianceShowModuleset() *cobra.Command {
	var options commands.CmdNodeComplianceShowModuleset
	cmd := &cobra.Command{
		Use:     "moduleset",
		Short:   "show modulesets and modules attached to this node",
		Aliases: []string{"modulese", "modules", "module", "modul", "modu", "mod", "mo"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagModuleset(flags, &options.Moduleset)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeComplianceShowRuleset() *cobra.Command {
	var options commands.CmdNodeComplianceShowRuleset
	cmd := &cobra.Command{
		Use:     "ruleset",
		Short:   "show rules contextualized for this node",
		Aliases: []string{"rulese", "rules", "rule", "rul", "ru"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeDrain() *cobra.Command {
	var options commands.CmdNodeDrain
	cmd := &cobra.Command{
		GroupID: commoncmd.GroupIDOrchestratedActions,
		Use:     "drain",
		Short:   "freeze node and shutdown all its object instances",
		Long:    "If not specified with --node, the local node is selected for drain.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsAsync(flags, &options.OptsAsync)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeDrivers() *cobra.Command {
	var options commands.CmdNodeDrivers
	cmd := &cobra.Command{
		Use:     "drivers",
		Short:   "list builtin drivers",
		Aliases: []string{"driver", "drive", "driv", "drv", "dr"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodePing() *cobra.Command {
	var options commands.CmdNodePing
	cmd := &cobra.Command{
		Use:   "ping",
		Short: "ask node to ping all cluster nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeScheduleList() *cobra.Command {
	var options commands.CmdNodeScheduleList
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "list the node scheduler entries",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeSystem() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "system",
		Short: "node system commands",
	}
	return cmd
}

func newCmdNodeSystemDisk() *cobra.Command {
	var options commands.CmdNodeSystemDisk
	cmd := &cobra.Command{
		Use:     "disk",
		Short:   "show node system disks",
		Aliases: []string{"dsk", "dis"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeSystemGroup() *cobra.Command {
	var options commands.CmdNodeSystemGroup
	cmd := &cobra.Command{
		Use:     "group",
		Short:   "show node system groups",
		Aliases: []string{"grp", "gr"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeSystemHardware() *cobra.Command {
	var options commands.CmdNodeSystemHardware
	cmd := &cobra.Command{
		Use:     "hardware",
		Short:   "show node system hardware",
		Aliases: []string{"device", "hard"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeSystemIPAddress() *cobra.Command {
	var options commands.CmdNodeSystemIPAddress
	cmd := &cobra.Command{
		Use:     "ipaddress",
		Short:   "show node system IP address",
		Aliases: []string{"addr", "ipaddr"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeSystemPackage() *cobra.Command {
	var options commands.CmdNodeSystemPackage
	cmd := &cobra.Command{
		Use:     "package",
		Short:   "show node system package",
		Aliases: []string{"pkg"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeSystemPatch() *cobra.Command {
	var options commands.CmdNodeSystemPatch
	cmd := &cobra.Command{
		Use:     "patch",
		Short:   "show node system patch",
		Aliases: []string{"pacth"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeSystemProperty() *cobra.Command {
	var options commands.CmdNodeSystemProperty
	cmd := &cobra.Command{
		Use:     "property",
		Short:   "show node system property",
		Aliases: []string{"proper", "prop"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeSystemSAN() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "san",
		Short: "node system san commands",
	}
	return cmd
}

func newCmdNodeSystemSANPathInitiator() *cobra.Command {
	var options commands.CmdNodeSystemInitiator
	cmd := &cobra.Command{
		Use:     "initiator",
		Short:   "show node system san initiator",
		Aliases: []string{"init", "ini"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeSystemSANPath() *cobra.Command {
	var options commands.CmdNodeSystemSANPath
	cmd := &cobra.Command{
		Use:     "path",
		Short:   "show node system san path",
		Aliases: []string{"pa"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeSystemUser() *cobra.Command {
	var options commands.CmdNodeSystemUser
	cmd := &cobra.Command{
		Use:     "user",
		Short:   "show node system users",
		Aliases: []string{"usr", "us"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeConfigEdit() *cobra.Command {
	var options commands.CmdNodeConfigEdit
	cmd := &cobra.Command{
		Use:     "edit",
		Short:   "edit the node configuration",
		Aliases: []string{"ed", "edi"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagDiscard(flags, &options.Discard)
	commoncmd.FlagRecover(flags, &options.Recover)
	cmd.MarkFlagsMutuallyExclusive("discard", "recover")
	return cmd
}

func newCmdNodeConfigEval() *cobra.Command {
	var options commands.CmdNodeConfigGet
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "evaluate a configuration key value",
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Eval = true
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagImpersonate(flags, &options.Impersonate)
	commoncmd.FlagKeywords(flags, &options.Keywords)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeConfigGet() *cobra.Command {
	var options commands.CmdNodeConfigGet
	cmd := &cobra.Command{
		Use:   "get",
		Short: "get a configuration key value",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagEval(flags, &options.Eval)
	commoncmd.FlagImpersonate(flags, &options.Impersonate)
	commoncmd.FlagKeywords(flags, &options.Keywords)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeConfigShow() *cobra.Command {
	var options commands.CmdNodeConfigShow
	cmd := &cobra.Command{
		Use:   "show",
		Short: "show the node configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	commoncmd.FlagSections(flags, &options.Sections)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeConfigUpdate() *cobra.Command {
	var options commands.CmdNodeConfigUpdate
	cmd := commoncmd.NewCmdAnyConfigUpdate()
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return options.Run()
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	commoncmd.FlagUpdateDelete(flags, &options.Delete)
	commoncmd.FlagUpdateSet(flags, &options.Set)
	commoncmd.FlagUpdateUnset(flags, &options.Unset)
	return cmd
}

func newCmdNodeConfigValidate() *cobra.Command {
	var options commands.CmdNodeConfigValidate
	cmd := &cobra.Command{
		Use:     "validate",
		Short:   "verify the node configuration syntax",
		Aliases: []string{"valid", "val"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	return cmd
}

func newCmdNodeEvents() *cobra.Command {
	var options commands.CmdNodeEvents
	cmd := &cobra.Command{
		Use:     "events",
		Short:   "print the node event stream",
		Long:    "print the node event stream\n\nAvailable kinds: \n" + eventKindTemplate,
		Aliases: []string{"eve", "even", "event"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	commoncmd.FlagColor(flags, &options.Color)
	commoncmd.FlagDuration(flags, &options.Duration)
	commoncmd.FlagEventFilters(flags, &options.Filters)
	commoncmd.FlagEventTemplate(flags, &options.Template)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	commoncmd.FlagObjectSelector(flags, &options.ObjectSelector)
	commoncmd.FlagOutput(flags, &options.Output)
	commoncmd.FlagQuiet(flags, &options.Quiet)
	commoncmd.FlagWait(flags, &options.Wait)
	flags.Uint64Var(&options.Limit, "limit", 0, "stop listening when <limit> events are received, the default is 0 (unlimited) or 1 if --wait is set")
	return cmd
}

func newCmdNodeFreeze() *cobra.Command {
	var options commands.CmdNodeFreeze
	cmd := &cobra.Command{
		Use:   "freeze",
		Short: "block ha automatic start and split action",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeLogs() *cobra.Command {
	var options commands.CmdNodeLogs
	cmd := &cobra.Command{
		GroupID: commoncmd.GroupIDQuery,
		Use:     "logs",
		Aliases: []string{"logs", "log", "lo"},
		Short:   "show this node logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsLogs(flags, &options.OptsLogs)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeList() *cobra.Command {
	var options commands.CmdNodeList
	cmd := &cobra.Command{
		GroupID: commoncmd.GroupIDQuery,
		Aliases: []string{"ls"},
		Use:     "list",
		Short:   "list the cluster nodes",
		Long:    "The list can be filtered using the --node selector. This command can be used to validate node selector expressions.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodePRKey() *cobra.Command {
	var options commands.CmdNodePRKey
	cmd := &cobra.Command{
		Use:     "prkey",
		Short:   "show the scsi3 persistent reservation key of this node",
		Aliases: []string{"prk", "prke"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectSchedule(kind string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "schedule",
		Short:   "object scheduler commands",
		Aliases: []string{"sched"},
	}
	return cmd
}

func newCmdObjectScheduleList(kind string) *cobra.Command {
	var options commands.CmdObjectScheduleList
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "list the object scheduler entries",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodePushAsset() *cobra.Command {
	var options commands.CmdNodePushAsset
	cmd := &cobra.Command{
		Use:     "asset",
		Short:   "run the node discovery, push and print the result",
		Aliases: []string{"asse", "ass", "as"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodePushDisk() *cobra.Command {
	var options commands.CmdNodePushDisks
	cmd := &cobra.Command{
		Use:     "disk",
		Short:   "run the disk discovery, push and print the result",
		Aliases: []string{"disks", "dis", "di"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodePushPatch() *cobra.Command {
	var options commands.CmdNodePushPatch
	cmd := &cobra.Command{
		Use:     "patch",
		Short:   "run the node installed patches discovery, push and print the result",
		Aliases: []string{"patc", "pat", "pa"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodePushPkg() *cobra.Command {
	var options commands.CmdNodePushPkg
	cmd := &cobra.Command{
		Use:     "pkg",
		Short:   "run the node installed packages discovery, push and print the result",
		Aliases: []string{"pk"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeRegister() *cobra.Command {
	var options commands.CmdNodeRegister
	cmd := &cobra.Command{
		Use:     "register",
		Short:   "initial login on the collector",
		Long:    "Obtain a registration id from the collector, store it in the node configuration node.uuid keyword. This uuid is then used to authenticate the node in collector communications.",
		Aliases: []string{"registe", "regist", "regis", "regi", "reg", "re"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagCollectorUser(flags, &options.User)
	commoncmd.FlagCollectorPassword(flags, &options.Password)
	commoncmd.FlagCollectorApp(flags, &options.App)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)

	return cmd
}

func newCmdNodeRelayStatus() *cobra.Command {
	var options commands.CmdNodeRelayStatus
	cmd := &cobra.Command{
		Use:   "status",
		Short: "show the clients and last data update time of the configured relays",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flagSet := cmd.Flags()
	addFlagsGlobal(flagSet, &options.OptsGlobal)
	commoncmd.FlagRelay(flagSet, &options.Relays)
	return cmd
}

func newCmdNodeSysreport() *cobra.Command {
	var options commands.CmdNodeSysreport
	cmd := &cobra.Command{
		Use:     "sysreport",
		Short:   "collect system data and push it to the collector",
		Long:    "Push system report to the collector for archiving and diff analysis. The --force option resend all monitored files and outputs to the collector instead of only those that changed since the last sysreport.",
		Aliases: []string{"sysrepor", "sysrepo", "sysrep", "sysre", "sysr", "sys", "sy"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagForce(flags, &options.Force)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeUnfreeze() *cobra.Command {
	var options commands.CmdNodeUnfreeze
	cmd := &cobra.Command{
		Use:     "unfreeze",
		Short:   "unblock ha automatic start and split action",
		Aliases: []string{"thaw"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeVersion() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "version",
		Short:  "display agent version",
		Hidden: true,
		Run: func(cmd *cobra.Command, args []string) {
			commands.CmdNodeVersion()
		},
	}
	return cmd
}

func newCmdObjectAbort(kind string) *cobra.Command {
	var options commands.CmdObjectAbort
	cmd := &cobra.Command{
		GroupID: commoncmd.GroupIDOrchestratedActions,
		Use:     "abort",
		Short:   "abort the running orchestration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	commoncmd.FlagsAsync(flags, &options.OptsAsync)
	addFlagsGlobal(flags, &options.OptsGlobal)
	return cmd
}

func newCmdObjectBoot(kind string) *cobra.Command {
	var options commands.CmdObjectBoot
	cmd := &cobra.Command{
		Use:    "boot",
		Hidden: true,
		Short:  "clean up actions executed on boot only",
		Long:   "SCSI reservation release, vg tags removal, ... Never execute this action manually.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeSSHTrust() *cobra.Command {
	var options commands.CmdNodeSSHTrust
	cmd := &cobra.Command{
		Use:   "trust",
		Short: "ssh-trust node peers",
		Long: "Configure the nodes specified by the --node flag to allow SSH communication from their peers." +
			" By default, the trusted SSH key is opensvc, but this can be customized using the node.sshkey setting." +
			" If the key does not exist, OpenSVC automatically generates it.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	addFlagsGlobal(flags, &options.OptsGlobal)
	return cmd
}

func newCmdObjectCertificate(kind string) *cobra.Command {
	return &cobra.Command{
		Aliases: []string{"cert", "crt"},
		Use:     "certificate",
		Short:   "create, renew, delete certificates",
	}
}

func newCmdObjectCertificateCreate(kind string) *cobra.Command {
	var options commands.CmdSecGenCert
	cmd := &cobra.Command{
		Use:   "create",
		Short: "create a certificate and store as a keyset",
		Long:  "Never change an existing private key. Only create a new certificate and renew the certificate chain.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	return cmd
}

func newCmdObjectCertificatePKCS(kind string) *cobra.Command {
	var options commands.CmdObjectCertificatePKCS
	cmd := &cobra.Command{
		Aliases: []string{"pkcs"},
		Use:     "pkcs12",
		Short:   "dump the x509 private key and certificate chain in PKCS#12 format",
		Long:    "A sec can contain a certificate, created by the `certificate create` command. The private_key, certificate and certificate_chain are stored as sec keys. The pkcs12 command decodes the private_key and certificate_chain keys, prepares and print the encrypted, password-protected PKCS#12 format. As this result is bytes-formatted, the stream should be redirected to a file.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	return cmd
}

func newCmdObjectPrint(kind string) *cobra.Command {
	return &cobra.Command{
		Use:     "print",
		Short:   "print information about the object",
		Aliases: []string{"prin", "pri", "pr"},
	}
}

func newCmdObjectPush(kind string) *cobra.Command {
	return &cobra.Command{
		Use:     "push",
		Short:   "push information about the object to the collector",
		Aliases: []string{"push", "pus", "pu"},
	}
}

func newCmdObjectCollectorTag(kind string) *cobra.Command {
	return &cobra.Command{
		Use:   "tag",
		Short: "collector tags management commands",
	}
}

func newCmdObjectCollectorTagAttach(kind string) *cobra.Command {
	var attachData string
	var options commands.CmdObjectCollectorTagAttach
	cmd := &cobra.Command{
		Use:     "attach",
		Short:   "attach a tag to this node",
		Long:    "The tag must already exist in the collector.",
		Aliases: []string{"atta", "att", "at", "a"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flag("attach-data").Changed {
				options.AttachData = &attachData
			}
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	flags.StringVar(&options.Name, "name", "", "the tag name")
	flags.StringVar(&attachData, "attach-data", "", "the data stored with the tag attachment")
	return cmd
}

func newCmdObjectCollectorTagDetach(kind string) *cobra.Command {
	var options commands.CmdObjectCollectorTagDetach
	cmd := &cobra.Command{
		Use:     "detach",
		Short:   "detach a tag from this node",
		Aliases: []string{"deta", "det", "de", "d"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	flags.StringVar(&options.Name, "name", "", "the tag name")
	return cmd
}

func newCmdObjectCollectorTagShow(kind string) *cobra.Command {
	var options commands.CmdObjectCollectorTagShow
	cmd := &cobra.Command{
		Use:     "show",
		Short:   "show tags attached to this node",
		Aliases: []string{"sho", "sh", "s"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	flags.BoolVar(&options.Verbose, "verbose", false, "also show the attach data")
	return cmd
}

func newCmdObjectComplianceAttach(kind string) *cobra.Command {
	return &cobra.Command{
		Use:     "attach",
		Short:   "attach modulesets and rulesets to the node",
		Aliases: []string{"attac", "atta", "att", "at"},
	}
}

func newCmdObjectComplianceDetach(kind string) *cobra.Command {
	return &cobra.Command{
		Use:     "detach",
		Short:   "detach modulesets and rulesets from the node",
		Aliases: []string{"detac", "deta", "det", "de"},
	}
}

func newCmdObjectComplianceList(kind string) *cobra.Command {
	return &cobra.Command{
		Aliases: []string{"ls"},
		Use:     "list",
		Short:   "list modules, modulesets and rulesets available",
	}
}

func newCmdObjectComplianceShow(kind string) *cobra.Command {
	return &cobra.Command{
		Use:     "show",
		Short:   "show current modulesets and rulesets attachments, modules last check",
		Aliases: []string{"sho", "sh", "s"},
	}
}

func newCmdObjectConfigEdit(kind string) *cobra.Command {
	var options commands.CmdObjectConfigEdit
	cmd := &cobra.Command{
		Use:     "edit",
		Short:   "edit selected object and instance configuration",
		Aliases: []string{"ed"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagDiscard(flags, &options.Discard)
	commoncmd.FlagRecover(flags, &options.Recover)
	cmd.MarkFlagsMutuallyExclusive("discard", "recover")
	return cmd
}

func newCmdObjectConfigEval(kind string) *cobra.Command {
	var options commands.CmdObjectConfigGet
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "evaluate a configuration key value",
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Eval = true
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagKeywords(flags, &options.Keywords)
	commoncmd.FlagImpersonate(flags, &options.Impersonate)
	return cmd
}

func newCmdObjectConfigGet(kind string) *cobra.Command {
	var options commands.CmdObjectConfigGet
	cmd := &cobra.Command{
		Use:   "get",
		Short: "get a configuration key value",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagEval(flags, &options.Eval)
	commoncmd.FlagImpersonate(flags, &options.Impersonate)
	commoncmd.FlagKeywords(flags, &options.Keywords)
	return cmd
}

func newCmdObjectConfigShow(kind string) *cobra.Command {
	var options commands.CmdObjectConfigShow
	cmd := &cobra.Command{
		Use:   "show",
		Short: "show the object configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	commoncmd.FlagObjectSelector(flags, &options.ObjectSelector)
	commoncmd.FlagSections(flags, &options.Sections)
	return cmd
}

func newCmdObjectConfigUpdate(kind string) *cobra.Command {
	var options commands.CmdObjectConfigUpdate
	cmd := commoncmd.NewCmdAnyConfigUpdate()
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return options.Run(kind)
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagUpdateDelete(flags, &options.Delete)
	commoncmd.FlagUpdateSet(flags, &options.Set)
	commoncmd.FlagUpdateUnset(flags, &options.Unset)
	return cmd
}

func newCmdObjectConfigValidate(kind string) *cobra.Command {
	var options commands.CmdObjectConfigValidate
	cmd := &cobra.Command{
		Use:     "validate",
		Short:   "verify the object configuration syntax",
		Aliases: []string{"val"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	return cmd
}

func newCmdObjectComplianceAttachModuleset(kind string) *cobra.Command {
	var options commands.CmdObjectComplianceAttachModuleset
	cmd := &cobra.Command{
		Use:     "moduleset",
		Short:   "attach modulesets to this object",
		Long:    "Modules of attached modulesets are checked on schedule.",
		Aliases: []string{"modulese", "modules", "module", "modul", "modu", "mod", "mo"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagModuleset(flags, &options.Moduleset)
	return cmd
}

func newCmdObjectComplianceAttachRuleset(kind string) *cobra.Command {
	var options commands.CmdObjectComplianceAttachRuleset
	cmd := &cobra.Command{
		Use:     "ruleset",
		Short:   "attach rulesets to this object",
		Long:    "Rules of attached rulesets are exposed to modules.",
		Aliases: []string{"rulese", "rules", "rule", "rul", "ru"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagRuleset(flags, &options.Ruleset)
	return cmd
}

func newCmdObjectComplianceAuto(kind string) *cobra.Command {
	var options commands.CmdObjectComplianceAuto
	cmd := &cobra.Command{
		Use:   "auto",
		Short: "run modules fixes or checks",
		Long:  "If the module is has the 'autofix' property set, do a fix, else do a check.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagModule(flags, &options.Module)
	commoncmd.FlagModuleset(flags, &options.Moduleset)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	commoncmd.FlagComplianceAttach(flags, &options.Attach)
	commoncmd.FlagComplianceForce(flags, &options.Force)
	return cmd
}

func newCmdObjectComplianceCheck(kind string) *cobra.Command {
	var options commands.CmdObjectComplianceCheck
	cmd := &cobra.Command{
		Use:     "check",
		Short:   "run modules checks",
		Aliases: []string{"chec", "che", "ch"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagModule(flags, &options.Module)
	commoncmd.FlagModuleset(flags, &options.Moduleset)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	commoncmd.FlagComplianceAttach(flags, &options.Attach)
	commoncmd.FlagComplianceForce(flags, &options.Force)
	return cmd
}

func newCmdObjectComplianceFix(kind string) *cobra.Command {
	var options commands.CmdObjectComplianceFix
	cmd := &cobra.Command{
		Use:   "fix",
		Short: "run modules fixes",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagModule(flags, &options.Module)
	commoncmd.FlagModuleset(flags, &options.Moduleset)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	commoncmd.FlagComplianceAttach(flags, &options.Attach)
	commoncmd.FlagComplianceForce(flags, &options.Force)
	return cmd
}

func newCmdObjectComplianceFixable(kind string) *cobra.Command {
	var options commands.CmdObjectComplianceFixable
	cmd := &cobra.Command{
		Use:     "fixable",
		Short:   "run modules fixable-tests",
		Aliases: []string{"fixabl", "fixab", "fixa"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagModule(flags, &options.Module)
	commoncmd.FlagModuleset(flags, &options.Moduleset)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	commoncmd.FlagComplianceAttach(flags, &options.Attach)
	commoncmd.FlagComplianceForce(flags, &options.Force)
	return cmd
}

func newCmdObjectComplianceDetachModuleset(kind string) *cobra.Command {
	var options commands.CmdObjectComplianceDetachModuleset
	cmd := &cobra.Command{
		Use:     "moduleset",
		Short:   "detach modulesets from this object",
		Long:    "Modules of attached modulesets are checked on schedule.",
		Aliases: []string{"modulese", "modules", "module", "modul", "modu", "mod", "mo"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagModuleset(flags, &options.Moduleset)
	return cmd
}

func newCmdObjectComplianceDetachRuleset(kind string) *cobra.Command {
	var options commands.CmdObjectComplianceDetachRuleset
	cmd := &cobra.Command{
		Use:     "ruleset",
		Short:   "detach rulesets from this object",
		Long:    "Rules of attached rulesets are made available to their module.",
		Aliases: []string{"rulese", "rules", "rule", "rul", "ru"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagRuleset(flags, &options.Ruleset)
	return cmd
}

func newCmdObjectComplianceEnv(kind string) *cobra.Command {
	var options commands.CmdObjectComplianceEnv
	cmd := &cobra.Command{
		Use:     "env",
		Short:   "show the env variables set for a module run",
		Aliases: []string{"en"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagModuleset(flags, &options.Moduleset)
	commoncmd.FlagModule(flags, &options.Module)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectComplianceListModules(kind string) *cobra.Command {
	var options commands.CmdObjectComplianceListModules
	cmd := &cobra.Command{
		Use:     "modules",
		Short:   "list modules available on this object",
		Aliases: []string{"module", "modul", "modu", "mod"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	return cmd
}

func newCmdObjectComplianceListModuleset(kind string) *cobra.Command {
	var options commands.CmdObjectComplianceListModuleset
	cmd := &cobra.Command{
		Use:     "moduleset",
		Short:   "list modulesets available to this object",
		Aliases: []string{"modulesets"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagModuleset(flags, &options.Moduleset)
	return cmd
}

func newCmdObjectComplianceListRuleset(kind string) *cobra.Command {
	var options commands.CmdObjectComplianceListRuleset
	cmd := &cobra.Command{
		Use:     "ruleset",
		Short:   "list rulesets available to this object",
		Aliases: []string{"rulese", "rules", "rule", "rul", "ru"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagRuleset(flags, &options.Ruleset)
	return cmd
}

func newCmdObjectComplianceShowModuleset(kind string) *cobra.Command {
	var options commands.CmdObjectComplianceShowModuleset
	cmd := &cobra.Command{
		Use:     "moduleset",
		Short:   "show modulesets and modules attached to this object",
		Aliases: []string{"modulese", "modules", "module", "modul", "modu", "mod", "mo"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagModuleset(flags, &options.Moduleset)
	return cmd
}

func newCmdObjectComplianceShowRuleset(kind string) *cobra.Command {
	var options commands.CmdObjectComplianceShowRuleset
	cmd := &cobra.Command{
		Use:     "ruleset",
		Short:   "show rules contextualized for to this object",
		Aliases: []string{"rulese", "rules", "rule", "rul", "ru"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectCreate(kind string) *cobra.Command {
	var options commands.CmdObjectCreate
	cmd := &cobra.Command{
		Use:   "create",
		Short: "create a new object",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsAsync(flags, &options.OptsAsync)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagCreateConfig(flags, &options.Config)
	commoncmd.FlagCreateEnv(flags, &options.Env)
	commoncmd.FlagCreateForce(flags, &options.Force)
	commoncmd.FlagCreateNamespace(flags, &options.Namespace)
	commoncmd.FlagCreateRestore(flags, &options.Restore)
	commoncmd.FlagKeywords(flags, &options.Keywords)
	commoncmd.FlagProvision(flags, &options.Provision)
	return cmd
}

func newCmdObjectDelete(kind string) *cobra.Command {
	var options commands.CmdObjectDelete
	cmd := &cobra.Command{
		GroupID: commoncmd.GroupIDOrchestratedActions,
		Use:     "delete",
		Aliases: []string{"del"},
		Short:   "delete object configuration",
		Long: "Delete object configuration.\n\n" +
			"Beware:" +
			" The delete is orchestrated so all instance configurations" +
			" are deleted. The delete command is not responsible for stopping or unprovisioning." +
			" The deletion happens whatever the object status.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsAsync(flags, &options.OptsAsync)
	commoncmd.HiddenFlagsLock(flags, &options.OptsLock)
	commoncmd.HiddenFlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectDeploy(kind string) *cobra.Command {
	var options commands.CmdObjectCreate
	cmd := &cobra.Command{
		GroupID: commoncmd.GroupIDOrchestratedActions,
		Use:     "deploy",
		Short:   "create a new object and orchestrate provision",
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Provision = true
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsAsync(flags, &options.OptsAsync)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagCreateConfig(flags, &options.Config)
	commoncmd.FlagCreateEnv(flags, &options.Env)
	commoncmd.FlagCreateForce(flags, &options.Force)
	commoncmd.FlagCreateNamespace(flags, &options.Namespace)
	commoncmd.FlagCreateRestore(flags, &options.Restore)
	commoncmd.FlagKeywords(flags, &options.Keywords)
	return cmd
}

func newCmdObjectDisable(kind string) *cobra.Command {
	var options commands.CmdObjectDisable
	cmd := &cobra.Command{
		Use:   "disable",
		Short: "disable a svc or resources",
		Long:  "Disabled svc or resources are skipped on actions.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagsResourceSelector(flags, &options.OptsResourceSelector)
	return cmd
}

func newCmdObjectEnable(kind string) *cobra.Command {
	var options commands.CmdObjectEnable
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "enable a svc or resources",
		Long:  "Disabled svc or resources are skipped on actions.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagsResourceSelector(flags, &options.OptsResourceSelector)
	return cmd
}

func newCmdObjectEnter(kind string) *cobra.Command {
	var options commands.CmdObjectEnter
	cmd := &cobra.Command{
		Use:   "enter",
		Short: "open a shell in a container resource",
		Long:  "Enter any container resource if --rid is not set.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagObject(flags, &options.ObjectSelector)
	commoncmd.FlagRID(flags, &options.RID)
	return cmd
}

func newCmdObjectFreeze(kind string) *cobra.Command {
	var options commands.CmdObjectFreeze
	cmd := &cobra.Command{
		GroupID: commoncmd.GroupIDOrchestratedActions,
		Use:     "freeze",
		Short:   "orchestrate block ha automatic start and monitor action",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsAsync(flags, &options.OptsAsync)
	return cmd
}

func newCmdObjectGiveback(kind string) *cobra.Command {
	var options commands.CmdObjectGiveback
	cmd := &cobra.Command{
		GroupID: commoncmd.GroupIDOrchestratedActions,
		Use:     "giveback",
		Short:   "orchestrate to reach optimal placement",
		Long:    "Stop the misplaced service instances and start on the preferred nodes.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsAsync(flags, &options.OptsAsync)
	return cmd
}

func newCmdObjectLogs(kind string) *cobra.Command {
	var options commands.CmdObjectLogs
	cmd := &cobra.Command{
		Use:     "logs",
		Aliases: []string{"logs", "log", "lo"},
		GroupID: commoncmd.GroupIDQuery,
		Short:   "show object logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsLogs(flags, &options.OptsLogs)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectList(kind string) *cobra.Command {
	var options commands.CmdObjectList
	cmd := &cobra.Command{
		Aliases: []string{"ls"},
		GroupID: commoncmd.GroupIDQuery,
		Use:     "list",
		Short:   "print the selected objects path",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	return cmd
}

func newCmdObjectPrintResourceInfo(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceResourceInfoList
	cmd := &cobra.Command{
		Use:    "resinfo",
		Short:  "list the key-values reported by resources",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectInstanceDelete(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceDelete
	cmd := &cobra.Command{
		Use:     "delete",
		Aliases: []string{"del"},
		Short:   "delete the instance configuration",
		Long: "Delete the instance configuration\n\n" +
			"Beware: this command only removes the selected instances configuration and states." +
			" The config may be recreated by the daemon from a remote instance copy." +
			" The delete command is not responsible for stopping or unprovisioning." +
			" The deletion happens whatever the object status.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectInstanceDeviceList(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceDeviceList
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "print the object's exposed, used, base and claimed block devices",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	commoncmd.FlagDevRoles(flags, &options.Roles)
	return cmd
}

func newCmdObjectInstanceFreeze(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceFreeze
	cmd := &cobra.Command{
		Use:   "freeze",
		Short: "block ha automatic start and monitor action",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsEncap(flags, &options.OptsEncap)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectInstanceList(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceList
	cmd := &cobra.Command{
		Aliases: []string{"ls"},
		Use:     "list",
		Short:   "object instances list",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectInstanceProvision(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceProvision
	cmd := &cobra.Command{
		Use:     "provision",
		Short:   "allocate the system resources of the instance resources",
		Long:    "Allocate the system resources required by the object instance resources.\n\nFor example, provision a fs.ext3 resource means format the device with the mkfs.ext3 command.\n\nOperate on a selection of instances asynchronously using --node=<selector>.",
		Aliases: []string{"prov"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsEncap(flags, &options.OptsEncap)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagsResourceSelector(flags, &options.OptsResourceSelector)
	commoncmd.FlagsTo(flags, &options.OptTo)
	commoncmd.FlagDisableRollback(flags, &options.DisableRollback)
	commoncmd.FlagForce(flags, &options.Force)
	commoncmd.FlagLeader(flags, &options.Leader)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	commoncmd.FlagStateOnly(flags, &options.StateOnly)
	return cmd
}

func newCmdObjectInstancePRStart(kind string) *cobra.Command {
	var options commands.CmdObjectInstancePRStart
	cmd := &cobra.Command{
		Use:   "prstart",
		Short: "preempt devices exclusive write access reservation",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsEncap(flags, &options.OptsEncap)
	commoncmd.FlagsResourceSelector(flags, &options.OptsResourceSelector)
	commoncmd.FlagsTo(flags, &options.OptTo)
	commoncmd.FlagDisableRollback(flags, &options.DisableRollback)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	commoncmd.FlagForce(flags, &options.Force)
	cmd.MarkFlagsMutuallyExclusive("no-lock", "node")
	cmd.MarkFlagsMutuallyExclusive("waitlock", "node")
	return cmd
}

func newCmdObjectInstancePRStop(kind string) *cobra.Command {
	var options commands.CmdObjectInstancePRStop
	cmd := &cobra.Command{
		Use:   "prstop",
		Short: "release devices exclusive write access reservation",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsEncap(flags, &options.OptsEncap)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagsResourceSelector(flags, &options.OptsResourceSelector)
	commoncmd.FlagsTo(flags, &options.OptTo)
	commoncmd.FlagForce(flags, &options.Force)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	cmd.MarkFlagsMutuallyExclusive("no-lock", "node")
	cmd.MarkFlagsMutuallyExclusive("waitlock", "node")
	return cmd
}

func newCmdObjectInstanceRestart(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceRestart
	cmd := &cobra.Command{
		Use:   "restart",
		Short: "restart the selected instances or resources",
		Long:  "Restart the local instance inline, or a selection of instances asynchronously using --node=<selector>.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsEncap(flags, &options.OptsEncap)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagsResourceSelector(flags, &options.OptsResourceSelector)
	commoncmd.FlagsTo(flags, &options.OptTo)
	commoncmd.FlagForce(flags, &options.Force)
	commoncmd.FlagDisableRollback(flags, &options.DisableRollback)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	cmd.MarkFlagsMutuallyExclusive("no-lock", "node")
	cmd.MarkFlagsMutuallyExclusive("waitlock", "node")
	return cmd
}

func newCmdObjectInstanceStart(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceStart
	cmd := &cobra.Command{
		Use:   "start",
		Short: "bring up instance resources",
		Long:  "Start a selection of instances asynchronously using --node=<selector>.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsEncap(flags, &options.OptsEncap)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagsResourceSelector(flags, &options.OptsResourceSelector)
	commoncmd.FlagsTo(flags, &options.OptTo)
	commoncmd.FlagForce(flags, &options.Force)
	commoncmd.FlagDisableRollback(flags, &options.DisableRollback)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	cmd.MarkFlagsMutuallyExclusive("no-lock", "node")
	cmd.MarkFlagsMutuallyExclusive("waitlock", "node")
	return cmd
}

func newCmdObjectInstanceStop(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceStop
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "bring down instance resources",
		Long:  "Stop a selection of instances asynchronously using --node=<selector>.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsEncap(flags, &options.OptsEncap)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagsResourceSelector(flags, &options.OptsResourceSelector)
	commoncmd.FlagsTo(flags, &options.OptTo)
	commoncmd.FlagForce(flags, &options.Force)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	cmd.MarkFlagsMutuallyExclusive("no-lock", "node")
	cmd.MarkFlagsMutuallyExclusive("waitlock", "node")
	return cmd
}

func newCmdObjectInstanceUnfreeze(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceUnfreeze
	cmd := &cobra.Command{
		Use:   "unfreeze",
		Short: "unblock ha automatic start and monitor action",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsEncap(flags, &options.OptsEncap)
	commoncmd.HiddenFlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectInstanceUnprovision(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceUnprovision
	cmd := &cobra.Command{
		Use:     "unprovision",
		Short:   "free the system resources of the instance resources (data-loss danger)",
		Long:    "Free the system resources required by the object instance resources.\n\nOperate on a selection of instances asynchronously using --node=<selector>.",
		Aliases: []string{"prov"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsEncap(flags, &options.OptsEncap)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagsResourceSelector(flags, &options.OptsResourceSelector)
	commoncmd.FlagsTo(flags, &options.OptTo)
	commoncmd.FlagForce(flags, &options.Force)
	commoncmd.FlagLeader(flags, &options.Leader)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	commoncmd.FlagStateOnly(flags, &options.StateOnly)
	return cmd
}

func newCmdObjectInstanceStatus(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceStatus
	cmd := &cobra.Command{
		Use:     "status",
		Aliases: []string{"statu", "stat", "sta", "st"},
		Short:   "print the object instances status",
		Long: `Resources Flags:

(1) R   Running,           . Not Running
(2) M   Monitored,         . Not Monitored
(3) D   Disabled,          . Enabled
(4) O   Optional,          . Not Optional
(5) E   Encap,             . Not Encap
(6) P   Not Provisioned,   . Provisioned
(7) S   Standby,           . Not Standby
(8) <n> Remaining Restart, + if more than 10,   . No Restart

`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagRefresh(flags, &options.Refresh)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectProvision(kind string) *cobra.Command {
	var options commands.CmdObjectProvision
	cmd := &cobra.Command{
		GroupID: commoncmd.GroupIDOrchestratedActions,
		Use:     "provision",
		Short:   "orchestrate provision",
		Long:    "Allocate the system resources required by the object instance resources.\n\nFor example, provision a fs.ext3 resource means format the device with the mkfs.ext3 command.\n\nOperate on a selection of instances asynchronously using --node=<selector>.",
		Aliases: []string{"prov"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsAsync(flags, &options.OptsAsync)
	return cmd
}

func newCmdObjectPRStart(kind string) *cobra.Command {
	var options commands.CmdObjectInstancePRStart
	cmd := &cobra.Command{
		Use:        "prstart",
		Hidden:     true,
		Deprecated: "use \"instance prstart\"",
		Short:      "preempt devices exclusive write access reservation",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsEncap(flags, &options.OptsEncap)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagsResourceSelector(flags, &options.OptsResourceSelector)
	commoncmd.FlagsTo(flags, &options.OptTo)
	commoncmd.FlagForce(flags, &options.Force)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectPRStop(kind string) *cobra.Command {
	var options commands.CmdObjectInstancePRStop
	cmd := &cobra.Command{
		Use:        "prstop",
		Hidden:     true,
		Deprecated: "use \"instance prstop\"",
		Short:      "release devices exclusive write access reservation",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsEncap(flags, &options.OptsEncap)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagsResourceSelector(flags, &options.OptsResourceSelector)
	commoncmd.FlagsTo(flags, &options.OptTo)
	commoncmd.FlagForce(flags, &options.Force)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectPurge(kind string) *cobra.Command {
	var options commands.CmdObjectPurge
	cmd := &cobra.Command{
		GroupID: commoncmd.GroupIDOrchestratedActions,
		Use:     "purge",
		Short:   "orchestrate unprovision and delete",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsAsync(flags, &options.OptsAsync)
	return cmd
}

func newCmdObjectPushResourceInfo(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceResourceInfoPush
	cmd := &cobra.Command{
		Hidden:  true,
		Use:     "resinfo",
		Short:   "push key-values reported by resources",
		Aliases: []string{"res"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectRestart(kind string) *cobra.Command {
	var options commands.CmdObjectRestart
	cmd := &cobra.Command{
		GroupID: commoncmd.GroupIDOrchestratedActions,
		Use:     "orchestrate restart",
		Short:   "restart the selected objects, instances or resources",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsAsync(flags, &options.OptsAsync)
	return cmd
}

func newCmdObjectInstanceSyncIngest(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceSyncIngest
	cmd := &cobra.Command{
		Use:   "Ingest",
		Short: "ingest files received from the active instance",
		Long:  "Resource drivers can send files from the active instance to the stand-by instances via the update action.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagsResourceSelector(flags, &options.OptsResourceSelector)
	return cmd
}

func newCmdObjectInstanceSyncFull(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceSyncFull
	cmd := &cobra.Command{
		Use:   "full",
		Short: "full copy of the local dataset on peers",
		Long:  "This update can use only full copy.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagsResourceSelector(flags, &options.OptsResourceSelector)
	commoncmd.FlagForce(flags, &options.Force)
	commoncmd.FlagTarget(flags, &options.Target)
	return cmd
}

func newCmdObjectInstanceSyncResync(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceSyncResync
	cmd := &cobra.Command{
		Use:   "resync",
		Short: "restore optimal synchronization",
		Long:  "Only a subset of drivers support this interface. For example, the disk.md driver re-adds removed disks.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagsResourceSelector(flags, &options.OptsResourceSelector)
	commoncmd.FlagForce(flags, &options.Force)
	return cmd
}

func newCmdObjectInstanceSyncUpdate(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceSyncUpdate
	cmd := &cobra.Command{
		Use:   "update",
		Short: "synchronize the copy of the local dataset on peers",
		Long:  "This update can use either full or incremental copy, depending on the resource drivers and host capabilities. This is the action executed by the scheduler.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagsResourceSelector(flags, &options.OptsResourceSelector)
	commoncmd.FlagForce(flags, &options.Force)
	commoncmd.FlagTarget(flags, &options.Target)
	return cmd
}

func newCmdObjectInstanceResourceInfoList(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceResourceInfoList
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "list the key-values reported by resources",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectInstanceResourceInfoPush(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceResourceInfoPush
	cmd := &cobra.Command{
		Use:   "push",
		Short: "push key-values reported by resources",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectResourceList(kind string) *cobra.Command {
	var options commands.CmdObjectResourceList
	cmd := &cobra.Command{
		Aliases: []string{"ls"},
		Use:     "list",
		Short:   "list the selected resource (config, monitor, status)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagRID(flags, &options.RID)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectInstanceRun(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceRun
	cmd := &cobra.Command{
		Use:   "run",
		Short: "execute instance tasks",
		Long:  "The svc and vol objects can define task resources. Tasks are usually run on a schedule, but this command can trigger a run now.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsEncap(flags, &options.OptsEncap)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagsResourceSelector(flags, &options.OptsResourceSelector)
	commoncmd.FlagConfirm(flags, &options.Confirm)
	commoncmd.FlagCron(flags, &options.Cron)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	commoncmd.FlagEnv(flags, &options.Env)
	return cmd
}

func newCmdObjectSetProvisioned(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceProvision
	cmd := &cobra.Command{
		Use:        "provisioned",
		Short:      "set the resources provisioned property",
		Long:       "This action does not provision the resources (fs are not formatted, disk not allocated, ...). This is just a resources provisioned flag create. Necessary to allow the unprovision action, which is bypassed if the provisioned flag is not set.",
		Aliases:    []string{"provision", "prov"},
		Deprecated: "use provision --state-only.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	options.StateOnly = true
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsEncap(flags, &options.OptsEncap)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagsResourceSelector(flags, &options.OptsResourceSelector)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectSetUnprovisioned(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceUnprovision
	cmd := &cobra.Command{
		Use:        "unprovisioned",
		Short:      "unset the resources provisioned property",
		Long:       "This action does not unprovision the resources (fs are not wiped, disk not removed, ...). This is just a resources provisioned flag remove. Necessary to allow the provision action, which is bypassed if the provisioned flag is set.",
		Deprecated: "use unprovision --state-only.",
		Aliases:    []string{"unprovision", "unprov"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsEncap(flags, &options.OptsEncap)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagsResourceSelector(flags, &options.OptsResourceSelector)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectInstanceShutdown(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceShutdown
	cmd := &cobra.Command{
		Use:   "shutdown",
		Short: "shutdown the object or instance",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsEncap(flags, &options.OptsEncap)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagsResourceSelector(flags, &options.OptsResourceSelector)
	commoncmd.FlagsTo(flags, &options.OptTo)
	commoncmd.FlagForce(flags, &options.Force)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectStart(kind string) *cobra.Command {
	var options commands.CmdObjectStart
	cmd := &cobra.Command{
		GroupID: commoncmd.GroupIDOrchestratedActions,
		Use:     "start",
		Short:   "orchestrate start",
		Long:    "Request the daemon to orchestrate object start.\n\nUse the `instance start` command to start a specific instance.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	commoncmd.FlagsAsync(flags, &options.OptsAsync)
	commoncmd.FlagColor(flags, &options.OptsGlobal.Color)
	commoncmd.FlagOutput(flags, &options.OptsGlobal.Output)
	commoncmd.FlagObjectSelector(flags, &options.OptsGlobal.ObjectSelector)
	return cmd
}

func newCmdObjectInstanceStartStandby(kind string) *cobra.Command {
	var options commands.CmdObjectInstanceStartStandby
	cmd := &cobra.Command{
		Use:   "startstandby",
		Short: "activate resources for standby",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsEncap(flags, &options.OptsEncap)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagsResourceSelector(flags, &options.OptsResourceSelector)
	commoncmd.FlagsTo(flags, &options.OptTo)
	commoncmd.FlagForce(flags, &options.Force)
	commoncmd.FlagDisableRollback(flags, &options.DisableRollback)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdObjectStop(kind string) *cobra.Command {
	var options commands.CmdObjectStop
	cmd := &cobra.Command{
		GroupID: commoncmd.GroupIDOrchestratedActions,
		Use:     "stop",
		Short:   "orchestrate stop",
		Long:    "Request the daemon to orchestrate object stop.\n\nUse the `instance stop` command to stop a specific instance.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}

	flags := cmd.Flags()
	commoncmd.FlagColor(flags, &options.OptsGlobal.Color)
	commoncmd.FlagOutput(flags, &options.OptsGlobal.Output)
	commoncmd.FlagObjectSelector(flags, &options.OptsGlobal.ObjectSelector)
	commoncmd.FlagsAsync(flags, &options.OptsAsync)
	return cmd
}

func newCmdObjectSwitch(kind string) *cobra.Command {
	var options commands.CmdObjectSwitch
	cmd := &cobra.Command{
		GroupID: commoncmd.GroupIDOrchestratedActions,
		Use:     "switch",
		Short:   "orchestrate a running instance move-out",
		Long:    "Stop the running object instance and start on the next preferred node.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsAsync(flags, &options.OptsAsync)
	commoncmd.FlagSwitchTo(flags, &options.To)
	return cmd
}

func newCmdObjectUnfreeze(kind string) *cobra.Command {
	var options commands.CmdObjectUnfreeze
	cmd := &cobra.Command{
		GroupID: commoncmd.GroupIDOrchestratedActions,
		Use:     "unfreeze",
		Short:   "orchestrate unblock ha automatic start and monitor action",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsAsync(flags, &options.OptsAsync)
	return cmd
}

func newCmdObjectTakeover(kind string) *cobra.Command {
	var options commands.CmdObjectTakeover
	cmd := &cobra.Command{
		GroupID: commoncmd.GroupIDOrchestratedActions,
		Use:     "takeover",
		Short:   "orchestrate a running instance move-in",
		Long:    "Stop a object instance and start one on the local node.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsAsync(flags, &options.OptsAsync)
	return cmd
}

func newCmdObjectThaw(kind string) *cobra.Command {
	cmd := newCmdObjectUnfreeze(kind)
	cmd.Use = "thaw"
	cmd.Hidden = true
	return cmd
}

func newCmdObjectUnprovision(kind string) *cobra.Command {
	var options commands.CmdObjectUnprovision
	cmd := &cobra.Command{
		GroupID: commoncmd.GroupIDOrchestratedActions,
		Use:     "unprovision",
		Short:   "orchestrate free system resources (data-loss danger)",
		Long:    "Free the system resources required by the object instances resources.",
		Aliases: []string{"unprov"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsAsync(flags, &options.OptsAsync)
	return cmd
}

func newCmdPoolList() *cobra.Command {
	var options commands.CmdPoolList
	cmd := &cobra.Command{
		Aliases: []string{"ls"},
		Use:     "list",
		Short:   "list the cluster pools",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagPoolName(flags, &options.Name)
	return cmd
}

func newCmdPoolVolumeList() *cobra.Command {
	var options commands.CmdPoolVolumeList
	cmd := &cobra.Command{
		Aliases: []string{"ls"},
		Use:     "list",
		Short:   "list the pool volumes",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagPoolName(flags, &options.Name)
	return cmd
}

func newCmdTUI(kind string) *cobra.Command {
	var options tui.Options
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "interactive terminal user interface",
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Selector = mergeSelector("", kind, "")
			return tui.Run(&options)
		},
	}
	return cmd
}

// Hidden commands. Kept for backward compatibility.
func newCmdNodeEdit() *cobra.Command {
	cmd := newCmdNodeConfigEdit()
	cmd.Hidden = true
	return cmd
}

func newCmdNodeEditConfig() *cobra.Command {
	cmd := newCmdNodeConfigEdit()
	cmd.Hidden = true
	cmd.Use = "config"
	cmd.Aliases = []string{"confi", "conf", "con", "co", "c", "cf", "cfg"}
	return cmd
}

func newCmdNodeEval() *cobra.Command {
	cmd := newCmdNodeConfigEval()
	cmd.Hidden = true
	return cmd
}

func newCmdNodeGet() *cobra.Command {
	cmd := newCmdNodeConfigGet()
	cmd.Hidden = true
	return cmd
}

func newCmdNodePrintConfig() *cobra.Command {
	cmd := newCmdNodeConfigShow()
	cmd.Hidden = true
	cmd.Use = "config"
	cmd.Aliases = []string{"conf", "co", "cf", "cfg"}
	return cmd
}

func newCmdNodePrintSchedule() *cobra.Command {
	cmd := newCmdNodeScheduleList()
	cmd.Hidden = true
	cmd.Use = "schedule"
	cmd.Aliases = []string{"schedul", "schedu", "sched", "sche", "sch", "sc"}
	return cmd
}

func newCmdNodeUpdate() *cobra.Command {
	cmd := newCmdNodeConfigUpdate()
	cmd.Hidden = true
	return cmd
}

func newCmdNodeSet() *cobra.Command {
	var options commands.CmdNodeSet
	cmd := &cobra.Command{
		Use:    "set",
		Short:  "set a configuration key value",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagKeywordOps(flags, &options.KeywordOps)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	return cmd
}

func newCmdNodeUnset() *cobra.Command {
	var options commands.CmdNodeUnset
	cmd := &cobra.Command{
		Use:    "unset",
		Short:  "unset configuration keywords or sections",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run()
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagKeywords(flags, &options.Keywords)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	commoncmd.FlagSections(flags, &options.Sections)
	return cmd
}

func newCmdNodeValidate() *cobra.Command {
	cmd := newCmdNodeConfigValidate()
	cmd.Hidden = true
	return cmd
}

func newCmdNodeValidateConfig() *cobra.Command {
	cmd := newCmdNodeConfigValidate()
	cmd.Hidden = true
	cmd.Use = "config"
	cmd.Aliases = []string{"confi", "conf", "con", "co", "c"}
	return cmd
}

func newCmdObjectEval(kind string) *cobra.Command {
	cmd := newCmdObjectConfigEval(kind)
	cmd.Hidden = true
	return cmd
}

func newCmdObjectGet(kind string) *cobra.Command {
	cmd := newCmdObjectConfigGet(kind)
	cmd.Hidden = true
	return cmd
}

func newCmdObjectPrintConfig(kind string) *cobra.Command {
	cmd := newCmdObjectConfigShow(kind)
	cmd.Hidden = true
	cmd.Use = "config"
	cmd.Aliases = []string{"conf", "co", "cf", "cfg"}
	return cmd
}

func newCmdObjectEdit(kind string) *cobra.Command {
	var optionsGlobal commands.OptsGlobal
	var optionsConfig commands.CmdObjectConfigEdit
	var optionsKey commands.CmdObjectKeyEdit
	cmd := &cobra.Command{
		Use:     "edit",
		Short:   "edit object configuration or data key",
		Aliases: []string{"ed"},
		Hidden:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if optionsKey.Name != "" {
				optionsKey.OptsGlobal = optionsGlobal
				return optionsKey.Run(kind)
			} else {
				optionsConfig.OptsGlobal = optionsGlobal
				return optionsConfig.Run(kind)
			}
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &optionsGlobal)
	commoncmd.FlagDiscard(flags, &optionsConfig.Discard)
	commoncmd.FlagRecover(flags, &optionsConfig.Recover)
	commoncmd.FlagKey(flags, &optionsKey.Name)
	cmd.MarkFlagsMutuallyExclusive("discard", "recover")
	cmd.MarkFlagsMutuallyExclusive("discard", "key")
	cmd.MarkFlagsMutuallyExclusive("recover", "key")
	return cmd
}

func newCmdObjectEditConfig(kind string) *cobra.Command {
	cmd := newCmdObjectConfigEdit(kind)
	cmd.Hidden = true
	cmd.Aliases = []string{"ed"}
	return cmd
}

func newCmdObjectSet(kind string) *cobra.Command {
	var options commands.CmdObjectSet
	cmd := &cobra.Command{
		Use:    "set",
		Short:  "set a configuration key value",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagKeywordOps(flags, &options.KeywordOps)
	return cmd
}

func newCmdObjectUnset(kind string) *cobra.Command {
	var options commands.CmdObjectUnset
	cmd := &cobra.Command{
		Use:    "unset",
		Short:  "unset configuration keywords or sections",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagKeywords(flags, &options.Keywords)
	commoncmd.FlagSections(flags, &options.Sections)
	return cmd
}

func newCmdObjectUpdate(kind string) *cobra.Command {
	cmd := newCmdObjectConfigUpdate(kind)
	cmd.Hidden = true
	return cmd
}

func newCmdObjectValidate(kind string) *cobra.Command {
	cmd := newCmdObjectConfigValidate(kind)
	cmd.Hidden = true
	return cmd
}

func newCmdObjectValidateConfig(kind string) *cobra.Command {
	cmd := newCmdObjectConfigValidate(kind)
	cmd.Hidden = true
	cmd.Use = "config"
	cmd.Aliases = []string{"confi", "conf", "con", "co", "c"}
	return cmd
}

func newCmdObjectPrintStatus(kind string) *cobra.Command {
	cmd := newCmdObjectInstanceStatus(kind)
	cmd.Hidden = true
	return cmd
}

func newCmdObjectPrintSchedule(kind string) *cobra.Command {
	cmd := newCmdObjectScheduleList(kind)
	cmd.Hidden = true
	cmd.Use = "schedule"
	cmd.Aliases = []string{"schedul", "schedu", "sched", "sche", "sch", "sc"}
	return cmd
}

func newCmdDataStoreAdd(kind string) *cobra.Command {
	var options commands.CmdObjectKeyAdd
	var from, value string
	cmd := &cobra.Command{
		Hidden: true,
		Use:    "add",
		Short:  "add new keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flag("from").Changed {
				options.From = &from
			}
			if cmd.Flag("value").Changed {
				options.Value = &value
			}
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagsLock(flags, &options.OptsLock)
	commoncmd.FlagFrom(flags, &from)
	commoncmd.FlagKey(flags, &options.Name)
	commoncmd.FlagKeyValue(flags, &value)
	cmd.MarkFlagsMutuallyExclusive("from", "value")
	return cmd
}

func newCmdDataStoreChange(kind string) *cobra.Command {
	var options commands.CmdObjectKeyChange
	var from, value string
	cmd := &cobra.Command{
		Hidden: true,
		Use:    "change",
		Short:  "change existing keys value",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flag("from").Changed {
				options.From = &from
			}
			if cmd.Flag("value").Changed {
				options.Value = &value
			}
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagFrom(flags, &from)
	commoncmd.FlagKey(flags, &options.Name)
	commoncmd.FlagKeyValue(flags, &value)
	cmd.MarkFlagsMutuallyExclusive("from", "value")
	return cmd
}

func newCmdDataStoreDecode(kind string) *cobra.Command {
	var options commands.CmdObjectKeyDecode
	cmd := &cobra.Command{
		Hidden: true,
		Use:    "decode",
		Short:  "decode a key value",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagKey(flags, &options.Name)
	return cmd
}

func newCmdDataStoreInstall(kind string) *cobra.Command {
	var options commands.CmdObjectKeyInstall
	cmd := &cobra.Command{
		Hidden: true,
		Use:    "install",
		Short:  "install keys as files in volumes",
		Long:   "Keys of sec and cfg can be projected to volumes via the configs and secrets keywords of volume resources. When a key value change all projections are automatically refreshed. This command triggers manually the same operations.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagNodeSelector(flags, &options.NodeSelector)
	commoncmd.FlagKey(flags, &options.Name)
	return cmd
}

func newCmdDataStoreKeys(kind string) *cobra.Command {
	var options commands.CmdObjectKeyList
	cmd := &cobra.Command{
		Hidden: true,
		Use:    "keys",
		Short:  "list the keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagMatch(flags, &options.Match)
	return cmd
}

func newCmdDataStoreRemove(kind string) *cobra.Command {
	var options commands.CmdObjectKeyRemove
	cmd := &cobra.Command{
		Hidden: true,
		Use:    "remove",
		Short:  "remove a key",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagKey(flags, &options.Name)
	return cmd
}

func newCmdDataStoreRename(kind string) *cobra.Command {
	var options commands.CmdObjectKeyRename
	cmd := &cobra.Command{
		Hidden: true,
		Use:    "rename",
		Short:  "rename a key",
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	addFlagsGlobal(flags, &options.OptsGlobal)
	commoncmd.FlagKey(flags, &options.Name)
	commoncmd.FlagKeyTo(flags, &options.To)
	return cmd
}

func newCmdObjectGenCert(kind string) *cobra.Command {
	cmd := newCmdObjectCertificateCreate(kind)
	cmd.Use = "cert"
	cmd.Hidden = true
	cmd.Aliases = []string{"crt"}
	return cmd
}

func newCmdObjectPKCS(kind string) *cobra.Command {
	cmd := newCmdObjectCertificatePKCS(kind)
	cmd.Hidden = true
	return cmd
}

func newCmdObjectGen(kind string) *cobra.Command {
	return &cobra.Command{
		Hidden: true,
		Use:    "gen",
	}
}
