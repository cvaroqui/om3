package commoncmd

import (
	"context"
	"fmt"

	"github.com/opensvc/om3/core/client"
	"github.com/opensvc/om3/core/naming"
	"github.com/opensvc/om3/daemon/api"
	"github.com/opensvc/om3/util/xsession"
)

func ObjectInstanceDeleteRemoteFunc(ctx context.Context, p naming.Path, nodename string) (interface{}, error) {
	c, err := client.New()
	if err != nil {
		return nil, err
	}
	params := api.PostInstanceActionDeleteParams{}
	{
		sid := xsession.ID
		params.RequesterSid = &sid
	}
	response, err := c.PostInstanceActionDeleteWithResponse(ctx, nodename, p.Namespace, p.Kind, p.Name, &params)
	if err != nil {
		return nil, err
	}
	switch {
	case response.JSON200 != nil:
		return *response.JSON200, nil
	case response.JSON401 != nil:
		return nil, fmt.Errorf("%s: node %s: %s", p, nodename, *response.JSON401)
	case response.JSON403 != nil:
		return nil, fmt.Errorf("%s: node %s: %s", p, nodename, *response.JSON403)
	case response.JSON500 != nil:
		return nil, fmt.Errorf("%s: node %s: %s", p, nodename, *response.JSON500)
	default:
		return nil, fmt.Errorf("%s: node %s: unexpected response: %s", p, nodename, response.Status())
	}
}
