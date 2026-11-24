package daemonapi

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/opensvc/om3/v3/core/naming"
	"github.com/opensvc/om3/v3/core/object"
	"github.com/opensvc/om3/v3/core/xconfig"
	"github.com/opensvc/om3/v3/daemon/rbac"
	"github.com/opensvc/om3/v3/util/hostname"
	"github.com/opensvc/om3/v3/util/key"
)

// followedConfig returns the configuration body, the one a run of its source
// saved, as the replica p applies it, and has the rest of the request judged
// under the grants recorded when the replica was created.
//
// A replica follows its source with no requester: the follow action runs as
// root on the node, on a schedule. Judged as root, the configuration of the
// source would be written whatever it holds, and an administrator of a
// namespace of the source cluster would write on this one what nobody
// granted them here. So the write is the one the creator of the replica
// would make: every keyword the configuration changes answers to the grants
// the daemon recorded in replica_grants, and the grants of the request are
// replaced by them from here on, for every check that reads them.
//
// Only root asks it: for any other requester, it would trade their grants
// for the ones of the creator.
func followedConfig(ctx echo.Context, p naming.Path, body []byte) ([]byte, int, error) {
	if !grantsFromContext(ctx).HasGrant(rbac.GrantRoot) {
		return nil, http.StatusForbidden, fmt.Errorf("a configuration followed is written by the follow action of the replica, as root")
	}
	current := currentConfig(p)
	if current == nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("the configuration of %s can not be read", p)
	}
	if _, ok, err := object.ReplicaSourceOf(current); err != nil {
		return nil, http.StatusConflict, err
	} else if !ok {
		return nil, http.StatusConflict, fmt.Errorf("%s is not a replica, it follows no configuration", p)
	}
	body, err := object.FollowedConfig(p, current, body)
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	if err := replicaHostedHere(p, body); err != nil {
		return nil, http.StatusBadRequest, err
	}
	ctx.Set("grants", rbac.NewGrants(current.GetStrings(object.KeyReplicaGrants)...))
	return body, 0, nil
}

// replicaHostedHere refuses the configuration body of the replica p when
// neither its drpnodes nor its nodes name this node: a replica has its
// instances on the nodes of its cluster the configuration of its source
// names, its drpnodes, or its nodes for the object a replica was promoted
// over, and a configuration naming none of this cluster, or no longer this
// node, would leave the replica with no instance to follow from. A nodes
// keyword naming no node of the cluster falls back on the node evaluating
// it, which is not naming it.
func replicaHostedHere(p naming.Path, body []byte) error {
	oc, err := object.NewConfigurer(p, object.WithConfigData(body), object.WithVolatile(true))
	if err != nil {
		return err
	}
	cfg := oc.Config()
	if !object.HostedOn(cfg, hostname.Hostname()) {
		return fmt.Errorf("the drpnodes of the configuration, %q, do not name %s, nor its nodes: the instances of a replica are on the nodes of this cluster the configuration names, its drpnodes on a recovery cluster", cfg.Get(key.New("DEFAULT", "drpnodes")), hostname.Hostname())
	}
	return nil
}

// evalNodes returns the nodes the instances of the object of cfg are on.
func evalNodes(cfg *xconfig.T) ([]string, error) {
	k := object.ScopeKey(cfg)
	v, err := cfg.Eval(k)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", k, err)
	}
	l, _ := v.([]string)
	return l, nil
}
