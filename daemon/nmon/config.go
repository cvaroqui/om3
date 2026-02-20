package nmon

import (
	"runtime"
	"strings"

	"github.com/opensvc/om3/v3/core/node"
	"github.com/opensvc/om3/v3/core/object"
	"github.com/opensvc/om3/v3/util/key"
)

func (t *Manager) getNodeConfig() node.Config {
	var (
		keyMaintenanceGracePeriod = key.New("node", "maintenance_grace_period")
		keyMaxParallel            = key.New("node", "max_parallel")
		keyMaxKeySize             = key.New("node", "max_key_size")
		keyReadyPeriod            = key.New("node", "ready_period")
		keyRejoinGracePeriod      = key.New("node", "rejoin_grace_period")
		keyEnv                    = key.New("node", "env")
		keySplitAction            = key.New("node", "split_action")
		keySSHKey                 = key.New("node", "sshkey")
		keyPRKey                  = key.New("node", "prkey")
		keyMinAvailMemPct         = key.New("node", "min_avail_mem_pct")
		keyMinAvailSwapPct        = key.New("node", "min_avail_swap_pct")
	)
	cfg := node.Config{}
	cfg.Labels = t.config.SectionMap("labels")
	if d := t.config.GetDuration(keyMaintenanceGracePeriod); d != nil {
		cfg.MaintenanceGracePeriod = *d
	}
	if d := t.config.GetDuration(keyReadyPeriod); d != nil {
		cfg.ReadyPeriod = *d
	}
	if d := t.config.GetDuration(keyRejoinGracePeriod); d != nil {
		cfg.RejoinGracePeriod = *d
	}
	if d := t.config.GetSize(keyMaxKeySize); d != nil {
		cfg.MaxKeySize = *d
	}
	cfg.MinAvailMemPct = t.config.GetInt(keyMinAvailMemPct)
	cfg.MinAvailSwapPct = t.config.GetInt(keyMinAvailSwapPct)
	cfg.MaxParallel = t.config.GetInt(keyMaxParallel)
	cfg.Env = t.config.GetString(keyEnv)
	cfg.SplitAction = t.config.GetString(keySplitAction)
	cfg.SSHKey = t.config.GetString(keySSHKey)
	cfg.PRKey = t.config.GetString(keyPRKey)

	if cfg.MaxParallel == 0 {
		cfg.MaxParallel = runtime.NumCPU()
	}
	if cfg.MaxParallel < MinMaxParallel {
		cfg.MaxParallel = MinMaxParallel
	}

	for _, s := range t.config.SectionStrings() {
		if !strings.HasPrefix(s, "hook#") {
			continue
		}
		t.log.Tracef("analyse config: %s", s)
		hook := node.Hook{Name: s[5:]}
		if hook.Name == "" {
			t.log.Debugf("skip empty hook name for %s", s)
			continue
		}
		hook.Events = t.config.GetStrings(key.New(s, "events"))
		if len(hook.Events) == 0 {
			t.log.Debugf("skip empty hook events for %s", s)
			continue
		}
		hook.Command = t.config.GetStrings(key.New(s, "command"))
		if len(hook.Command) == 0 {
			t.log.Debugf("skip empty hook command for %s", s)
			continue
		}
		cfg.Hooks = append(cfg.Hooks, hook)
		t.log.Tracef("hook %s: %#v", hook.Name, hook)
	}

	node, err := object.NewNode(object.WithVolatile(true))
	if err != nil {
		t.log.Warnf("load node config: %s", err)
	} else {
		for _, e := range node.Schedules() {
			cfg.Schedules = append(cfg.Schedules, e.Config)
		}
		cfg.Collector = node.CollectorRawConfig().AsConfig()
	}

	return cfg
}
