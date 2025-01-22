package daemonapi

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/opensvc/om3/core/client"
	"github.com/opensvc/om3/core/instance"
	"github.com/opensvc/om3/core/naming"
	"github.com/opensvc/om3/daemon/rbac"
)

func (a *DaemonAPI) PostObjectActionUnfreeze(ctx echo.Context, namespace string, kind naming.Kind, name string) error {
	if v, err := assertGrant(ctx, rbac.NewGrant(rbac.RoleOperator, namespace), rbac.NewGrant(rbac.RoleAdmin, namespace), rbac.GrantRoot); !v {
		return err
	}
	return a.postObjectAction(ctx, namespace, kind, name, instance.MonitorGlobalExpectThawed, func(c *client.T) (*http.Response, error) {
		return c.PostObjectActionUnfreeze(ctx.Request().Context(), namespace, kind, name)
	})
}
