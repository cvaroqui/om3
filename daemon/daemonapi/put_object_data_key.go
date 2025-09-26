package daemonapi

import (
	"errors"
	"io/ioutil"
	"net/http"
	"slices"

	"github.com/labstack/echo/v4"

	"github.com/opensvc/om3/core/instance"
	"github.com/opensvc/om3/core/naming"
	"github.com/opensvc/om3/core/object"
	"github.com/opensvc/om3/daemon/api"
)

func (a *DaemonAPI) PutObjectDataKey(ctx echo.Context, namespace string, kind naming.Kind, name string, params api.PutObjectDataKeyParams) error {
	log := LogHandler(ctx, "PutObjectDataKey")

	if v, err := assertAdmin(ctx, namespace); !v {
		return err
	}

	p, err := naming.NewPath(namespace, kind, name)
	if err != nil {
		return JSONProblemf(ctx, http.StatusBadRequest, "Invalid parameters", "%s", err)
	}
	log = naming.LogWithPath(log, p)

	instanceConfigData := instance.ConfigData.GetByPath(p)

	if _, ok := instanceConfigData[a.localhost]; ok {
		ks, err := object.NewDataStore(p)

		switch {
		case errors.Is(err, object.ErrWrongType):
			return JSONProblemf(ctx, http.StatusBadRequest, "NewDataStore", "%s", err)
		case err != nil:
			return JSONProblemf(ctx, http.StatusInternalServerError, "NewDataStore", "%s", err)
		}

		ok, err := a.CheckDataSize(ctx)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}

		keys, err := ks.AllKeys()
		if err != nil {
			return JSONProblemf(ctx, http.StatusInternalServerError, "AllKeys", "%s", err)
		}
		if !slices.Contains(keys, params.Name) {
			return JSONProblemf(ctx, http.StatusNotFound, "ChangeKey", "%s: %s. consider using the POST method.", params.Name, err)
		}
		b, err := ioutil.ReadAll(ctx.Request().Body)
		if err != nil {
			return JSONProblemf(ctx, http.StatusInternalServerError, "ReadAll", "%s: %s", params.Name, err)
		}
		err = ks.ChangeKey(params.Name, b)
		switch {
		case errors.Is(err, object.ErrKeyEmpty):
			return JSONProblemf(ctx, http.StatusBadRequest, "ChangeKey", "%s: %s", params.Name, err)
		case err != nil:
			return JSONProblemf(ctx, http.StatusInternalServerError, "ChangeKey", "%s: %s", params.Name, err)
		default:
			return ctx.NoContent(http.StatusNoContent)
		}
	}

	for nodename := range instanceConfigData {
		c, err := a.newProxyClient(ctx, nodename)
		if err != nil {
			return JSONProblemf(ctx, http.StatusInternalServerError, "New client", "%s: %s", nodename, err)
		}
		if resp, err := c.PutObjectDataKeyWithBodyWithResponse(ctx.Request().Context(), namespace, kind, name, &params, "application/octet-stream", ctx.Request().Body); err != nil {
			return JSONProblemf(ctx, http.StatusInternalServerError, "Request peer", "%s: %s", nodename, err)
		} else if len(resp.Body) > 0 {
			return ctx.JSONBlob(resp.StatusCode(), resp.Body)
		}
	}

	return nil
}
