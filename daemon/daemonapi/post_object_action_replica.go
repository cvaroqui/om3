package daemonapi

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/opensvc/om3/v3/core/backup"
	"github.com/opensvc/om3/v3/core/client"
	"github.com/opensvc/om3/v3/core/instance"
	"github.com/opensvc/om3/v3/core/keyop"
	"github.com/opensvc/om3/v3/core/naming"
	"github.com/opensvc/om3/v3/core/object"
	"github.com/opensvc/om3/v3/core/status"
	"github.com/opensvc/om3/v3/core/xconfig"
	"github.com/opensvc/om3/v3/daemon/api"
	"github.com/opensvc/om3/v3/daemon/discovered"
	"github.com/opensvc/om3/v3/daemon/rbac"
	"github.com/opensvc/om3/v3/util/key"
)

// replicaConsumerGroups are the driver groups of the resources using the
// heads of an object, which a follow writes under.
var replicaConsumerGroups = []string{"app", "container", "share"}

// PostObjectActionReplica makes of an object of this cluster the replica of
// the object another cluster runs: the way back of a cluster a replica was
// promoted over, which follows its successor until promoted in its turn.
//
// It is the create of a replica from a snapshot, on an object there
// already: the object takes the configuration of the latest snapshot of its
// source, as a follow applies it, judged as a write of the requester, an
// administrator of the namespace here and of the one of the object
// followed, and the daemon sets the keywords only it sets, the source and
// the grants of the requester, which the configurations followed from then
// on are judged under.
func (a *DaemonAPI) PostObjectActionReplica(ctx echo.Context, namespace string, kind naming.Kind, name string, params api.PostObjectActionReplicaParams) error {
	if v, err := assertAdmin(ctx, namespace); !v {
		return err
	}
	p, err := naming.NewPath(namespace, kind, name)
	if err != nil {
		return JSONProblemf(ctx, http.StatusBadRequest, "Invalid parameters", "%s", err)
	}
	if instance.MonitorData.GetByPathAndNode(p, a.localhost) == nil {
		// The configuration is read and written by a node holding it.
		if nodename := actionNode(a.localhost, instance.MonitorData.GetByPath(p), hearsPeer); nodename != "" {
			return a.proxy(ctx, nodename, func(c *client.T) (*http.Response, error) {
				return c.PostObjectActionReplica(ctx.Request().Context(), namespace, kind, name, &params)
			})
		}
		if !discovered.Settled() {
			return JSONProblemExpand(ctx, ErrDiscovering)
		}
		return JSONProblem(ctx, http.StatusNotFound, "object not found", "")
	}
	if params.Store == nil || *params.Store == "" {
		return JSONProblemf(ctx, http.StatusBadRequest, "Invalid parameters", "a replica follows its source in a store: name it")
	}
	// Read before anything is checked against it: the write lands only
	// over this file.
	base, err := readConfigBase(p)
	if err != nil {
		return JSONProblemf(ctx, http.StatusInternalServerError, "Read config", "%s", err)
	}
	current := currentConfig(p)
	if current == nil {
		return JSONProblemf(ctx, http.StatusInternalServerError, "Read config", "the configuration of %s can not be read", p)
	}
	if src, ok, _ := object.ReplicaSourceOf(current); ok {
		return JSONProblemf(ctx, http.StatusConflict, "Replica", "%s is a replica already, of the object %s of the cluster %s in store %s", p, src.ObjectID, src.ClusterID, src.Store)
	}
	if up := consumersUp(instance.StatusData.GetByPath(p)); len(up) > 0 {
		return JSONProblemf(ctx, http.StatusConflict, "Replica", "%s up, using the data a replica writes the snapshots of its source under: stop %s first", strings.Join(up, " "), p)
	}
	n, err := object.NewNode(object.WithVolatile(true))
	if err != nil {
		return JSONProblemf(ctx, http.StatusInternalServerError, "New node", "%s", err)
	}
	s, err := n.Store(*params.Store, p)
	if errors.Is(err, object.ErrStoreNotUsable) {
		return JSONProblemf(ctx, http.StatusForbidden, "Replica", "%s", err)
	} else if err != nil {
		return JSONProblemf(ctx, http.StatusBadRequest, "Replica", "%s", err)
	}
	defer s.Close()
	reqCtx := ctx.Request().Context()
	clusterID, _ := n.Cluster()
	src, status, err := replicaSourceFor(ctx, s, clusterID, current, params)
	if err != nil {
		return JSONProblemf(ctx, status, "Replica", "%s", err)
	}
	// The latest snapshot of the source is what says who may follow it,
	// by the namespace it is of, and what the replica is from now on.
	run, err := backup.FindRun(reqCtx, s, backup.RunFilter{ObjectID: src.ObjectID, Cluster: src.ClusterID})
	switch {
	case errors.Is(err, backup.ErrNotFound):
		return JSONProblemf(ctx, http.StatusNotFound, "Replica", "store %s holds no snapshot of the object %s of the cluster %s, whose configuration a replica takes", s.Ref(), src.ObjectID, src.ClusterID)
	case err != nil:
		return JSONProblemf(ctx, http.StatusInternalServerError, "Replica", "%s", err)
	case !mayUseRun(grantsFromContext(ctx), *params.Store, run, clusterID, rbac.RoleAdmin):
		return JSONProblemf(ctx, http.StatusForbidden, "Replica", "snapshot %s is of %s on the cluster %s: following it needs the admin grant of its namespace on this cluster, or root", run.ShortID(), run.Path, run.ClusterName)
	case !run.Has(backup.MemberConfig):
		return JSONProblemf(ctx, http.StatusBadRequest, "Replica", "snapshot %s holds no configuration", run.ShortID())
	}
	saved, err := backup.ReadMember(reqCtx, s, run, backup.MemberConfig)
	if err != nil {
		return JSONProblemf(ctx, http.StatusInternalServerError, "Replica", "read the configuration of snapshot %s: %s", run.ShortID(), err)
	}
	body, err := object.FollowedConfig(p, current, saved)
	if err != nil {
		return JSONProblemf(ctx, http.StatusBadRequest, "Replica", "the configuration of snapshot %s: %s", run.ShortID(), err)
	}
	// The configuration of the source is judged before the keywords of
	// the daemon are set in it, as a write of the requester: what it
	// changes on the nodes of this cluster needs their grants.
	check := a.newConfigCheck(ctx, p, false)
	if !check.rbac(body) {
		return check.answer()
	}
	if !check.stopToChange(body, false) {
		return check.answer()
	}
	grants := grantsFromContext(ctx).AsStringList()
	_, body, err = object.CreatedConfig(p, body, true,
		*keyop.New(object.KeyReplicaOf, keyop.Set, src.String(), 0),
		*keyop.New(object.KeyReplicaGrants, keyop.Set, strings.Join(grants, " "), 0),
	)
	if err != nil {
		return JSONProblemf(ctx, http.StatusInternalServerError, "Replica", "%s", err)
	}
	if err := replicaHostedHere(p, body); err != nil {
		return JSONProblemf(ctx, http.StatusBadRequest, "Replica", "%s: name the nodes of this cluster in the drpnodes of the object followed, as its promotion does of the nodes it took over from", err)
	}
	LogHandler(ctx, "PostObjectActionReplica").Infof("%s: made a replica of the object %s of the cluster %s by %s, following its snapshots in store %s under the grants %s, from snapshot %s", p, src.ObjectID, run.ClusterName, userFromContext(ctx).Username, src.Store, strings.Join(grants, " "), run.ShortID())
	return a.writeObjectConfigFile(ctx, check, body, base, nil, false, false, false)
}

// replicaSourceFor returns the object the request has the object of the
// configuration current follow in the store s: the one it names, or else
// the one the store records as the primary of the object, the cluster
// promoted over this one.
func replicaSourceFor(ctx echo.Context, s backup.Driver, clusterID string, current *xconfig.T, params api.PostObjectActionReplicaParams) (backup.ReplicaSource, int, error) {
	src := backup.ReplicaSource{Store: s.Ref()}
	if params.SourceClusterId != nil {
		src.ClusterID = *params.SourceClusterId
	}
	if params.SourceObjectId != nil {
		src.ObjectID = *params.SourceObjectId
	}
	switch {
	case (src.ClusterID == "") != (src.ObjectID == ""):
		return src, http.StatusBadRequest, fmt.Errorf("a source is named by the id of its cluster and the id of the object: both, or none")
	case src.ClusterID == "":
		a, ok := s.(backup.Annotator)
		if !ok {
			return src, http.StatusBadRequest, fmt.Errorf("store %s: %w, where the primary of the object would be recorded: name the source to follow", s.Ref(), backup.ErrNoDocs)
		}
		objectID := current.Get(key.New("DEFAULT", "id"))
		primary, found, err := backup.ReadPrimary(ctx.Request().Context(), a, clusterID, objectID)
		switch {
		case err != nil:
			return src, http.StatusInternalServerError, err
		case !found:
			return src, http.StatusConflict, fmt.Errorf("store %s records no primary of the object: no replica of it was promoted, name the source to follow", s.Ref())
		case primary.Names(clusterID):
			return src, http.StatusConflict, fmt.Errorf("store %s records this cluster as the primary of the object, epoch %d: it follows none", s.Ref(), primary.Epoch)
		}
		src.ClusterID, src.ObjectID = primary.ClusterID, primary.ObjectID
	}
	if src.ClusterID == clusterID {
		return src, http.StatusBadRequest, fmt.Errorf("a replica is of an object of another cluster")
	}
	return src, 0, nil
}

// consumersUp returns the resources using the heads of the object that are
// up on a node, sorted, as its instances say: the ones a follow would
// write under.
func consumersUp(instances map[string]*instance.Status) []string {
	l := make([]string, 0)
	for nodename, st := range instances {
		if st == nil {
			continue
		}
		for rid, rs := range st.Resources {
			group, _, _ := strings.Cut(rid, "#")
			if slices.Contains(replicaConsumerGroups, group) && rs.Status.Is(status.Up) {
				l = append(l, rid+"@"+nodename)
			}
		}
	}
	slices.Sort(l)
	return l
}
