//go:build linux

package daemonapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opensvc/om3/v3/core/backup"
	"github.com/opensvc/om3/v3/core/instance"
	"github.com/opensvc/om3/v3/core/naming"
	"github.com/opensvc/om3/v3/core/object"
	"github.com/opensvc/om3/v3/core/resource"
	"github.com/opensvc/om3/v3/core/status"
	"github.com/opensvc/om3/v3/core/xconfig"
	"github.com/opensvc/om3/v3/daemon/api"
	"github.com/opensvc/om3/v3/daemon/rbac"
	"github.com/opensvc/om3/v3/drivers/storetar"
	"github.com/opensvc/om3/v3/util/hostname"
	"github.com/opensvc/om3/v3/util/key"
	"github.com/opensvc/om3/v3/util/plog"
)

// A promotion, and the way back, are asked by an administrator of the
// namespace of the object, or root: an operator of it, or an administrator
// of another namespace, is refused before anything is read.
func TestPromoteAndReplicaNeedTheAdminGrant(t *testing.T) {
	a := &DaemonAPI{localhost: "node1"}
	for _, grants := range []rbac.Grants{
		rbac.NewGrants("operator:test"),
		rbac.NewGrants("admin:other"),
		rbac.NewGrants("guest:test"),
		{},
	} {
		rec := httptest.NewRecorder()
		ctx := echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/", nil), rec)
		ctx.Set("logger", plog.NewDefaultLogger())
		ctx.Set("grants", grants)
		require.NoError(t, a.PostObjectActionPromote(ctx, "test", naming.KindSvc, "replica", api.PostObjectActionPromoteParams{}))
		assert.Equal(t, http.StatusForbidden, rec.Code, "promote with %v", grants)

		rec = httptest.NewRecorder()
		ctx = echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/", nil), rec)
		ctx.Set("logger", plog.NewDefaultLogger())
		ctx.Set("grants", grants)
		store := "drp"
		require.NoError(t, a.PostObjectActionReplica(ctx, "test", naming.KindSvc, "replica", api.PostObjectActionReplicaParams{Store: &store}))
		assert.Equal(t, http.StatusForbidden, rec.Code, "replica with %v", grants)

		rec = httptest.NewRecorder()
		ctx = echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/", nil), rec)
		ctx.Set("logger", plog.NewDefaultLogger())
		ctx.Set("grants", grants)
		require.NoError(t, a.PostInstanceActionPromote(ctx, "node1", "test", naming.KindSvc, "replica", api.PostInstanceActionPromoteParams{}))
		assert.Equal(t, http.StatusForbidden, rec.Code, "instance promote with %v", grants)
	}
	for _, grants := range []rbac.Grants{rbac.NewGrants("admin:test"), {rbac.GrantRoot}} {
		ok, _ := assertAdmin(replicaTestContext(grants), "test")
		assert.True(t, ok, "%v", grants)
	}
}

// The write a promotion makes, the replica_of and replica_grants keywords
// taken away, is one the keyword policy refuses to an administrator of the
// namespace asking it as a configuration write: the promotion is the only
// request of theirs that unsets them, and root the only other requester.
func TestThePromotionAloneUnsetsTheReplicaKeywords(t *testing.T) {
	knownNodes(t)
	defer hostname.Impersonate("node1")()
	p, err := naming.ParsePath("test/svc/replica")
	require.NoError(t, err)
	replica := "[DEFAULT]\nid = 11111111-1111-1111-1111-111111111111\nnodes = p1 p2\ndrpnodes = node1\nreplica_of = " + testReplicaOf + "\nreplica_grants = admin:test\n\n[fs#1]\ntype = flag\n"
	promoted, err := object.PromotedConfig(p, []byte(replica))
	require.NoError(t, err)
	cfgOf := func(b []byte) *xconfig.T {
		o, err := object.New(p, object.WithConfigData(b), object.WithVolatile(true))
		require.NoError(t, err)
		return o.(object.Configurer).Config()
	}
	to := cfgOf(promoted)
	assert.Empty(t, to.Get(object.KeyReplicaOf))
	assert.Empty(t, to.Get(object.KeyReplicaGrants))
	assert.Equal(t, "p1 p2", to.Get(key.Parse("nodes")), "nothing else changes")
	assert.Equal(t, "node1", to.Get(key.Parse("drpnodes")))

	err = configRbacChanges(rbac.NewGrants("admin:test"), p.Kind, cfgOf([]byte(replica)), to)
	require.Error(t, err, "an administrator writing the configuration a promotion writes is refused")
	assert.ErrorContains(t, err, "replica_of")
	assert.NoError(t, configRbacChanges(rbac.Grants{rbac.GrantRoot}, p.Kind, cfgOf([]byte(replica)), to))
}

// The node promoting a replica is its placement leader, the node its follow
// runs on: this node when it is, the leader it hears otherwise, and this
// node again when no leader is heard and it has an instance.
func TestPromoteNode(t *testing.T) {
	hears := func(_, nodename string) bool { return nodename != "deaf" }
	leader := &instance.Monitor{IsLeader: true}
	other := &instance.Monitor{}
	assert.Equal(t, "n1", promoteNode("n1", map[string]*instance.Monitor{"n1": leader, "n2": other}, hears))
	assert.Equal(t, "n2", promoteNode("n1", map[string]*instance.Monitor{"n1": other, "n2": leader}, hears))
	assert.Equal(t, "n2", promoteNode("n1", map[string]*instance.Monitor{"n2": leader, "n3": other}, hears))
	assert.Equal(t, "n1", promoteNode("n1", map[string]*instance.Monitor{"n1": other, "deaf": leader}, hears), "a leader not heard is not handed the request")
	assert.Equal(t, "n1", promoteNode("n1", map[string]*instance.Monitor{"n1": other, "n2": other}, hears))
	assert.Equal(t, "n2", promoteNode("n1", map[string]*instance.Monitor{"n2": other, "n3": other}, hears))
	assert.Empty(t, promoteNode("n1", nil, hears))
}

// The options of a promotion: wait_restore is a duration, or "end" for the
// end of the restore, and says the opposite of abort_restore.
func TestPromoteOptions(t *testing.T) {
	yes := true
	str := func(s string) *string { return &s }
	opts, err := promoteOptions(api.PostObjectActionPromoteParams{})
	require.NoError(t, err)
	assert.Equal(t, object.PromoteOptions{}, opts)
	opts, err = promoteOptions(api.PostObjectActionPromoteParams{WaitRestore: str("90s"), DryRun: &yes, Force: &yes})
	require.NoError(t, err)
	assert.Equal(t, object.PromoteOptions{WaitRestore: true, WaitRestoreFor: 90 * time.Second, DryRun: true, Force: true}, opts)
	opts, err = promoteOptions(api.PostObjectActionPromoteParams{WaitRestore: str("end")})
	require.NoError(t, err)
	assert.Equal(t, object.PromoteOptions{WaitRestore: true}, opts)
	_, err = promoteOptions(api.PostObjectActionPromoteParams{WaitRestore: str("soon")})
	assert.Error(t, err)
	_, err = promoteOptions(api.PostObjectActionPromoteParams{WaitRestore: str("1m"), AbortRestore: &yes})
	assert.ErrorContains(t, err, "opposite")
}

// An object is made a replica of the object the store records as its
// primary, the cluster promoted over it, or of the one the request names.
// One nobody was promoted over, or one that is the primary, follows none
// unless told which.
func TestReplicaSourceFor(t *testing.T) {
	ctx := replicaTestContext(rbac.Grants{rbac.GrantRoot})
	cfg, err := xconfig.NewObject("", []byte("[store#drp]\ntype = tar\nurl = file://"+t.TempDir()+"\n"))
	require.NoError(t, err)
	drv := storetar.New()
	drv.SetName("drp")
	drv.SetConfig(cfg)
	s := drv.(*storetar.T)
	current, err := xconfig.NewObject("", []byte("[DEFAULT]\nid = 11111111-1111-1111-1111-111111111111\n"))
	require.NoError(t, err)
	str := func(s string) *string { return &s }

	_, code, err := replicaSourceFor(ctx, s, "prod-id", current, api.PostObjectActionReplicaParams{})
	require.ErrorContains(t, err, "records no primary")
	assert.Equal(t, http.StatusConflict, code)

	src, _, err := replicaSourceFor(ctx, s, "prod-id", current, api.PostObjectActionReplicaParams{SourceClusterId: str("drp-id"), SourceObjectId: str("ob")})
	require.NoError(t, err)
	assert.Equal(t, backup.ReplicaSource{Store: s.Ref(), ClusterID: "drp-id", ObjectID: "ob"}, src)

	_, code, err = replicaSourceFor(ctx, s, "prod-id", current, api.PostObjectActionReplicaParams{SourceClusterId: str("drp-id")})
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, code)
	_, code, err = replicaSourceFor(ctx, s, "prod-id", current, api.PostObjectActionReplicaParams{SourceClusterId: str("prod-id"), SourceObjectId: str("ob")})
	require.ErrorContains(t, err, "another cluster")
	assert.Equal(t, http.StatusBadRequest, code)

	// A replica of the object was promoted on the drp cluster.
	_, err = backup.Promote(context.Background(), s, "prod-id", "11111111-1111-1111-1111-111111111111", backup.Primary{ClusterID: "drp-id", ClusterName: "drp", ObjectID: "ob"})
	require.NoError(t, err)
	src, _, err = replicaSourceFor(ctx, s, "prod-id", current, api.PostObjectActionReplicaParams{})
	require.NoError(t, err)
	assert.Equal(t, backup.ReplicaSource{Store: s.Ref(), ClusterID: "drp-id", ObjectID: "ob"}, src)

	// The cluster promoted follows none.
	promoted, err := xconfig.NewObject("", []byte("[DEFAULT]\nid = ob\n"))
	require.NoError(t, err)
	_, code, err = replicaSourceFor(ctx, s, "drp-id", promoted, api.PostObjectActionReplicaParams{})
	require.ErrorContains(t, err, "records this cluster as the primary")
	assert.Equal(t, http.StatusConflict, code)
}

// An object is not made a replica while a resource using its heads is up
// on a node: a follow would write the snapshots of its source under it.
func TestConsumersUp(t *testing.T) {
	up := resource.Status{Status: status.Up}
	down := resource.Status{Status: status.Down}
	assert.Empty(t, consumersUp(nil))
	assert.Empty(t, consumersUp(map[string]*instance.Status{
		"n1": {Resources: instance.ResourceStatuses{"fs#1": up, "app#1": down, "sync#1": up}},
		"n2": nil,
	}), "the heads up are what a follow needs")
	assert.Equal(t, []string{"app#1@n2", "container#1@n1"}, consumersUp(map[string]*instance.Status{
		"n1": {Resources: instance.ResourceStatuses{"fs#1": up, "container#1": up}},
		"n2": {Resources: instance.ResourceStatuses{"app#1": up, "share#1": down}},
	}))
}

// The nodes a promotion has settle their own follow before it records the
// primary: the other nodes of a replica keeping a copy per node, a flex
// one, that this node hears. A node not heard is skipped, and told.
func TestFollowerNodes(t *testing.T) {
	hears := func(_, nodename string) bool { return nodename != "deaf" }
	monitors := map[string]*instance.Monitor{"n1": {}, "n3": {}, "n2": {}, "deaf": {}}
	asked, skipped := followerNodes("n1", true, monitors, hears)
	assert.Equal(t, []string{"n2", "n3"}, asked)
	assert.Equal(t, []string{"deaf"}, skipped)
	asked, skipped = followerNodes("n1", false, monitors, hears)
	assert.Empty(t, asked, "the nodes of a failover replica hold one copy, its leader's to settle")
	assert.Empty(t, skipped)
	asked, _ = followerNodes("n1", true, map[string]*instance.Monitor{"n1": {}}, hears)
	assert.Empty(t, asked)
}

// Every follower is asked to settle, at once, and all answer before the
// promotion goes on: a refusal of one refuses the promotion, naming the
// node, a failure fails it, and nothing is recorded meanwhile.
func TestSettleFollowers(t *testing.T) {
	var mu sync.Mutex
	asked := make([]string, 0)
	settle := func(fail map[string]error) func(string) (api.Promotion, error) {
		return func(nodename string) (api.Promotion, error) {
			mu.Lock()
			asked = append(asked, nodename)
			mu.Unlock()
			return api.Promotion{Node: nodename, Action: api.PromotionActionAbort}, fail[nodename]
		}
	}
	answers, err := settleFollowers([]string{"n2", "n3"}, settle(nil))
	require.NoError(t, err)
	require.Len(t, answers, 2)
	assert.Equal(t, "n2", answers[0].Node)
	assert.Equal(t, "n3", answers[1].Node)
	assert.ElementsMatch(t, []string{"n2", "n3"}, asked)

	asked = asked[:0]
	_, err = settleFollowers([]string{"n2", "n3", "n4"}, settle(map[string]error{
		"n2": errors.New("connection refused"),
		"n3": promoteRefusal{detail: "the restore in flight writes a head in place"},
	}))
	var refusal promoteRefusal
	require.ErrorAs(t, err, &refusal, "a refusal is answered as one, ahead of a failure")
	assert.ErrorContains(t, err, "node n3: the restore in flight writes a head in place")
	assert.Len(t, asked, 3, "every node is asked, whatever one answers")

	_, err = settleFollowers([]string{"n2"}, settle(map[string]error{"n2": errors.New("connection refused")}))
	require.Error(t, err)
	assert.False(t, errors.As(err, &refusal))
	assert.ErrorContains(t, err, "node n2: connection refused")

	answers, err = settleFollowers(nil, settle(nil))
	require.NoError(t, err)
	assert.Empty(t, answers)
}

// What the promotion of an instance prints is read from the last line of
// its output, with its refusal.
func TestParsePromoteResult(t *testing.T) {
	result, err := parsePromoteResult([]byte("some line\n" + `{"report":{"path":"test/svc/replica","node":"n1","action":"abort","epoch":3,"from_run_id":"run1","restore":{"run_id":"run2","pid":42}},"error":"promotion refused: x","refused":true}` + "\n"))
	require.NoError(t, err)
	assert.Equal(t, "abort", result.Report.Action)
	assert.EqualValues(t, 3, result.Report.Epoch)
	assert.Equal(t, 42, result.Report.Restore.PID)
	assert.True(t, result.Refused)
	_, err = parsePromoteResult(nil)
	assert.Error(t, err)
	_, err = parsePromoteResult([]byte("panic: boom\n"))
	assert.Error(t, err)
}
