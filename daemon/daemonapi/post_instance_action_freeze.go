package daemonapi

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/opensvc/om3/core/client"
	"github.com/opensvc/om3/core/naming"
	"github.com/opensvc/om3/daemon/api"
)

func (a *DaemonAPI) PostInstanceActionFreeze(ctx echo.Context, nodename, namespace string, kind naming.Kind, name string, params api.PostInstanceActionFreezeParams) error {
	if v, err := assertOperator(ctx, namespace); !v {
		return err
	}
	nodename = a.parseNodename(nodename)
	if a.localhost == nodename {
		return a.postLocalInstanceActionFreeze(ctx, namespace, kind, name, params)
	}
	return a.proxy(ctx, nodename, func(c *client.T) (*http.Response, error) {
		return c.PostInstanceActionFreeze(ctx.Request().Context(), nodename, namespace, kind, name, &params)
	})
}

func (a *DaemonAPI) postLocalInstanceActionFreeze(ctx echo.Context, namespace string, kind naming.Kind, name string, params api.PostInstanceActionFreezeParams) error {
	log := LogHandler(ctx, "PostInstanceActionFreeze")
	var requesterSid uuid.UUID
	p, err := naming.NewPath(namespace, kind, name)
	if err != nil {
		return JSONProblemf(ctx, http.StatusBadRequest, "Invalid parameters", "%s", err)
	}
	log = naming.LogWithPath(log, p)
	args := []string{p.String(), "instance", "freeze"}
	if params.Slave != nil && len(*params.Slave) > 0 {
		args = append(args, "--slave", strings.Join(*params.Slave, ","))
	}
	if params.Slaves != nil && *params.Slaves {
		args = append(args, "--slaves")
	}
	if params.Master != nil && *params.Master {
		args = append(args, "--master")
	}
	if params.RequesterSid != nil {
		requesterSid = *params.RequesterSid
	}
	if sid, err := a.apiExec(ctx, p, requesterSid, args, log); err != nil {
		return JSONProblemf(ctx, http.StatusInternalServerError, "", "%s", err)
	} else {
		return ctx.JSON(http.StatusOK, api.InstanceActionAccepted{SessionID: sid})
	}
}
