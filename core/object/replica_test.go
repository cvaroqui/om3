package object_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opensvc/om3/v3/core/actioncontext"
	"github.com/opensvc/om3/v3/core/cluster"
	"github.com/opensvc/om3/v3/core/naming"
	"github.com/opensvc/om3/v3/core/object"
	_ "github.com/opensvc/om3/v3/drivers/resappforking"
	_ "github.com/opensvc/om3/v3/drivers/resfsflag"
	"github.com/opensvc/om3/v3/testhelper"
	"github.com/opensvc/om3/v3/util/hostname"
	"github.com/opensvc/om3/v3/util/key"
)

const testReplicaOf = "store1 3e1b2a54-5c1f-4f0e-9f0a-6d2f4c1b7a90 7c0e41d2-9a1b-4c3d-8e5f-0a1b2c3d4e5f"

func setupReplicaCluster(t *testing.T) {
	t.Helper()
	env := testhelper.Setup(t)
	nodesInfo := fmt.Sprintf(`{%q: {"labels": {}}}`, hostname.Hostname())
	require.NoError(t, os.WriteFile(filepath.Join(env.Root, "var", "nodes_info.json"), []byte(nodesInfo), 0o644))
	cluster.ConfigData.Set(&cluster.Config{Name: "drp", Nodes: []string{hostname.Hostname()}})
}

// replicaConfig is the configuration of an object of a cluster of the nodes
// p1 and p2, which this cluster does not know, naming this node in its
// drpnodes, as its replica holds it when replicaOf is set.
func replicaConfig(replicaOf string) []byte {
	s := fmt.Sprintf(`[DEFAULT]
nodes = p1 p2
drpnodes = %s

[env]
site = prod
site@drpnodes = drp

[fs#1]
type = flag

[app#1]
type = forking
start = /bin/true
check = /bin/true
`, hostname.Hostname())
	if replicaOf != "" {
		s += "\n[DEFAULT]\nreplica_of = " + replicaOf + "\nreplica_grants = admin:test\n"
	}
	return []byte(s)
}

// The instances of a replica are its drpnodes: its nodes are the ones of
// the cluster of its source, unknown here, which the nodes keyword falls
// back from on the node evaluating it. A replica reads the values scoped
// @drpnodes, and the scope of its instances is its drpnodes.
func TestReplicaNodesAreTheDRPNodes(t *testing.T) {
	setupReplicaCluster(t)
	p, err := naming.ParsePath("test/svc/replica")
	require.NoError(t, err)

	o, err := object.NewSvc(p, object.WithConfigData(replicaConfig(testReplicaOf)), object.WithVolatile(true))
	require.NoError(t, err)
	nodes, err := o.Nodes()
	require.NoError(t, err)
	assert.Empty(t, nodes, "the node hosting a replica is none of its nodes")
	drpNodes, err := o.DRPNodes()
	require.NoError(t, err)
	assert.Equal(t, []string{hostname.Hostname()}, drpNodes)
	assert.Equal(t, "drp", o.Config().GetString(key.Parse("env.site")), "the value scoped for the drpnodes")
	assert.Equal(t, "drpnodes", object.ScopeKey(o.Config()).Option)
	scope, err := o.Config().Eval(object.ScopeKey(o.Config()))
	require.NoError(t, err)
	assert.Equal(t, []string{hostname.Hostname()}, scope)
	alerts, err := o.Config().Validate()
	require.NoError(t, err)
	assert.False(t, alerts.HasError(), "nodes unknown to this cluster do not break a replica: %s", alerts.Errors())

	// The same configuration with nothing saying it is a replica, as
	// once promoted: the node is one of its drpnodes still, and none of
	// its nodes, and reads the configuration the same.
	o, err = object.NewSvc(p, object.WithConfigData(replicaConfig("")), object.WithVolatile(true))
	require.NoError(t, err)
	assert.Equal(t, "drpnodes", object.ScopeKey(o.Config()).Option)
	nodes, err = o.Nodes()
	require.NoError(t, err)
	assert.Empty(t, nodes)
	assert.Equal(t, "drp", o.Config().GetString(key.Parse("env.site")))
}

// A node reads the list of nodes it is in, as OpenSVC v2 computed the peers
// of a service: the nodes for one of the nodes, the drpnodes for one of the
// drpnodes, a replica or not. A node in both is one of the nodes, and so is
// one in neither, which the nodes keyword falls back on.
func TestScopeKeyFollowsTheNode(t *testing.T) {
	setupReplicaCluster(t)
	p, err := naming.ParsePath("test/svc/scope")
	require.NoError(t, err)
	here := hostname.Hostname()
	for _, tc := range []struct {
		name, config, scope, site string
		hosted                    bool
	}{
		{"one of the nodes", "nodes = " + here + "\ndrpnodes = d1 d2\n", "nodes", "prod", true},
		{"one of the drpnodes", "nodes = p1 p2\ndrpnodes = " + here + "\n", "drpnodes", "drp", true},
		// A node in both reads a value scoped for the drpnodes when
		// none is scoped for the nodes, as in v2.
		{"in both", "nodes = " + here + "\ndrpnodes = " + here + "\n", "nodes", "drp", true},
		{"in neither", "nodes = p1 p2\ndrpnodes = d1 d2\n", "nodes", "prod", false},
		{"no nodes named", "drpnodes = " + here + "\n", "drpnodes", "drp", true},
		{"nothing named", "", "nodes", "prod", false},
	} {
		for _, replicaOf := range []string{"", "replica_of = " + testReplicaOf + "\nreplica_grants = root\n"} {
			conf := "[DEFAULT]\n" + tc.config + replicaOf + "\n[env]\nsite = prod\nsite@drpnodes = drp\n"
			o, err := object.NewSvc(p, object.WithConfigData([]byte(conf)), object.WithVolatile(true))
			require.NoError(t, err, tc.name)
			assert.Equal(t, tc.scope, object.ScopeKey(o.Config()).Option, "%s, replica %v", tc.name, replicaOf != "")
			assert.Equal(t, tc.site, o.Config().GetString(key.Parse("env.site")), "%s, replica %v", tc.name, replicaOf != "")
			assert.Equal(t, tc.hosted, object.HostedOn(o.Config(), here), "%s", tc.name)
			scope, err := o.Config().Eval(object.ScopeKey(o.Config()))
			require.NoError(t, err)
			assert.Contains(t, scope, here, "%s: the scope of this node names it", tc.name)
		}
	}
}

// The configuration of the source names in its drpnodes nodes of another
// cluster, which the cluster it runs on does not know: it validates there,
// with the keywords scoped for them.
func TestSourceConfigNamingForeignDRPNodesValidates(t *testing.T) {
	setupReplicaCluster(t)
	p, err := naming.ParsePath("test/svc/source")
	require.NoError(t, err)
	conf := fmt.Sprintf(`[DEFAULT]
nodes = %s
drpnodes = d1 d2

[env]
site = prod
site@drpnodes = drp

[fs#1]
type = flag
`, hostname.Hostname())
	o, err := object.NewSvc(p, object.WithConfigData([]byte(conf)), object.WithVolatile(true))
	require.NoError(t, err)
	alerts, err := o.Config().Validate()
	require.NoError(t, err)
	assert.False(t, alerts.HasError(), "%s", alerts.Errors())
	drpNodes, err := o.DRPNodes()
	require.NoError(t, err)
	assert.Empty(t, drpNodes)
	assert.Equal(t, "prod", o.Config().GetString(key.Parse("env.site")))
}

// A replica refuses to start, unless forced: it starts by its promotion,
// and no longer follows once started. An object that is not a replica
// refuses to follow.
func TestReplicaRefusesToStartUnlessForced(t *testing.T) {
	setupReplicaCluster(t)
	p, err := naming.ParsePath("test/svc/replica")
	require.NoError(t, err)
	o, err := object.NewSvc(p, object.WithConfigData(replicaConfig(testReplicaOf)))
	require.NoError(t, err)
	ctx := context.Background()
	err = o.Start(ctx)
	require.ErrorContains(t, err, "a replica starts by its promotion")
	assert.ErrorContains(t, err, "--force")
	assert.ErrorContains(t, o.Start(actioncontext.WithRID(ctx, "fs#1")), "a replica starts by its promotion")
	require.NoError(t, o.Start(actioncontext.WithForce(ctx, true)))

	// Started, it no longer follows: a follow would stop what runs on
	// its heads, and write the last run of its source over them.
	err = o.SyncFollow(ctx)
	require.ErrorContains(t, err, "the replica is started, app#1 up: it no longer follows its source")

	// Its schedule skips the follow rather than fail at every period.
	t.Setenv("OSVC_ACTION_ORIGIN", "daemon/scheduler")
	require.NoError(t, o.SyncFollow(ctx))
	t.Setenv("OSVC_ACTION_ORIGIN", "")

	p, err = naming.ParsePath("test/svc/source")
	require.NoError(t, err)
	src, err := object.NewSvc(p, object.WithConfigData([]byte(fmt.Sprintf("[DEFAULT]\nnodes = %s\n\n[fs#1]\ntype = flag\n", hostname.Hostname()))))
	require.NoError(t, err)
	require.NoError(t, src.Start(ctx))
	assert.ErrorContains(t, src.SyncFollow(ctx), "is not a replica")
}

// The configuration a replica follows is the one of its source, but what
// each recorded of itself and what the daemon set on the replica: the id of
// the replica, the object it follows, the grants it follows it under, and
// the snapshot its provision restores from.
func TestFollowedConfig(t *testing.T) {
	setupReplicaCluster(t)
	p, err := naming.ParsePath("test/svc/replica")
	require.NoError(t, err)
	current, err := object.NewConfigurer(p, object.WithConfigData([]byte(`[DEFAULT]
id = 11111111-1111-1111-1111-111111111111
nodes = p1 p2
drpnodes = d1
restore_from = store1 test/svc/replica aaaa
replica_of = `+testReplicaOf+`
replica_grants = admin:test

[fs#1]
type = flag

[fs#2]
type = flag
`)), object.WithVolatile(true))
	require.NoError(t, err)
	run := []byte(`[DEFAULT]
id = 22222222-2222-2222-2222-222222222222
nodes = p1 p2
drpnodes = d1 d2
restore_from = store9 other/svc/x bbbb
replica_of = store9 x y
replica_grants = root

[fs#1]
type = flag
tags = new

[app#1]
type = forking
start = /bin/true
`)
	b, err := object.FollowedConfig(p, current.Config(), run)
	require.NoError(t, err)
	oc, err := object.NewConfigurer(p, object.WithConfigData(b), object.WithVolatile(true))
	require.NoError(t, err)
	cfg := oc.Config()
	get := func(s string) string { return cfg.Get(key.Parse(s)) }
	assert.Equal(t, "11111111-1111-1111-1111-111111111111", get("id"), "the replica keeps its id")
	assert.Equal(t, testReplicaOf, get("replica_of"), "the source it follows is not the source's to say")
	assert.Equal(t, "admin:test", get("replica_grants"), "nor the grants it is judged under")
	assert.Equal(t, "store1 test/svc/replica aaaa", get("restore_from"))
	assert.Equal(t, "d1 d2", get("drpnodes"), "every other keyword is the one of the source")
	assert.Equal(t, "new", get("fs#1.tags"))
	assert.True(t, cfg.HasSectionString("app#1"))
	assert.False(t, cfg.HasSectionString("fs#2"), "a resource the source no longer has")

	// Applied again, it is the same: the daemon applies it on what the
	// follow computed.
	again, err := object.FollowedConfig(p, current.Config(), b)
	require.NoError(t, err)
	assert.Equal(t, string(b), string(again))
}

// A resource using the heads that is standby on the replica is up on a
// replica stopped, which a follow can not ready: the refusal names it, and
// says how the designer of the object makes it not standby there.
func TestFollowRefusesAStandbyResourceUsingTheHeads(t *testing.T) {
	setupReplicaCluster(t)
	p, err := naming.ParsePath("test/svc/standby")
	require.NoError(t, err)
	conf := fmt.Sprintf(`[DEFAULT]
nodes = p1 p2
drpnodes = %s
replica_of = %s
replica_grants = admin:test

[app#1]
type = forking
start = /bin/true
check = /bin/true
standby = true
`, hostname.Hostname(), testReplicaOf)
	o, err := object.NewSvc(p, object.WithConfigData([]byte(conf)))
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, o.StartStandby(ctx))
	err = o.SyncFollow(ctx)
	require.ErrorContains(t, err, "app#1 up, standby on this node")
	assert.ErrorContains(t, err, "standby@drpnodes = false")
	assert.NotContains(t, err.Error(), "the replica is started")

	// Not a replica started: its schedule does not skip it quietly.
	t.Setenv("OSVC_ACTION_ORIGIN", "daemon/scheduler")
	assert.Error(t, o.SyncFollow(ctx))
}

// The promotion of a replica takes away the keywords making it one, and
// nothing else: the object is then one of this cluster, on the nodes its
// replica was on, its drpnodes, reading the values it read. The source, made
// a replica of it, follows that configuration, where its own nodes are the
// nodes: nothing trades places, there or back.
func TestPromotedConfigKeepsTheSites(t *testing.T) {
	setupReplicaCluster(t)
	p, err := naming.ParsePath("test/svc/replica")
	require.NoError(t, err)

	body, err := object.PromotedConfig(p, replicaConfig(testReplicaOf))
	require.NoError(t, err)
	o, err := object.NewSvc(p, object.WithConfigData(body), object.WithVolatile(true))
	require.NoError(t, err)
	cfg := o.Config()
	assert.False(t, o.IsReplica(), "no longer a replica")
	assert.Empty(t, cfg.Get(object.KeyReplicaOf))
	assert.Empty(t, cfg.Get(object.KeyReplicaGrants))
	assert.Equal(t, "p1 p2", cfg.Get(key.Parse("nodes")), "the nodes are the ones of the source still")
	assert.Equal(t, hostname.Hostname(), cfg.Get(key.Parse("drpnodes")))
	assert.Equal(t, "drpnodes", object.ScopeKey(cfg).Option, "the object runs on its drpnodes")
	scope, err := cfg.Eval(object.ScopeKey(cfg))
	require.NoError(t, err)
	assert.Equal(t, []string{hostname.Hostname()}, scope)
	assert.Equal(t, "drp", cfg.GetString(key.Parse("env.site")), "the instances read the values they read as a replica")
	alerts, err := cfg.Validate()
	require.NoError(t, err)
	assert.False(t, alerts.HasError(), "%s", alerts.Errors())

	// The way back, as the source reads the same configuration, a
	// replica in its turn: one of the nodes.
	back := fmt.Sprintf("[DEFAULT]\nnodes = %s\ndrpnodes = d1 d2\nreplica_of = %s\nreplica_grants = root\n\n[env]\nsite = prod\nsite@drpnodes = drp\n", hostname.Hostname(), testReplicaOf)
	o, err = object.NewSvc(p, object.WithConfigData([]byte(back)), object.WithVolatile(true))
	require.NoError(t, err)
	require.True(t, o.IsReplica())
	assert.Equal(t, "nodes", object.ScopeKey(o.Config()).Option)
	assert.Equal(t, "prod", o.Config().GetString(key.Parse("env.site")))
	nodes, err := o.Nodes()
	require.NoError(t, err)
	assert.Equal(t, []string{hostname.Hostname()}, nodes)
}
