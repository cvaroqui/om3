package object

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/opensvc/om3/core/naming"
	"github.com/opensvc/om3/util/key"
	"github.com/opensvc/om3/util/pg"
	"github.com/opensvc/om3/util/stringslice"
	"github.com/opensvc/om3/util/systemd"
)

func pgNameObject(p naming.Path) string {
	return systemd.Escape(fmt.Sprintf("%s.%s", p.Kind, p.Name))
}

func pgNameNamespace(s string) string {
	return systemd.Escape(fmt.Sprintf("ns.%s", s))
}

func pgNameSubset(s string) string {
	return systemd.Escape(fmt.Sprintf("subset.%s", strings.ReplaceAll(s, ":", ".")))
}

func pgNameResource(s string) string {
	return systemd.Escape(strings.ReplaceAll(s, "#", "."))
}

// CGroup path must be systemd compliant so docker --parent-cgroup can be
// set to a nested cgroup using a name without /:
//
// With namespace, with subset:
//
//	/opensvc/opensvc-ns.ns1/opensvc-ns.ns1-svc.s1/opensvc-ns.ns1-svc.s1-subset.g1/opensvc-ns.ns1-svc.s1-subset.g1-app.1
//	/opensvc/opensvc-ns.ns1/opensvc-ns.ns1-svc.s1/opensvc-ns.ns1-svc.s1-subset.g1/opensvc-ns.ns1-svc.s1-subset.g1-container.1
//	/opensvc/opensvc-ns.ns1/opensvc-ns.ns1-svc.s1/opensvc-ns.ns1-svc.s1-subset.g1/opensvc-ns.ns1-svc.s1-subset.g1-container.2
//
// With namespace, without subset:
//
//	/opensvc/opensvc-ns.ns1/opensvc-ns.ns1-svc.s1/opensvc-ns.ns1-svc.s1-container.1
//	/opensvc/opensvc-ns.ns1/opensvc-ns.ns1-svc.s1/opensvc-ns.ns1-svc.s1-app.1
//
// Without namespace, with subset:
//
//	/opensvc/opensvc-svc.s1/opensvc-svc.s1-subset.g1/opensvc-svc.s1-subset.g1-app.1
//	/opensvc/opensvc-svc.s1/opensvc-svc.s1-subset.g1/opensvc-svc.s1-subset.g1-app.2
//
// Without namespace, without subset:
//
//	/opensvc/opensvc-svc.s1/opensvc-svc.s1-app.1
func (t *core) pgConfig(section string) *pg.Config {
	data := pg.Config{}
	data.CPUShares, _ = t.config.EvalNoConv(key.New(section, "pg_cpu_shares"))
	data.CPUs, _ = t.config.EvalNoConv(key.New(section, "pg_cpus"))
	data.Mems, _ = t.config.EvalNoConv(key.New(section, "pg_mems"))
	data.CPUQuota, _ = t.config.EvalNoConv(key.New(section, "pg_cpu_quota"))
	data.MemLimit, _ = t.config.EvalNoConv(key.New(section, "pg_mem_limit"))
	data.VMemLimit, _ = t.config.EvalNoConv(key.New(section, "pg_vmem_limit"))
	data.MemOOMControl, _ = t.config.EvalNoConv(key.New(section, "pg_mem_oom_control"))
	data.MemSwappiness, _ = t.config.EvalNoConv(key.New(section, "pg_mem_swappiness"))
	data.BlockIOWeight, _ = t.config.EvalNoConv(key.New(section, "pg_blkio_weight"))
	subsetName := func(s string) string {
		if s == "" {
			return ""
		}
		l := strings.SplitN(s, ":", 2)
		n := len(l)
		switch n {
		case 2:
			return pgNameSubset(l[n-1])
		case 1:
			return pgNameSubset(s)
		default:
			return ""
		}
	}
	svcPGName := func() []string {
		s := pgNameObject(t.path)
		if t.path.Namespace == naming.NsRoot {
			return []string{"opensvc", "opensvc-" + s}
		}
		ns := pgNameNamespace(t.path.Namespace)
		return []string{
			"opensvc",
			"opensvc-" + ns,
			"opensvc-" + ns + "-" + s,
		}
	}
	subsetPGName := func(s string) []string {
		l := svcPGName()
		name := subsetName(s)
		if name == "" {
			return l
		}
		return append(svcPGName(), l[len(l)-1]+"-"+name)
	}
	resPGName := func(s string) []string {
		ss, _ := t.config.EvalNoConv(key.New(section, "subset"))
		l := subsetPGName(ss)
		return append(subsetPGName(ss), l[len(l)-1]+"-"+pgNameResource(s))
	}
	pgName := func(s string) string {
		var l []string
		switch {
		case section == "":
			l = svcPGName()
		case strings.HasPrefix(s, "subset#"):
			l = subsetPGName(s[7:])
		default:
			l = resPGName(s)
		}
		l = stringslice.Map(l, func(s string) string {
			return s + ".slice"
		})
		return "/" + strings.Join(l, "/")
	}
	data.ID = pgName(section)
	return &data
}

func (t *core) CleanPG(ctx context.Context) {
	mgr := pg.FromContext(ctx)
	if mgr == nil {
		return
	}
	for _, run := range mgr.Clean() {
		if run.Err != nil {
			if errors.Is(run.Err, os.ErrPermission) {
				t.log.Debugf("remove pg %s: still active", run.Config.ID)
			} else {
				t.log.Errorf("remove pg %s error: %s", run.Config.ID, run.Err)
			}
		} else if run.Changed {
			t.log.Debugf("remove pg %s", run.Config.ID)
		}
	}
}
