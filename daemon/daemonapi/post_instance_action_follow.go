package daemonapi

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/opensvc/om3/v3/core/client"
	"github.com/opensvc/om3/v3/core/naming"
	"github.com/opensvc/om3/v3/daemon/api"
)

// PostInstanceActionFollow has the instance of a replica follow the object
// it is a replica of. An operator of the namespace asks it: the
// configuration followed is judged under the grants recorded when the
// replica was created, never under the ones of the requester.
func (a *DaemonAPI) PostInstanceActionFollow(ctx echo.Context, nodename, namespace string, kind naming.Kind, name string, params api.PostInstanceActionFollowParams) error {
	if v, err := assertOperator(ctx, namespace); !v {
		return err
	}
	nodename = a.parseNodename(nodename)
	if a.localhost == nodename {
		return a.postLocalInstanceActionFollow(ctx, namespace, kind, name, params)
	}
	return a.proxy(ctx, nodename, func(c *client.T) (*http.Response, error) {
		return c.PostInstanceActionFollow(ctx.Request().Context(), nodename, namespace, kind, name, &params)
	})
}

func (a *DaemonAPI) postLocalInstanceActionFollow(ctx echo.Context, namespace string, kind naming.Kind, name string, params api.PostInstanceActionFollowParams) error {
	log := LogHandler(ctx, "PostInstanceActionFollow")
	var requesterSessionID uuid.UUID
	p, err := naming.NewPath(namespace, kind, name)
	if err != nil {
		return JSONProblemf(ctx, http.StatusBadRequest, "Invalid parameters", "%s", err)
	}
	log = naming.LogWithPath(log, p)
	if v, err := assertConfigUpdatedAt(ctx, p, params.ConfigUpdatedAt); !v {
		return err
	}
	args := []string{p.String(), "instance", "follow"}
	if params.Rid != nil && *params.Rid != "" {
		args = append(args, "--rid", *params.Rid)
	}
	if params.Subset != nil && *params.Subset != "" {
		args = append(args, "--subset", *params.Subset)
	}
	if params.Tag != nil && *params.Tag != "" {
		args = append(args, "--tag", *params.Tag)
	}
	if params.Force != nil && *params.Force {
		args = append(args, "--force")
	}
	if params.SessionID != nil {
		requesterSessionID = *params.SessionID
	}
	if sessionID, execID, err := a.apiExec(ctx, p, requesterSessionID, args, log); err != nil {
		return JSONProblemf(ctx, http.StatusInternalServerError, "", "%s", err)
	} else {
		return ctx.JSON(http.StatusOK, api.InstanceActionAccepted{SessionID: sessionID, ExecID: execID})
	}
}
