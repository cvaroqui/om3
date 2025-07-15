package daemonapi

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/opensvc/om3/core/naming"
	"github.com/opensvc/om3/core/object"
	"github.com/opensvc/om3/core/xconfig"
	"github.com/opensvc/om3/daemon/api"
)

type (
	configProvider interface {
		Config() *xconfig.T
	}
)

func (a *DaemonAPI) GetObjectConfigKeywords(ctx echo.Context, namespace string, kind naming.Kind, name string, params api.GetObjectConfigKeywordsParams) error {
	var (
		err    error
		status int
	)
	store := object.KeywordStoreWithDrivers(kind)
	path := naming.Path{
		Name:      name,
		Namespace: namespace,
		Kind:      kind,
	}
	store, status, err = filterKeywordStore(ctx, store, params.Driver, params.Section, params.Option, path, func() (configProvider, error) {
		var (
			i   any
			err error
		)
		i, err = object.NewConfigurer(path, object.WithVolatile(true))
		if err != nil {
			return nil, err
		}
		return i.(configProvider), nil
	})
	if err != nil {
		return JSONProblemf(ctx, status, http.StatusText(status), "%s", err)
	}
	r := api.KeywordDefinitionList{
		Kind:  "KeywordDefinitionList",
		Items: convertKeywordStore(store),
	}
	return ctx.JSON(http.StatusOK, r)
}
