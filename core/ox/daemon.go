package ox

import (
	"github.com/spf13/cobra"
)

var (
	cmdDaemon = &cobra.Command{
		Use:   "daemon",
		Short: "manage the opensvc daemon",
	}

	cmdDaemonDNS = &cobra.Command{
		Use:   "dns",
		Short: "dns subsystem commands",
	}

	cmdDaemonHeartbeat = &cobra.Command{
		Use:   "hb",
		Short: "manage opensvc daemon heartbeat",
	}

	cmdDaemonListener = &cobra.Command{
		Use:   "listener",
		Short: "manage opensvc daemon listener",
	}

	cmdDaemonRelay = &cobra.Command{
		Use:   "relay",
		Short: "relay subsystem commands",
	}
)

func init() {
	root.AddCommand(
		cmdDaemon,
	)

	cmdDaemon.AddCommand(
		newCmdDaemonAuth(),
		cmdDaemonDNS,
		cmdDaemonHeartbeat,
		cmdDaemonListener,
		cmdDaemonRelay,
		newCmdDaemonRestart(),
		newCmdDaemonShutdown(),
		newCmdDaemonStatus(),
		newCmdDaemonStop(),
	)

	cmdDaemonDNS.AddCommand(
		newCmdDaemonDNSDump(),
	)

	cmdDaemonHeartbeat.AddCommand(
		newCmdDaemonHeartbeatRestart(),
		newCmdDaemonHeartbeatStart(),
		newCmdDaemonHeartbeatStatus(),
		newCmdDaemonHeartbeatStop(),
	)

	cmdDaemonListener.AddCommand(
		newCmdDaemonListenerLog(),
		newCmdDaemonListenerRestart(),
		newCmdDaemonListenerStart(),
		newCmdDaemonListenerStop(),
	)

	cmdDaemonRelay.AddCommand(
		newCmdDaemonRelayStatus(),
	)
}
