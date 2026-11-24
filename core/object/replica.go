package object

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/opensvc/om3/v3/core/actioncontext"
	"github.com/opensvc/om3/v3/core/backup"
	"github.com/opensvc/om3/v3/core/keyop"
	"github.com/opensvc/om3/v3/core/naming"
	"github.com/opensvc/om3/v3/core/resource"
	"github.com/opensvc/om3/v3/core/xconfig"
	"github.com/opensvc/om3/v3/util/hostname"
	"github.com/opensvc/om3/v3/util/key"
)

// A replica is the copy of an object another cluster runs, kept on this
// cluster to take over from it in a disaster: its replica_of keyword names
// the object it follows. Its configuration is the one of its source, word
// for word, so what tells the two apart is that keyword, which the daemon
// sets, and the nodes: the instances of a replica are the drpnodes.

var (
	// KeyReplicaOf and KeyReplicaGrants are the keywords the daemon sets
	// on a replica: the object it follows, and the grants the
	// configurations it follows are judged under.
	KeyReplicaOf     = key.New("DEFAULT", "replica_of")
	KeyReplicaGrants = key.New("DEFAULT", "replica_grants")

	keyNodes    = key.New("DEFAULT", "nodes")
	keyDRPNodes = key.New("DEFAULT", "drpnodes")
)

// ReplicaSourceOf returns what the object of the configuration cfg is a
// replica of, and false when it is none.
func ReplicaSourceOf(cfg *xconfig.T) (backup.ReplicaSource, bool, error) {
	s := cfg.Get(KeyReplicaOf)
	if s == "" {
		return backup.ReplicaSource{}, false, nil
	}
	src, err := backup.ParseReplicaSource(s)
	if err != nil {
		return src, true, err
	}
	return src, true, nil
}

// ScopeKey returns the keyword naming the nodes the instances of the object
// of the configuration cfg are on, as this node reads it: drpnodes when the
// node is one of the drpnodes and none of the nodes, and nodes otherwise.
//
// An object has two lists of nodes, the ones it runs on and the ones it is
// recovered on, and a node reads the list it is in, as OpenSVC v2 computed
// the peers of a service: the nodes for one of the nodes, the drpnodes for
// one of the drpnodes. The designer of the object scopes the keywords so
// the same configuration runs on the ones and the others, and nothing in it
// changes when the object moves: a replica reads the configuration of its
// source as its drpnodes do, promoted it runs on them still, and the
// source, made a replica of it, reads it as its nodes do.
func ScopeKey(cfg *xconfig.T) key.T {
	return ScopeKeyOn(cfg, hostname.Hostname())
}

// ScopeKeyOn is ScopeKey, as the node nodename reads it.
func ScopeKeyOn(cfg *xconfig.T, nodename string) key.T {
	if onDRPNodes(cfg, nodename) {
		return keyDRPNodes
	}
	return keyNodes
}

// HostedOn says the configuration cfg names the node nodename among its
// nodes or its drpnodes: not by the fallback of a nodes keyword naming no
// node of the cluster on the node evaluating it.
func HostedOn(cfg *xconfig.T, nodename string) bool {
	return slices.Contains(expandNodes(cfg, keyNodes), nodename) || slices.Contains(expandNodes(cfg, keyDRPNodes), nodename)
}

// expandNodes returns the nodes the keyword k of cfg names, as written:
// none for a keyword naming no node of the cluster, where its evaluation
// would fall back on the node evaluating it.
func expandNodes(cfg *xconfig.T, k key.T) []string {
	v, err := xconfig.PeersConverter.Convert(cfg.Get(k))
	if err != nil {
		return nil
	}
	l, _ := v.([]string)
	return l
}

// onDRPNodes says the node nodename is one of the drpnodes of cfg, and none
// of its nodes.
//
// The nodes of the other site are nodes of another cluster, which this one
// does not know: they evaluate to none, and the nodes keyword then falls
// back on the node evaluating it. So the nodes are read as written here,
// the fallback aside: a node of the recovery site would otherwise be one of
// the nodes, and read the values scoped for them.
func onDRPNodes(cfg *xconfig.T, nodename string) bool {
	return slices.Contains(expandNodes(cfg, keyDRPNodes), nodename) && !slices.Contains(expandNodes(cfg, keyNodes), nodename)
}

// IsReplica says the object is a replica of an object of another cluster.
func (t *core) IsReplica() bool {
	return t.config.Get(KeyReplicaOf) != ""
}

// withoutDRPNodes returns nodes, the nodes keyword evaluated, as a node of
// the drpnodes reads it: without the drpnodes, itself among them, which the
// fallback of a nodes keyword naming no node of this cluster put there. A
// keyword scoped @nodes is then not read by that node, and the one scoped
// @drpnodes is.
//
// The drpnodes are read as written: evaluating a scoped keyword asks for the
// nodes, which is asking this.
func (t *core) withoutDRPNodes(nodes []string) []string {
	if !onDRPNodes(t.config, hostname.Hostname()) {
		return nodes
	}
	drp := expandNodes(t.config, keyDRPNodes)
	return slices.DeleteFunc(slices.Clone(nodes), func(s string) bool {
		return slices.Contains(drp, s)
	})
}

// checkReplicaStart refuses the start of a replica, unless forced: a
// replica holds the data of the last run it followed, for a promotion to
// start it on once its source is known lost. Started beside its source, it
// would run the object twice, and the next follow would write under it.
func (t *actor) checkReplicaStart(ctx context.Context) error {
	if !t.IsReplica() {
		return nil
	}
	if actioncontext.IsForce(ctx) {
		t.log.Warnf("start a replica, as forced: its source may be running, and it no longer follows it while started")
		return nil
	}
	return fmt.Errorf("%s is a replica: a replica starts by its promotion, or with --force", t.path)
}

// ErrFenced is the refusal of the start of an object whose store records
// another cluster as its primary.
var ErrFenced = errors.New("another cluster is the primary of the object")

// checkFencedStart refuses the start of an object a replica was promoted
// over, unless forced: the store its snapshots go to records the cluster
// promoted as its primary, where the object runs since. Started here too,
// as by the daemon of a site back from the disaster that took it away, the
// object would run twice, on data nothing saves.
//
// The store is asked at a start only, and only by a resource writing
// snapshots into one that keeps such records. A store that does not answer
// refuses no start: the object is not kept down by its backups.
func (t *actor) checkFencedStart(ctx context.Context) error {
	if t.IsReplica() {
		// A replica starts by its promotion, which takes over.
		return nil
	}
	fencers := make([]resource.Fencer, 0)
	for _, r := range t.Resources() {
		if r.IsDisabled() {
			continue
		}
		if fencer, ok := r.(resource.Fencer); ok {
			fencers = append(fencers, fencer)
		}
	}
	return fencedStart(ctx, t.path, fencers, actioncontext.IsForce(ctx), t.log.Warnf)
}

func fencedStart(ctx context.Context, p naming.Path, fencers []resource.Fencer, force bool, warnf func(string, ...any)) error {
	for _, fencer := range fencers {
		reason, err := fencer.FencedBy(ctx)
		switch {
		case err != nil:
			warnf("the primary of the object can not be read, the start goes on: %s", err)
		case reason == "":
		case force:
			warnf("start beside the primary of the object, as forced: %s", reason)
		default:
			return fmt.Errorf("%w: %s, or start %s beside it with --force", ErrFenced, reason, p)
		}
	}
	return nil
}

// FollowedConfig returns the configuration run, the one a run of its source
// saved, as the replica at p holding the configuration current applies it.
//
// What the source recorded of the things it made does not come with it, the
// id it was created with, the disk its provision created: the replica keeps
// the ones it recorded of its own. It keeps the keywords the daemon set on
// it too, which say what it is on this cluster: the snapshot its provision
// restores from, the object it follows and the grants it follows it under.
// Every other keyword is the one of the source: a value of the replica's own
// is written in the configuration of the source, scoped @drpnodes.
func FollowedConfig(p naming.Path, current *xconfig.T, run []byte) ([]byte, error) {
	oc, err := NewConfigurer(p, WithConfigData(run), WithVolatile(true))
	if err != nil {
		return nil, err
	}
	cfg := oc.Config()
	cfg.MaterializeDefaultSection()
	cfg.UnsetRecorded()
	for _, option := range backup.DaemonSetOptions {
		if err := cfg.PrepareUnset(key.New("DEFAULT", option)); err != nil {
			return nil, err
		}
	}
	if err := cfg.PrepareUnset(keyID); err != nil {
		return nil, err
	}
	sets := make([]keyop.T, 0)
	for _, section := range current.SectionStrings() {
		if section != "DEFAULT" && !cfg.HasSectionString(section) {
			// What the replica recorded of a resource the source
			// no longer has goes with the resource.
			continue
		}
		for _, option := range current.Keys(section) {
			k := key.New(section, option)
			keep := section == "DEFAULT" && (option == keyID.Option || slices.Contains(backup.DaemonSetOptions, option))
			if !keep && current.Referrer != nil {
				kw := current.Referrer.KeywordLookup(k, current.SectionType(k))
				keep = kw != nil && kw.Recorded
			}
			if keep {
				sets = append(sets, *keyop.New(k, keyop.Set, current.Get(k), 0))
			}
		}
	}
	if err := cfg.PrepareSet(sets...); err != nil {
		return nil, err
	}
	return cfg.Dump()
}

// PromotedConfig returns the configuration current, the one of a replica,
// as its promotion leaves it: the one of an object of this cluster, which
// its daemon starts. The keywords making it a replica are taken away, the
// object it follows and the grants it follows it under, and nothing else
// changes: its instances stay on the nodes they are on, reading the values
// they read, see ScopeKey.
func PromotedConfig(p naming.Path, current []byte) ([]byte, error) {
	oc, err := NewConfigurer(p, WithConfigData(current), WithVolatile(true))
	if err != nil {
		return nil, err
	}
	cfg := oc.Config()
	if err := cfg.PrepareUnset(KeyReplicaOf, KeyReplicaGrants); err != nil {
		return nil, err
	}
	return cfg.Dump()
}
