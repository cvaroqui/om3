package oxcmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/opensvc/om3/core/client"
	"github.com/opensvc/om3/core/commoncmd"
	"github.com/opensvc/om3/core/naming"
	"github.com/opensvc/om3/core/objectselector"
	"github.com/opensvc/om3/daemon/api"
)

type (
	CmdObjectConfigUpdate struct {
		OptsGlobal
		commoncmd.OptsLock
		Delete []string
		Set    []string
		Unset  []string
	}
)

func (t *CmdObjectConfigUpdate) Run(kind string) error {
	if len(t.Delete) == 0 && len(t.Set) == 0 && len(t.Unset) == 0 {
		fmt.Println("no changes requested")
		return nil
	}
	mergedSelector := commoncmd.MergeSelector("", t.ObjectSelector, kind, "")
	c, err := client.New()
	if err != nil {
		return err
	}
	sel := objectselector.New(mergedSelector, objectselector.WithClient(c))
	paths, err := sel.MustExpand()
	if err != nil {
		return err
	}

	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()

	errC := make(chan error)
	doneC := make(chan string)
	todo := len(paths)

	prefix := ""
	noPrefix := len(paths) == 1

	for _, path := range paths {
		go func(p naming.Path) {
			defer func() { doneC <- p.String() }()
			params := api.PatchObjectConfigParams{}
			params.Set = &t.Set
			params.Unset = &t.Unset
			params.Delete = &t.Delete
			response, err := c.PatchObjectConfigWithResponse(ctx, p.Namespace, p.Kind, p.Name, &params)
			if err != nil {
				errC <- err
				return
			}
			switch response.StatusCode() {
			case 200:
				if !noPrefix {
					prefix = path.String() + ": "
				}
				if response.JSON200.IsChanged {
					fmt.Printf("%scommitted\n", prefix)
				} else {
					fmt.Printf("%sunchanged\n", prefix)
				}
			case 400:
				errC <- fmt.Errorf("%s: %s", p, *response.JSON400)
			case 401:
				errC <- fmt.Errorf("%s: %s", p, *response.JSON401)
			case 403:
				errC <- fmt.Errorf("%s: %s", p, *response.JSON403)
			case 500:
				errC <- fmt.Errorf("%s: %s", p, *response.JSON500)
			default:
				errC <- fmt.Errorf("%s: unexpected response: %s", p, response.Status())
			}
		}(path)
	}

	var (
		errs error
		done int
	)

	for {
		select {
		case err := <-errC:
			errs = errors.Join(errs, err)
		case <-doneC:
			done++
			if done == todo {
				return errs
			}
		case <-ctx.Done():
			errs = errors.Join(errs, ctx.Err())
			return errs
		}
	}
}
