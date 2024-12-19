package object

import (
	"fmt"

	"github.com/opensvc/om3/core/keywords"
	"github.com/opensvc/om3/core/naming"
	"github.com/opensvc/om3/core/rawconfig"
	"github.com/opensvc/om3/daemon/daemonenv"
	"github.com/opensvc/om3/util/converters"
	"github.com/opensvc/om3/util/key"
)

const (
	DefaultNodeMaxParallel = 10
)

var nodePrivateKeywords = []keywords.Keyword{
	{
		Option:  "oci",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.oci"),
	},
	{
		Option:  "uuid",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.uuid"),
	},
	{
		DefaultText: keywords.NewText(fs, "text/kw/node/node.prkey.default"),
		Option:      "prkey",
		Section:     "node",
		Text:        keywords.NewText(fs, "text/kw/node/node.prkey"),
	},
	{
		Example: "1.2.3.4",
		Option:  "connect_to",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.connect_to"),
	},
	{
		Converter: converters.Size,
		Example:   "256mb",
		Option:    "mem_bytes",
		Section:   "node",
		Text:      keywords.NewText(fs, "text/kw/node/node.mem_bytes"),
	},
	{
		Converter: converters.Int,
		Example:   "4",
		Option:    "mem_banks",
		Section:   "node",
		Text:      keywords.NewText(fs, "text/kw/node/node.mem_banks"),
	},
	{
		Converter: converters.Int,
		Example:   "4",
		Option:    "mem_slots",
		Section:   "node",
		Text:      keywords.NewText(fs, "text/kw/node/node.mem_slots"),
	},
	{
		Example: "Digital",
		Option:  "os_vendor",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.os_vendor"),
	},
	{
		Example: "5",
		Option:  "os_release",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.os_release"),
	},
	{
		Example: "5.1234",
		Option:  "os_kernel",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.os_kernel"),
	},
	{
		Example: "5.1234",
		Option:  "os_arch",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.os_arch"),
	},
	{
		Example: "3.2 Ghz",
		Option:  "cpu_freq",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.cpu_freq"),
	},
	{
		Converter: converters.Int,
		Example:   "4",
		Option:    "cpu_threads",
		Section:   "node",
		Text:      keywords.NewText(fs, "text/kw/node/node.cpu_threads"),
	},
	{
		Converter: converters.Int,
		Example:   "2",
		Option:    "cpu_cores",
		Section:   "node",
		Text:      keywords.NewText(fs, "text/kw/node/node.cpu_cores"),
	},
	{
		Converter: converters.Int,
		Example:   "1",
		Option:    "cpu_dies",
		Section:   "node",
		Text:      keywords.NewText(fs, "text/kw/node/node.cpu_dies"),
	},
	{
		Example: "Alpha EV5",
		Option:  "cpu_model",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.cpu_model"),
	},
	{
		Example: "abcdef0123456",
		Option:  "serial",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.serial"),
	},
	{
		Example: "1.025",
		Option:  "bios_version",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.bios_version"),
	},
	{
		Example: "1.026",
		Option:  "sp_version",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.sp_version"),
	},
	{
		Example: "1",
		Option:  "enclosure",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.enclosure"),
	},
	{
		Example: "+0200",
		Option:  "tz",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.tz"),
	},
	{
		Example: "Digital",
		Option:  "manufacturer",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.manufacturer"),
	},
	{
		Example: "ds20e",
		Option:  "model",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.model"),
	},
	{
		Option:  "schedule",
		Section: "array",
		Text:    keywords.NewText(fs, "text/kw/node/array.schedule"),
	},
	{
		Example: "array1",
		Option:  "name",
		Section: "array",
		Text:    keywords.NewText(fs, "text/kw/node/array.xtremio.name"),
		Types:   []string{"xtremio"},
	},
	{
		Option:  "schedule",
		Section: "backup",
		Text:    keywords.NewText(fs, "text/kw/node/backup.schedule"),
	},
	{
		Option:  "schedule",
		Section: "switch",
		Text:    keywords.NewText(fs, "text/kw/node/switch.schedule"),
	},
}

var nodeCommonKeywords = []keywords.Keyword{
	{
		Converter: converters.Bool,
		Default:   "true",
		Option:    "secure_fetch",
		Section:   "node",
		Text:      keywords.NewText(fs, "text/kw/node/node.secure_fetch"),
	},
	{
		Converter: converters.Size,
		Default:   "2%",
		Option:    "min_avail_mem",
		Section:   "node",
		Text:      keywords.NewText(fs, "text/kw/node/node.min_avail_mem"),
	},
	{
		Converter: converters.Size,
		Default:   "10%",
		Option:    "min_avail_swap",
		Section:   "node",
		Text:      keywords.NewText(fs, "text/kw/node/node.min_avail_swap"),
	},
	{
		Default: "TST",
		Option:  "env",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.env"),
	},
	{
		Converter: converters.Int,
		Default:   fmt.Sprintf("%d", DefaultNodeMaxParallel),
		Option:    "max_parallel",
		Section:   "node",
		Text:      keywords.NewText(fs, "text/kw/node/node.max_parallel"),
	},
	{
		Converter: converters.List,
		Default:   "10.0.0.0/8 172.16.0.0/24 192.168.0.0/16",
		Option:    "allowed_networks",
		Section:   "node",
		Text:      keywords.NewText(fs, "text/kw/node/node.allowed_networks"),
	},
	{
		Example: "fr",
		Option:  "loc_country",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.loc_country"),
	},
	{
		Example: "Paris",
		Option:  "loc_city",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.loc_city"),
	},
	{
		Example: "75017",
		Option:  "loc_zip",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.loc_zip"),
	},
	{
		Example: "7 rue blanche",
		Option:  "loc_addr",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.loc_addr"),
	},
	{
		Example: "Crystal",
		Option:  "loc_building",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.loc_building"),
	},
	{
		Example: "21",
		Option:  "loc_floor",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.loc_floor"),
	},
	{
		Example: "102",
		Option:  "loc_room",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.loc_room"),
	},
	{
		Example: "R42",
		Option:  "loc_rack",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.loc_rack"),
	},
	{
		Example: "dmz1",
		Option:  "sec_zone",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.sec_zone"),
	},
	{
		Example: "TINT",
		Option:  "team_integ",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.team_integ"),
	},
	{
		Example: "TSUP",
		Option:  "team_support",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.team_support"),
	},
	{
		Example: "Production",
		Option:  "asset_env",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.asset_env"),
	},
	{
		Example: "https://collector.opensvc.com",
		Option:  "dbopensvc",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.dbopensvc"),
	},
	{
		Converter: converters.Bool,
		Option:    "dbinsecure",
		Section:   "node",
		Text:      keywords.NewText(fs, "text/kw/node/node.dbinsecure"),
	},
	{
		DefaultText: keywords.NewText(fs, "text/kw/node/node.dbcompliance.default"),
		Example:     "https://collector.opensvc.com",
		Option:      "dbcompliance",
		Section:     "node",
		Text:        keywords.NewText(fs, "text/kw/node/node.dbcompliance"),
	},
	{
		Converter: converters.Bool,
		Default:   "true",
		Option:    "dblog",
		Section:   "node",
		Text:      keywords.NewText(fs, "text/kw/node/node.dblog"),
	},
	{
		Example: "1.9",
		Option:  "branch",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.branch"),
	},
	{
		Example: "http://opensvc.repo.corp",
		Option:  "repo",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.repo"),
	},
	{
		Example: "http://repo.opensvc.com",
		Option:  "repopkg",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.repopkg"),
	},
	{
		Example: "http://compliance.repo.corp",
		Option:  "repocomp",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.repocomp"),
	},
	{
		Default: "root",
		Example: "root opensvc@node1",
		Option:  "ruser",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.ruser"),
	},
	{
		Default:   "60",
		Converter: converters.Duration,
		Option:    "maintenance_grace_period",
		Section:   "node",
		Text:      keywords.NewText(fs, "text/kw/node/node.maintenance_grace_period"),
	},
	{
		Converter: converters.Duration,
		Default:   "90s",
		Option:    "rejoin_grace_period",
		Section:   "node",
		Text:      keywords.NewText(fs, "text/kw/node/node.rejoin_grace_period"),
	},
	{
		Converter: converters.Duration,
		Default:   "5s",
		Option:    "ready_period",
		Section:   "node",
		Text:      keywords.NewText(fs, "text/kw/node/node.ready_period"),
	},
	{
		Option:  "schedule",
		Section: "dequeue_actions",
		Text:    keywords.NewText(fs, "text/kw/node/dequeue_actions.schedule"),
	},
	{
		Default: "~00:00-06:00",
		Option:  "schedule",
		Section: "sysreport",
		Text:    keywords.NewText(fs, "text/kw/node/sysreport.schedule"),
	},
	{
		Default: "02:00-06:00",
		Option:  "schedule",
		Section: "compliance",
		Text:    keywords.NewText(fs, "text/kw/node/compliance.schedule"),
	},
	{
		Converter: converters.Bool,
		Default:   "false",
		Option:    "auto_update",
		Section:   "compliance",
		Text:      keywords.NewText(fs, "text/kw/node/compliance.auto_update"),
	},
	{
		Default: "~00:00-06:00",
		Option:  "schedule",
		Section: "checks",
		Text:    keywords.NewText(fs, "text/kw/node/checks.schedule"),
	},
	{
		Default: "~00:00-06:00",
		Option:  "schedule",
		Section: "packages",
		Text:    keywords.NewText(fs, "text/kw/node/packages.schedule"),
	},
	{
		Default: "~00:00-06:00",
		Option:  "schedule",
		Section: "patches",
		Text:    keywords.NewText(fs, "text/kw/node/patches.schedule"),
	},
	{
		Default: "~00:00-06:00",
		Option:  "schedule",
		Section: "asset",
		Text:    keywords.NewText(fs, "text/kw/node/asset.schedule"),
	},
	{
		Default: "~00:00-06:00",
		Option:  "schedule",
		Section: "disks",
		Text:    keywords.NewText(fs, "text/kw/node/disks.schedule"),
	},
	{
		Default: rawconfig.Paths.CACRL,
		Example: "https://crl.opensvc.com",
		Option:  "crl",
		Section: "listener",
		Text:    keywords.NewText(fs, "text/kw/node/listener.crl"),
	},
	{
		Default: "953",
		Option:  "dns_sock_uid",
		Section: "listener",
		Text:    keywords.NewText(fs, "text/kw/node/listener.dns_sock_uid"),
	},
	{
		Default: "953",
		Option:  "dns_sock_gid",
		Section: "listener",
		Text:    keywords.NewText(fs, "text/kw/node/listener.dns_sock_gid"),
	},
	{
		Aliases:  []string{"tls_addr"},
		Default:  "",
		Example:  "1.2.3.4",
		Option:   "addr",
		Scopable: true,
		Section:  "listener",
		Text:     keywords.NewText(fs, "text/kw/node/listener.addr"),
	},
	{
		Aliases:   []string{"tls_port"},
		Converter: converters.Int,
		Default:   fmt.Sprintf("%d", daemonenv.HTTPPort),
		Option:    "port",
		Scopable:  true,
		Section:   "listener",
		Text:      keywords.NewText(fs, "text/kw/node/listener.port"),
	},
	{
		Example: "https://keycloak.opensvc.com/auth/realms/clusters/.well-known/openid-configuration",
		Option:  "openid_well_known",
		Section: "listener",
		Text:    keywords.NewText(fs, "text/kw/node/listener.openid_well_known"),
	},
	{
		Default: "daemon",
		Option:  "facility",
		Section: "syslog",
		Text:    keywords.NewText(fs, "text/kw/node/syslog.facility"),
	},
	{
		Candidates: []string{"critical", "error", "warning", "info", "debug"},
		Default:    "info",
		Option:     "level",
		Section:    "syslog",
		Text:       keywords.NewText(fs, "text/kw/node/syslog.level"),
	},
	{
		DefaultText: keywords.NewText(fs, "text/kw/node/syslog.host.default"),
		Option:      "host",
		Section:     "syslog",
		Text:        keywords.NewText(fs, "text/kw/node/syslog.host"),
	},
	{
		Default: "514",
		Option:  "port",
		Section: "syslog",
		Text:    keywords.NewText(fs, "text/kw/node/syslog.port"),
	},
	{
		Example:  "192.168.99.12/24@eth0",
		Option:   "vip",
		Scopable: true,
		Section:  "cluster",
		Text:     keywords.NewText(fs, "text/kw/node/cluster.vip"),
	},
	{
		Converter: converters.List,
		Option:    "dns",
		Scopable:  true,
		Section:   "cluster",
		Text:      keywords.NewText(fs, "text/kw/node/cluster.dns"),
	},
	{
		DefaultText: keywords.NewText(fs, "text/kw/node/cluster.ca.default"),
		Converter:   converters.List,
		Option:      "ca",
		Section:     "cluster",
		Text:        keywords.NewText(fs, "text/kw/node/cluster.ca"),
	},
	{
		DefaultText: keywords.NewText(fs, "text/kw/node/cluster.cert.default"),
		Option:      "cert",
		Section:     "cluster",
		Text:        keywords.NewText(fs, "text/kw/node/cluster.cert"),
	},
	{
		DefaultText: keywords.NewText(fs, "text/kw/node/cluster.id.default"),
		Option:      "id",
		Scopable:    true,
		Section:     "cluster",
		Text:        keywords.NewText(fs, "text/kw/node/cluster.id"),
	},
	{
		DefaultText: keywords.NewText(fs, "text/kw/node/cluster.name.default"),
		Option:      "name",
		Section:     "cluster",
		Text:        keywords.NewText(fs, "text/kw/node/cluster.name"),
	},
	{
		DefaultText: keywords.NewText(fs, "text/kw/node/cluster.secret.default"),
		Option:      "secret",
		Scopable:    true,
		Section:     "cluster",
		Text:        keywords.NewText(fs, "text/kw/node/cluster.secret"),
	},
	{
		Converter: converters.List,
		Option:    "nodes",
		Section:   "cluster",
		Text:      keywords.NewText(fs, "text/kw/node/cluster.nodes"),
	},
	{
		Converter: converters.List,
		Option:    "drpnodes",
		Section:   "cluster",
		Text:      keywords.NewText(fs, "text/kw/node/cluster.drpnodes"),
	},
	{
		Converter: converters.List,
		Option:    "envs",
		Default:   "CERT DEV DRP FOR INT PRA PRD PRJ PPRD QUAL REC STG TMP TST UAT",
		Section:   "cluster",
		Text:      keywords.NewText(fs, "text/kw/node/cluster.envs"),
	},
	{
		Converter: converters.Bool,
		Default:   "false",
		Option:    "quorum",
		Section:   "cluster",
		Text:      keywords.NewText(fs, "text/kw/node/cluster.quorum"),
	},
	{
		Default: "opensvc",
		Option:  "sshkey",
		Section: "node",
		Text:    keywords.NewText(fs, "text/kw/node/node.sshkey"),
	},
	{
		Candidates: []string{"crash", "reboot", "disabled"},
		Default:    "crash",
		Option:     "split_action",
		Scopable:   true,
		Section:    "node",
		Text:       keywords.NewText(fs, "text/kw/node/node.split_action"),
	},
	{
		Aliases:  []string{"name"},
		Example:  "http://www.opensvc.com",
		Option:   "uri",
		Required: true,
		Section:  "arbitrator",
		Text:     keywords.NewText(fs, "text/kw/node/arbitrator.uri"),
	},
	{
		Converter: converters.Bool,
		Default:   "false",
		Option:    "insecure",
		Section:   "arbitrator",
		Text:      keywords.NewText(fs, "text/kw/node/arbitrator.insecure"),
	},
	{
		Converter: converters.Shlex,
		Example:   "/bin/true",
		Option:    "cmd",
		Required:  true,
		Scopable:  true,
		Section:   "stonith",
		Text:      keywords.NewText(fs, "text/kw/node/stonith.cmd"),
	},
	{
		Candidates: []string{"unicast", "multicast", "disk", "relay"},
		Option:     "type",
		Required:   true,
		Section:    "hb",
		Text:       keywords.NewText(fs, "text/kw/node/hb.type"),
	},
	{
		DefaultText: keywords.NewText(fs, "text/kw/node/hb.unicast.addr.default"),
		Example:     "1.2.3.4",
		Option:      "addr",
		Scopable:    true,
		Section:     "hb",
		Text:        keywords.NewText(fs, "text/kw/node/hb.unicast.addr"),
		Types:       []string{"unicast"},
	},
	{
		DefaultText: keywords.NewText(fs, "text/kw/node/hb.unicast.intf.default"),
		Example:     "eth0",
		Option:      "intf",
		Scopable:    true,
		Section:     "hb",
		Text:        keywords.NewText(fs, "text/kw/node/hb.unicast.intf"),
		Types:       []string{"unicast"},
	},
	{
		Converter: converters.Int,
		Default:   "10000",
		Option:    "port",
		Scopable:  true,
		Section:   "hb",
		Text:      keywords.NewText(fs, "text/kw/node/hb.unicast.port"),
		Types:     []string{"unicast"},
	},
	{
		Converter: converters.Duration,
		Default:   "15s",
		Option:    "timeout",
		Scopable:  true,
		Section:   "hb",
		Text:      keywords.NewText(fs, "text/kw/node/hb.timeout"),
	},
	{
		Converter: converters.Duration,
		Default:   "5s",
		Option:    "interval",
		Scopable:  true,
		Section:   "hb",
		Text:      keywords.NewText(fs, "text/kw/node/hb.interval"),
	},
	{
		Default:  "224.3.29.71",
		Option:   "addr",
		Scopable: true,
		Section:  "hb",
		Text:     keywords.NewText(fs, "text/kw/node/hb.multicast.addr"),
		Types:    []string{"multicast"},
	},
	{
		DefaultText: keywords.NewText(fs, "text/kw/node/hb.multicast.intf.default"),
		Example:     "eth0",
		Option:      "intf",
		Scopable:    true,
		Section:     "hb",
		Text:        keywords.NewText(fs, "text/kw/node/hb.multicast.intf"),
		Types:       []string{"multicast"},
	},
	{
		Converter: converters.Int,
		Default:   "10000",
		Option:    "port",
		Scopable:  true,
		Section:   "hb",
		Text:      keywords.NewText(fs, "text/kw/node/hb.multicast.port"),
		Types:     []string{"multicast"},
	},
	{
		Converter:   converters.List,
		DefaultText: keywords.NewText(fs, "text/kw/node/hb.unicast.nodes.default"),
		Option:      "nodes",
		Scopable:    true,
		Section:     "hb",
		Text:        keywords.NewText(fs, "text/kw/node/hb.unicast.nodes"),
		Types:       []string{"unicast"},
	},
	{
		Example:  "/dev/mapper/36589cfc000000e03957c51dabab8373a",
		Option:   "dev",
		Required: true,
		Scopable: true,
		Section:  "hb",
		Text:     keywords.NewText(fs, "text/kw/node/hb.disk.dev"),
		Types:    []string{"disk"},
	},
	{
		Converter: converters.Bool,
		Default:   "false",
		Option:    "insecure",
		Section:   "hb",
		Text:      keywords.NewText(fs, "text/kw/node/hb.relay.insecure"),
		Types:     []string{"relay"},
	},
	{
		Example:  "relaynode1",
		Option:   "relay",
		Required: true,
		Section:  "hb",
		Text:     keywords.NewText(fs, "text/kw/node/hb.relay.relay"),
		Types:    []string{"relay"},
	},
	{
		Default: "relay",
		Option:  "username",
		Section: "hb",
		Text:    keywords.NewText(fs, "text/kw/node/hb.relay.username"),
		Types:   []string{"relay"},
	},
	{
		Default: "system/sec/relay",
		Option:  "password",
		Section: "hb",
		Text:    keywords.NewText(fs, "text/kw/node/hb.relay.password"),
		Types:   []string{"relay"},
	},
	{
		Default: "/opt/cni/bin",
		Example: "/var/lib/opensvc/cni/bin",
		Option:  "plugins",
		Section: "cni",
		Text:    keywords.NewText(fs, "text/kw/node/cni.plugins"),
	},
	{
		Default: "/opt/cni/net.d",
		Example: "/var/lib/opensvc/cni/net.d",
		Option:  "config",
		Section: "cni",
		Text:    keywords.NewText(fs, "text/kw/node/cni.config"),
	},
	{
		Candidates: []string{"directory", "loop", "vg", "zpool", "freenas", "share", "shm", "symmetrix", "virtual", "dorado", "hoc", "drbd", "pure"},
		Default:    "directory",
		Option:     "type",
		Section:    "pool",
		Text:       keywords.NewText(fs, "text/kw/node/pool.type"),
	},
	{
		Option:  "status_schedule",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.status_schedule"),
	},
	{
		Option:   "mnt_opt",
		Scopable: true,
		Section:  "pool",
		Text:     keywords.NewText(fs, "text/kw/node/pool.mnt_opt"),
	},
	{
		Option:   "array",
		Required: true,
		Scopable: true,
		Section:  "pool",
		Text:     keywords.NewText(fs, "text/kw/node/pool.array"),
		Types:    []string{"freenas", "symmetrix", "dorado", "hoc", "pure"},
	},
	{
		Option:  "label_prefix",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.hoc.label_prefix"),
		Types:   []string{"hoc", "pure"},
	},
	{
		Converter: converters.Bool,
		Default:   "true",
		Option:    "delete_now",
		Section:   "pool",
		Text:      keywords.NewText(fs, "text/kw/node/delete_now.pure.pod"),
		Types:     []string{"pure"},
	},
	{
		Option:  "pod",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.pure.pod"),
		Types:   []string{"pure"},
	},
	{
		Option:  "volumegroup",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.pure.volumegroup"),
		Types:   []string{"pure"},
	},
	{
		Option:  "wwid_prefix",
		Section: "array",
		Types:   []string{"hoc"},
		Text:    keywords.NewText(fs, "text/kw/node/array.hoc.wwid_prefix"),
	},
	{
		Option:  "volume_id_range_from",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.hoc.volume_id_range_from"),
		Types:   []string{"hoc"},
	},
	{
		Option:  "volume_id_range_to",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.hoc.volume_id_range_to"),
		Types:   []string{"hoc"},
	},
	{
		Default: "",
		Option:  "vsm_id",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.hoc.vsm_id"),
		Types:   []string{"hoc"},
	},
	{
		Option:   "srp",
		Required: true,
		Section:  "pool",
		Text:     keywords.NewText(fs, "text/kw/node/pool.symmetrix.srp"),
		Types:    []string{"symmetrix"},
	},
	{
		Option:  "slo",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.symmetrix.slo"),
		Types:   []string{"symmetrix"},
	},
	{
		Converter: converters.Bool,
		Default:   "false",
		Option:    "srdf",
		Section:   "pool",
		Text:      keywords.NewText(fs, "text/kw/node/pool.symmetrix.srdf"),
		Types:     []string{"symmetrix"},
	},
	{
		Option:  "rdfg",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.symmetrix.rdfg"),
		Types:   []string{"symmetrix"},
	},
	{
		Option:   "diskgroup",
		Required: true,
		Section:  "pool",
		Text:     keywords.NewText(fs, "text/kw/node/pool.diskgroup"),
		Types:    []string{"freenas", "dorado", "hoc", "pure"},
	},
	{
		Converter: converters.Bool,
		Default:   "false",
		Option:    "insecure_tpc",
		Section:   "pool",
		Text:      keywords.NewText(fs, "text/kw/node/pool.freenas.insecure_tpc"),
		Types:     []string{"freenas"},
	},
	{
		Candidates: []string{"inherit", "none", "lz4", "gzip-1", "gzip-2", "gzip-3", "gzip-4", "gzip-5", "gzip-6", "gzip-7", "gzip-8", "gzip-9", "zle", "lzjb"},
		Default:    "inherit",
		Option:     "compression",
		Section:    "pool",
		Text:       keywords.NewText(fs, "text/kw/node/pool.freenas.compression"),
		Types:      []string{"freenas"},
	},
	{
		Default:   "false",
		Converter: converters.Bool,
		Option:    "sparse",
		Section:   "pool",
		Text:      keywords.NewText(fs, "text/kw/node/pool.freenas.sparse"),
		Types:     []string{"freenas"},
	},
	{
		Converter: converters.Size,
		Default:   "512",
		Option:    "blocksize",
		Section:   "pool",
		Text:      keywords.NewText(fs, "text/kw/node/pool.freenas.blocksize"),
		Types:     []string{"freenas"},
	},
	{
		Option:   "name",
		Required: true,
		Section:  "pool",
		Text:     keywords.NewText(fs, "text/kw/node/pool.vg.name"),
		Types:    []string{"vg"},
	},
	{
		DefaultText: keywords.NewText(fs, "text/kw/node/pool.drbd.addr.default"),
		Example:     "1.2.3.4",
		Option:      "addr",
		Scopable:    true,
		Text:        keywords.NewText(fs, "text/kw/node/pool.drbd.addr"),
	},
	{
		Option:  "vg",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.drbd.vg"),
		Types:   []string{"drbd"},
	},
	{
		Option:   "name",
		Required: true,
		Section:  "pool",
		Text:     keywords.NewText(fs, "text/kw/node/pool.zpool.name"),
		Types:    []string{"zpool"},
	},
	{
		Option:  "zpool",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.drbd.zpool"),
		Types:   []string{"drbd"},
	},
	{
		Option:  "path",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.drbd.path"),
		Types:   []string{"drbd"},
	},
	{
		Default: "{var}/pool/share",
		Option:  "path",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.share.path"),
		Types:   []string{"share"},
	},
	{
		Default: "{var}/pool/directory",
		Option:  "path",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.directory.path"),
		Types:   []string{"directory"},
	},
	{
		Example: "templates/vol/mpool-over-loop",
		Option:  "template",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.virtual.template"),
		Types:   []string{"virtual"},
	},
	{
		Converter: converters.List,
		Example:   "container#1.name:container_name env.foo:foo",
		Option:    "volume_env",
		Section:   "pool",
		Text:      keywords.NewText(fs, "text/kw/node/pool.virtual.volume_env"),
		Types:     []string{"virtual"},
	},
	{
		Converter: converters.List,
		Example:   "container#1.name:container_name env.foo:foo",
		Option:    "optional_volume_env",
		Section:   "pool",
		Text:      keywords.NewText(fs, "text/kw/node/pool.virtual.optional_volume_env"),
		Types:     []string{"virtual"},
	},
	{
		Converter: converters.List,
		Default:   "roo rwo rox rwx",
		Option:    "capabilities",
		Section:   "pool",
		Text:      keywords.NewText(fs, "text/kw/node/pool.virtual.capabilities"),
		Types:     []string{"virtual"},
	},
	{
		Default: "{var}/pool/loop",
		Option:  "path",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.loop.path"),
		Types:   []string{"loop"},
	},
	{
		Default: "",
		Option:  "pool_id",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.hoc.pool_id"),
		Types:   []string{"hoc"},
	},
	{
		Default: "xfs",
		Option:  "fs_type",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.fs_type"),
		Types:   []string{"freenas", "dorado", "hoc", "symmetrix", "drbd", "loop", "vg", "pure"},
	},
	{
		Example: "-O largefile",
		Option:  "mkfs_opt",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.mkfs_opt"),
	},
	{
		Option:  "mkblk_opt",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.mkblk_opt"),
	},
	{
		Converter: converters.List,
		Option:    "events",
		Section:   "hook",
		Text:      keywords.NewText(fs, "text/kw/node/hook.events"),
	},
	{
		Converter: converters.Shlex,
		Option:    "command",
		Section:   "hook",
		Text:      keywords.NewText(fs, "text/kw/node/hook.command"),
	},
	{
		Candidates: []string{"bridge", "routed_bridge"},
		Default:    "bridge",
		Option:     "type",
		Section:    "network",
		Text:       keywords.NewText(fs, "text/kw/node/network.type"),
	},
	{
		Option:   "subnet",
		Section:  "network",
		Scopable: true,
		Text:     keywords.NewText(fs, "text/kw/node/network.routed_bridge.subnet"),
		Types:    []string{"routed_bridge"},
	},
	{
		Option:   "gateway",
		Scopable: true,
		Section:  "network",
		Text:     keywords.NewText(fs, "text/kw/node/network.routed_bridge.gateway"),
		Types:    []string{"routed_bridge"},
	},
	{
		Converter: converters.Int,
		Default:   "1024",
		Option:    "ips_per_node",
		Section:   "network",
		Text:      keywords.NewText(fs, "text/kw/node/network.routed_bridge.ips_per_node"),
		Types:     []string{"routed_bridge"},
	},
	{
		Converter: converters.List,
		Default:   "main",
		Example:   "main custom1 custom2",
		Option:    "tables",
		Section:   "network",
		Text:      keywords.NewText(fs, "text/kw/node/network.routed_bridge.tables"),
		Types:     []string{"routed_bridge"},
	},
	{
		DefaultText: keywords.NewText(fs, "text/kw/node/network.routed_bridge.addr.default"),
		Option:      "addr",
		Section:     "network",
		Scopable:    true,
		Text:        keywords.NewText(fs, "text/kw/node/network.routed_bridge.addr"),
		Types:       []string{"routed_bridge"},
	},
	{
		Candidates: []string{"auto", "always", "never"},
		Default:    "auto",
		Option:     "tunnel",
		Section:    "network",
		Text:       keywords.NewText(fs, "text/kw/node/network.routed_bridge.tunnel"),
		Types:      []string{"routed_bridge"},
	},
	{
		Option:  "network",
		Section: "network",
		Text:    keywords.NewText(fs, "text/kw/node/network.network"),
		Types:   []string{"bridge", "routed_bridge"},
	},
	{
		Candidates: []string{"brocade"},
		Option:     "type",
		Required:   true,
		Section:    "switch",
		Text:       keywords.NewText(fs, "text/kw/node/switch.type"),
	},
	{
		Example: "sansw1.my.corp",
		Option:  "name",
		Section: "switch",
		Text:    keywords.NewText(fs, "text/kw/node/switch.brocade.name"),
		Types:   []string{"brocade"},
	},
	{
		Candidates: []string{"telnet", "ssh"},
		Default:    "ssh",
		Example:    "ssh",
		Option:     "method",
		Section:    "switch",
		Text:       keywords.NewText(fs, "text/kw/node/switch.brocade.method"),
		Types:      []string{"brocade"},
	},
	{
		Example:  "admin",
		Option:   "username",
		Required: true,
		Section:  "switch",
		Text:     keywords.NewText(fs, "text/kw/node/switch.brocade.username"),
		Types:    []string{"brocade"},
	},
	{
		Example: "mysec/password",
		Option:  "password",
		Section: "switch",
		Text:    keywords.NewText(fs, "text/kw/node/switch.brocade.password"),
		Types:   []string{"brocade"},
	},
	{
		Example: "/path/to/key",
		Option:  "key",
		Section: "switch",
		Text:    keywords.NewText(fs, "text/kw/node/switch.brocade.key"),
		Types:   []string{"brocade"},
	},
	{
		Candidates: []string{"freenas", "hds", "eva", "nexenta", "vioserver", "centera", "symmetrix", "emcvnx", "netapp", "hp3par", "ibmds", "ibmsvc", "xtremio", "dorado", "hoc"},
		Option:     "type",
		Required:   true,
		Section:    "array",
		Text:       keywords.NewText(fs, "text/kw/node/array.type"),
	},
	{
		Converter: converters.Bool,
		Default:   "false",
		Option:    "compression",
		Section:   "pool",
		Text:      keywords.NewText(fs, "text/kw/node/pool.compression"),
		Types:     []string{"dorado", "hoc"},
	},
	{
		Default: "off",
		Option:  "dedup",
		Section: "pool",
		Text:    keywords.NewText(fs, "text/kw/node/pool.freenas.dedup"),
		Types:   []string{"freenas"},
	},
	{
		Converter: converters.Bool,
		Default:   "false",
		Option:    "dedup",
		Section:   "pool",
		Text:      keywords.NewText(fs, "text/kw/node/pool.dedup"),
		Types:     []string{"dorado", "hoc"},
	},
	{
		Option:   "hypermetrodomain",
		Example:  "HyperMetroDomain_000",
		Required: false,
		Section:  "pool",
		Text:     keywords.NewText(fs, "text/kw/node/pool.dorado.hypermetrodomain"),
		Types:    []string{"dorado"},
	},
	{
		Example:  "https://array.opensvc.com/api/v1.0",
		Option:   "api",
		Required: true,
		Section:  "array",
		Text:     keywords.NewText(fs, "text/kw/node/array.api"),
		Types:    []string{"dorado", "freenas", "hoc", "pure", "xtremio"},
	},
	{
		Example: "http://proxy.mycorp:3158",
		Option:  "http_proxy",
		Section: "array",
		Text:    keywords.NewText(fs, "text/kw/node/array.hoc.http_proxy"),
		Types:   []string{"hoc"},
	},
	{
		Example: "https://proxy.mycorp:3158",
		Option:  "https_proxy",
		Section: "array",
		Text:    keywords.NewText(fs, "text/kw/node/array.hoc.https_proxy"),
		Types:   []string{"hoc"},
	},
	{
		Converter: converters.Int,
		Default:   "30",
		Option:    "retry",
		Section:   "array",
		Text:      keywords.NewText(fs, "text/kw/node/array.hoc.retry"),
		Types:     []string{"hoc"},
	},
	{
		Converter: converters.Duration,
		Default:   "10s",
		Option:    "delay",
		Section:   "array",
		Text:      keywords.NewText(fs, "text/kw/node/array.hoc.delay"),
		Types:     []string{"hoc"},
	},
	{
		Candidates: []string{"VSP G370", "VSP G700", "VSP G900", "VSP F370", "VSP F700", "VSP F900", "VSP G350", "VSP F350", "VSP G800", "VSP F800", "VSP G400", "VSP G600", "VSP F400", "VSP F600", "VSP G200", "VSP G1000", "VSP G1500", "VSP F1500", "Virtual Storage Platform", "HUS VM"},
		Example:    "VSP G350",
		Option:     "model",
		Required:   true,
		Section:    "array",
		Text:       keywords.NewText(fs, "text/kw/node/array.hoc.model"),
		Types:      []string{"hoc"},
	},
	{
		Example:  "root",
		Option:   "username",
		Required: true,
		Section:  "array",
		Text:     keywords.NewText(fs, "text/kw/node/array.username,required"),
		Types:    []string{"centera", "eva", "hds", "ibmds", "ibmsvc", "freenas", "netapp", "nexenta", "vioserver", "xtremio", "dorado", "hoc"},
	},
	{
		Example: "root",
		Option:  "username",
		Section: "array",
		Text:    keywords.NewText(fs, "text/kw/node/array.username,optional"),
		Types:   []string{"emcvnx", "hp3par", "symmetrix"},
	},
	{
		Example:  "bd2c75d0-f0d5-11ee-a362-8b0f2d1b83d7",
		Option:   "client_id",
		Required: true,
		Section:  "array",
		Text:     keywords.NewText(fs, "text/kw/node/array.pure.client_id"),
		Types:    []string{"pure"},
	},
	{
		Example:  "df80ae3a-f0d5-11ee-94c9-b7c8d2f57c4f",
		Option:   "key_id",
		Required: true,
		Section:  "array",
		Text:     keywords.NewText(fs, "text/kw/node/array.pure.key_id"),
		Types:    []string{"pure"},
	},
	{
		Converter: converters.Bool,
		Default:   "false",
		Option:    "insecure",
		Example:   "true",
		Section:   "array",
		Text:      keywords.NewText(fs, "text/kw/node/array.pure.insecure"),
		Types:     []string{"pure", "hoc"},
	},
	{
		Example:  "opensvc",
		Option:   "issuer",
		Required: true,
		Section:  "array",
		Text:     keywords.NewText(fs, "text/kw/node/array.pure.issuer"),
		Types:    []string{"pure"},
	},
	{
		Example:  "system/sec/array1",
		Option:   "secret",
		Required: true,
		Section:  "array",
		Text:     keywords.NewText(fs, "text/kw/node/array.pure.secret"),
		Types:    []string{"pure"},
	},
	{
		Example:  "opensvc",
		Option:   "username",
		Required: true,
		Section:  "array",
		Text:     keywords.NewText(fs, "text/kw/node/array.pure.username"),
		Types:    []string{"pure"},
	},
	{
		Example:  "system/sec/array1",
		Option:   "password",
		Required: true,
		Section:  "array",
		Text:     keywords.NewText(fs, "text/kw/node/array.password,required"),
		Types:    []string{"centera", "eva", "hds", "freenas", "nexenta", "xtremio", "dorado", "hoc"},
	},
	{
		Example: "system/sec/array1",
		Option:  "password",
		Section: "array",
		Text:    keywords.NewText(fs, "text/kw/node/array.password,optional"),
		Types:   []string{"emcvnx", "symmetrix"},
	},
	{
		Converter: converters.Duration,
		Default:   "120s",
		Example:   "10s",
		Option:    "timeout",
		Section:   "array",
		Text:      keywords.NewText(fs, "text/kw/node/array.timeout"),
		Types:     []string{"freenas", "dorado", "hoc"},
	},
	{
		Example: "a09",
		Option:  "name",
		Section: "array",
		Text:    keywords.NewText(fs, "text/kw/node/array.name"),
		Types:   []string{"dorado", "hoc"},
	},
	{
		Example: "00012345",
		Option:  "name",
		Section: "array",
		Text:    keywords.NewText(fs, "text/kw/node/array.symmetrix.name"),
		Types:   []string{"symmetrix"},
	},
	{
		Default: "/usr/symcli",
		Example: "/opt/symcli",
		Option:  "symcli_path",
		Section: "array",
		Text:    keywords.NewText(fs, "text/kw/node/array.symmetrix.symcli_path"),
		Types:   []string{"symmetrix"},
	},
	{
		Example: "MY_SYMAPI_SERVER",
		Option:  "symcli_connect",
		Section: "array",
		Text:    keywords.NewText(fs, "text/kw/node/array.symmetrix.symcli_connect"),
		Types:   []string{"symmetrix"},
	},
	{
		Example:  "centera1",
		Option:   "server",
		Required: true,
		Section:  "array",
		Text:     keywords.NewText(fs, "text/kw/node/array.server"),
		Types:    []string{"centera", "netapp"},
	},
	{
		Example:  "/opt/java/bin/java",
		Option:   "java_bin",
		Required: true,
		Section:  "array",
		Text:     keywords.NewText(fs, "text/kw/node/array.centera.java_bin"),
		Types:    []string{"centera"},
	},
	{
		Example:  "/opt/centera/LIB",
		Option:   "jcass_dir",
		Required: true,
		Section:  "array",
		Text:     keywords.NewText(fs, "text/kw/node/array.centera.jcass_dir"),
		Types:    []string{"centera"},
	},
	{
		Example:    "secfile",
		Candidates: []string{"secfile", "credentials"},
		Default:    "secfile",
		Option:     "method",
		Section:    "array",
		Text:       keywords.NewText(fs, "text/kw/node/array.emcvnx.secfile"),
		Types:      []string{"emcvnx"},
	},
	{
		Example:  "array1-a",
		Option:   "spa",
		Required: true,
		Section:  "array",
		Text:     keywords.NewText(fs, "text/kw/node/array.emcvnx.spa"),
		Types:    []string{"emcvnx"},
	},
	{
		Example:  "array1-b",
		Option:   "spb",
		Required: true,
		Section:  "array",
		Text:     keywords.NewText(fs, "text/kw/node/array.emcvnx.spb"),
		Types:    []string{"emcvnx"},
	},
	{
		Default: "0",
		Example: "1",
		Option:  "scope",
		Section: "array",
		Text:    keywords.NewText(fs, "text/kw/node/array.emcvnx.scope"),
		Types:   []string{"emcvnx"},
	},
	{
		Example:  "evamanager.mycorp",
		Option:   "manager",
		Required: true,
		Section:  "array",
		Text:     keywords.NewText(fs, "text/kw/node/array.eva.manager"),
		Types:    []string{"eva"},
	},
	{
		Example: "/opt/sssu/bin/sssu",
		Option:  "bin",
		Section: "array",
		Text:    keywords.NewText(fs, "text/kw/node/array.eva.bin"),
		Types:   []string{"eva"},
	},
	{
		Example: "/opt/hds/bin/HiCommandCLI",
		Option:  "bin",
		Section: "array",
		Text:    keywords.NewText(fs, "text/kw/node/array.hds.bin"),
		Types:   []string{"hds"},
	},
	{
		Example: "/opt/java",
		Option:  "jre_path",
		Section: "array",
		Text:    keywords.NewText(fs, "text/kw/node/array.hds.jre_path"),
		Types:   []string{"hds"},
	},
	{
		Option:  "name",
		Example: "HUSVM.1234",
		Section: "array",
		Text:    keywords.NewText(fs, "text/kw/node/array.hds.name"),
		Types:   []string{"hds"},
	},
	{
		Example:  "https://hdsmanager/",
		Option:   "url",
		Required: true,
		Section:  "array",
		Text:     keywords.NewText(fs, "text/kw/node/array.hds.url"),
		Types:    []string{"hds"},
	},
	{
		Candidates: []string{"proxy", "cli", "ssh"},
		Default:    "ssh",
		Example:    "ssh",
		Option:     "method",
		Section:    "array",
		Text:       keywords.NewText(fs, "text/kw/node/array.hp3par.method"),
		Types:      []string{"hp3par"},
	},
	{
		DefaultText: keywords.NewText(fs, "text/kw/node/array.hp3par.manager.default"),
		Example:     "mymanager.mycorp",
		Option:      "manager",
		Section:     "array",
		Text:        keywords.NewText(fs, "text/kw/node/array.hp3par.manager"),
		Types:       []string{"hp3par"},
	},
	{
		Example: "/path/to/key",
		Option:  "key",
		Section: "array",
		Text:    keywords.NewText(fs, "text/kw/node/array.hp3par.key"),
		Types:   []string{"hp3par"},
	},
	{
		Example: "/path/to/pwf",
		Option:  "pwf",
		Section: "array",
		Text:    keywords.NewText(fs, "text/kw/node/array.hp3par.pwf"),
		Types:   []string{"hp3par"},
	},
	{
		Default: "3parcli",
		Example: "/path/to/pwf",
		Option:  "cli",
		Section: "array",
		Text:    keywords.NewText(fs, "text/kw/node/array.hp3par.cli"),
		Types:   []string{"hp3par"},
	},
	{
		Example:  "hmc1.mycorp",
		Option:   "hmc1",
		Required: true,
		Section:  "array",
		Text:     keywords.NewText(fs, "text/kw/node/array.ibmds.hmc1"),
		Types:    []string{"ibmds"},
	},
	{
		Example:  "hmc2.mycorp",
		Option:   "hmc2",
		Required: true,
		Section:  "array",
		Text:     keywords.NewText(fs, "text/kw/node/array.ibmds.hmc2"),
		Types:    []string{"ibmds"},
	},
	{
		Example:  "/path/to/key",
		Option:   "key",
		Required: true,
		Section:  "array",
		Text:     keywords.NewText(fs, "text/kw/node/array.key,required"),
		Types:    []string{"netapp", "ibmsvc", "vioserver"},
	},
	{
		Converter: converters.Int,
		Default:   "2000",
		Example:   "2000",
		Option:    "port",
		Section:   "array",
		Text:      keywords.NewText(fs, "text/kw/node/array.nexenta.port"),
		Types:     []string{"nexenta"},
	},
}

var nodeKeywordStore = keywords.Store(append(nodePrivateKeywords, nodeCommonKeywords...))

func (t Node) KeywordLookup(k key.T, sectionType string) keywords.Keyword {
	return keywordLookup(nodeKeywordStore, k, naming.KindInvalid, sectionType)
}
