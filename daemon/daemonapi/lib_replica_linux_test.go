//go:build linux

package daemonapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opensvc/om3/v3/core/naming"
	"github.com/opensvc/om3/v3/core/object"
	"github.com/opensvc/om3/v3/daemon/rbac"
	"github.com/opensvc/om3/v3/util/hostname"
	"github.com/opensvc/om3/v3/util/key"
	"github.com/opensvc/om3/v3/util/plog"

	_ "github.com/opensvc/om3/v3/core/driverdb"
)

const testReplicaOf = "store1 3e1b2a54-5c1f-4f0e-9f0a-6d2f4c1b7a90 7c0e41d2-9a1b-4c3d-8e5f-0a1b2c3d4e5f"

func replicaTestContext(grants rbac.Grants) echo.Context {
	req := httptest.NewRequest(http.MethodPut, "/", nil)
	c := echo.New().NewContext(req, httptest.NewRecorder())
	c.Set("logger", plog.NewDefaultLogger())
	c.Set("grants", grants)
	return c
}

// A configuration a replica follows is judged under the grants recorded
// when the replica was created, never as root, which the follow action
// asking it is: a keyword change those grants do not allow is refused, and
// one they allow passes. What the daemon set on the replica is not the
// source's to change.
func TestAFollowedConfigIsJudgedUnderTheGrantsOfTheReplica(t *testing.T) {
	knownNodes(t)
	defer hostname.Impersonate("node1")()

	p, err := naming.ParsePath("test/svc/replica")
	require.NoError(t, err)
	base := fmt.Sprintf(`[DEFAULT]
id = 11111111-1111-1111-1111-111111111111
nodes = p1 p2
drpnodes = %s
`, hostname.Hostname())
	current := base + "replica_of = " + testReplicaOf + "\nreplica_grants = admin:test\n\n[fs#1]\ntype = flag\n"
	require.NoError(t, os.MkdirAll(filepath.Dir(p.ConfigFile()), 0o755))
	require.NoError(t, os.WriteFile(p.ConfigFile(), []byte(current), 0o644))

	// The source changes a keyword only root writes, and says of the
	// replica what only the daemon of its cluster says.
	run := base + "monitor_action = reboot\nreplica_of = store9 x y\nreplica_grants = root\n\n[fs#1]\ntype = flag\n"
	ctx := replicaTestContext(rbac.Grants{rbac.GrantRoot})
	body, _, err := followedConfig(ctx, p, []byte(run))
	require.NoError(t, err)
	assert.Equal(t, rbac.NewGrants("admin:test"), grantsFromContext(ctx), "the request is judged as the creator of the replica from here on")
	oc, err := object.NewConfigurer(p, object.WithConfigData(body), object.WithVolatile(true))
	require.NoError(t, err)
	assert.Equal(t, testReplicaOf, oc.Config().Get(key.Parse("replica_of")))
	assert.Equal(t, "admin:test", oc.Config().Get(key.Parse("replica_grants")))
	refusals := configRbacRefusals(ctx, p, body)
	require.Len(t, refusals, 1, "%v", refusals)
	assert.ErrorContains(t, errors.Join(refusals...), "monitor_action")
	assert.ErrorContains(t, errors.Join(refusals...), "root")

	// A change an administrator of the namespace makes is followed.
	run = base + "\n[fs#1]\ntype = flag\n\n[env]\nfoo = bar\n"
	ctx = replicaTestContext(rbac.Grants{rbac.GrantRoot})
	body, _, err = followedConfig(ctx, p, []byte(run))
	require.NoError(t, err)
	assert.Empty(t, configRbacRefusals(ctx, p, body))

	// Only the follow action asks it, as root: any other requester
	// would trade its grants for the ones of the creator.
	_, status, err := followedConfig(replicaTestContext(rbac.NewGrants("admin:test")), p, []byte(run))
	require.Error(t, err)
	assert.Equal(t, http.StatusForbidden, status)

	// A configuration no longer naming this node in its drpnodes would
	// leave the replica with no instance here.
	_, status, err = followedConfig(replicaTestContext(rbac.Grants{rbac.GrantRoot}), p, []byte("[DEFAULT]\nnodes = p1 p2\ndrpnodes = d9\n"))
	require.ErrorContains(t, err, "drpnodes")
	assert.Equal(t, http.StatusBadRequest, status)

	// An object that is not a replica follows no configuration.
	require.NoError(t, os.WriteFile(p.ConfigFile(), []byte(base+"\n[fs#1]\ntype = flag\n"), 0o644))
	_, status, err = followedConfig(replicaTestContext(rbac.Grants{rbac.GrantRoot}), p, []byte(run))
	require.ErrorContains(t, err, "is not a replica")
	assert.Equal(t, http.StatusConflict, status)
}

// The daemon alone writes what makes a replica of an object: an
// administrator of the namespace neither sets nor takes away the object it
// follows and the grants it is judged under, which root alone does.
func TestTheReplicaKeywordsAreTheDaemonsToSet(t *testing.T) {
	p, err := naming.ParsePath("test/svc/foo")
	require.NoError(t, err)
	cfgOf := func(config string) *object.Configurer {
		o, err := object.New(p, object.WithConfigData([]byte(config)), object.WithVolatile(true))
		require.NoError(t, err)
		c := o.(object.Configurer)
		return &c
	}
	grants := rbac.NewGrants("admin:test")
	plain := "[DEFAULT]\nid = 11111111-1111-1111-1111-111111111111\n"
	replica := plain + "replica_of = " + testReplicaOf + "\nreplica_grants = root\n"
	err = configRbacChanges(grants, p.Kind, (*cfgOf(plain)).Config(), (*cfgOf(replica)).Config())
	require.Error(t, err)
	assert.ErrorContains(t, err, "replica_of")
	assert.ErrorContains(t, err, "replica_grants")
	err = configRbacChanges(grants, p.Kind, (*cfgOf(replica)).Config(), (*cfgOf(plain)).Config())
	require.Error(t, err, "unset, the object is no longer a replica, which the daemon would start")
	assert.ErrorContains(t, err, "replica_of")
	assert.ErrorContains(t, err, "replica_grants")
	assert.NoError(t, configRbacChanges(rbac.Grants{rbac.GrantRoot}, p.Kind, (*cfgOf(replica)).Config(), (*cfgOf(plain)).Config()))
	assert.NoError(t, configRbacChanges(rbac.Grants{rbac.GrantRoot}, p.Kind, (*cfgOf(plain)).Config(), (*cfgOf(replica)).Config()))
}
