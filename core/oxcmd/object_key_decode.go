package oxcmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"

	"github.com/opensvc/om3/core/client"
	"github.com/opensvc/om3/core/commoncmd"
	"github.com/opensvc/om3/core/naming"
	"github.com/opensvc/om3/core/objectselector"
	"github.com/opensvc/om3/daemon/api"
)

type (
	CmdObjectKeyDecode struct {
		OptsGlobal
		Name string
	}
)

func (t *CmdObjectKeyDecode) Run(kind string) error {
	mergedSelector := commoncmd.MergeSelector("", t.ObjectSelector, kind, "")
	ctx := context.Background()
	c, err := client.New()
	if err != nil {
		return err
	}
	paths, err := objectselector.New(
		mergedSelector,
		objectselector.WithClient(c),
	).Expand()
	if err != nil {
		return err
	}
	for _, path := range paths {
		if !slices.Contains(naming.KindDataStore, path.Kind) {
			continue
		}
		if err := t.RunForPath(ctx, c, path); err != nil {
			return err
		}
	}
	return nil
}

func (t *CmdObjectKeyDecode) RunForPath(ctx context.Context, c *client.T, path naming.Path) error {
	params := api.GetObjectDataKeyParams{
		Name: t.Name,
	}
	response, err := c.GetObjectDataKeyWithResponse(ctx, path.Namespace, path.Kind, path.Name, &params)
	if err != nil {
		return err
	}
	switch response.StatusCode() {
	case http.StatusOK:
		_, err := io.Copy(os.Stdout, bytes.NewReader(response.Body))
		return err
	case http.StatusBadRequest:
		return fmt.Errorf("%s: %s", path, *response.JSON400)
	case http.StatusUnauthorized:
		return fmt.Errorf("%s: %s", path, *response.JSON401)
	case http.StatusForbidden:
		return fmt.Errorf("%s: %s", path, *response.JSON403)
	case http.StatusInternalServerError:
		return fmt.Errorf("%s: %s", path, *response.JSON500)
	case http.StatusNotFound:
		return fmt.Errorf("%s: %s", path, *response.JSON404)
	default:
		return fmt.Errorf("%s: unexpected response: %s", path, response.Status())
	}
}
