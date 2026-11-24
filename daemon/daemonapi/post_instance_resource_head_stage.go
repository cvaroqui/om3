package daemonapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/opensvc/om3/v3/core/client"
	"github.com/opensvc/om3/v3/core/driver"
	"github.com/opensvc/om3/v3/core/env"
	"github.com/opensvc/om3/v3/core/naming"
	"github.com/opensvc/om3/v3/core/object"
	"github.com/opensvc/om3/v3/core/resource"
	"github.com/opensvc/om3/v3/core/resourceid"
	"github.com/opensvc/om3/v3/daemon/api"
	"github.com/opensvc/om3/v3/util/command"
	"github.com/opensvc/om3/v3/util/xsession"
)

// The stage a transfer from another node is written into is opened,
// committed and discarded by the node receiving it, asked by the node
// sending, which is root as every node of the cluster is: the stage is a
// directory of the host root writes into, and its commit replaces the head
// of the instance.

// headStageTimeout bounds a step of the stage of a transfer: the wait for
// the lock of the object, and the step.
const headStageTimeout = 5 * time.Minute

// runHeadStage runs the step of the stage of the transfer name on the local
// instance, replaced by the tests.
var runHeadStage = execHeadStage

func (a *DaemonAPI) PostInstanceResourceHeadStage(ctx echo.Context, nodename, namespace string, kind naming.Kind, name string, rid string, params api.PostInstanceResourceHeadStageParams) error {
	if v, err := assertRoot(ctx); !v {
		return err
	}
	nodename = a.parseNodename(nodename)
	if nodename != a.localhost {
		// The peer waits for the lock of its instance before it
		// answers, longer than a request passed on is waited for.
		return a.proxyLong(ctx, nodename, func(c *client.T) (*http.Response, error) {
			return c.PostInstanceResourceHeadStage(ctx.Request().Context(), nodename, namespace, kind, name, rid, &params)
		})
	}
	result, pb := a.localHeadStage(ctx, namespace, kind, name, rid, params.Name, object.SyncStageOpen, params.Seeded != nil && *params.Seeded)
	if pb != nil {
		return pb.answer(ctx)
	}
	return ctx.JSON(http.StatusOK, api.InstanceHeadStage{
		Dir:      result.Dir,
		HeadRID:  result.HeadRID,
		LinkDest: result.LinkDest,
		Seeded:   result.Seeded,
		Staged:   result.Staged,
	})
}

func (a *DaemonAPI) PostInstanceResourceHeadCommit(ctx echo.Context, nodename, namespace string, kind naming.Kind, name string, rid string, params api.PostInstanceResourceHeadCommitParams) error {
	if v, err := assertRoot(ctx); !v {
		return err
	}
	nodename = a.parseNodename(nodename)
	if nodename != a.localhost {
		return a.proxyLong(ctx, nodename, func(c *client.T) (*http.Response, error) {
			return c.PostInstanceResourceHeadCommit(ctx.Request().Context(), nodename, namespace, kind, name, rid, &params)
		})
	}
	if _, pb := a.localHeadStage(ctx, namespace, kind, name, rid, params.Name, object.SyncStageCommit, false); pb != nil {
		return pb.answer(ctx)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (a *DaemonAPI) PostInstanceResourceHeadDiscard(ctx echo.Context, nodename, namespace string, kind naming.Kind, name string, rid string, params api.PostInstanceResourceHeadDiscardParams) error {
	if v, err := assertRoot(ctx); !v {
		return err
	}
	nodename = a.parseNodename(nodename)
	if nodename != a.localhost {
		return a.proxyLong(ctx, nodename, func(c *client.T) (*http.Response, error) {
			return c.PostInstanceResourceHeadDiscard(ctx.Request().Context(), nodename, namespace, kind, name, rid, &params)
		})
	}
	if _, pb := a.localHeadStage(ctx, namespace, kind, name, rid, params.Name, object.SyncStageDiscard, false); pb != nil {
		return pb.answer(ctx)
	}
	return ctx.NoContent(http.StatusNoContent)
}

// headStageProblem is why a step of the stage of a transfer is not done.
type headStageProblem struct {
	code          int
	title, detail string
}

func (t *headStageProblem) answer(ctx echo.Context) error {
	return JSONProblemf(ctx, t.code, t.title, "%s", t.detail)
}

// localHeadStage does the step of the stage of the transfer on the local
// instance.
func (a *DaemonAPI) localHeadStage(ctx echo.Context, namespace string, kind naming.Kind, name, rid, stage, step string, seeded bool) (resource.SyncStage, *headStageProblem) {
	none := resource.SyncStage{}
	p, err := naming.NewPath(namespace, kind, name)
	if err != nil {
		return none, &headStageProblem{http.StatusBadRequest, "Invalid parameters", err.Error()}
	}
	// The rid and the name of the transfer are arguments of a command.
	if id, err := resourceid.Parse(rid); err != nil || id.DriverGroup() != driver.GroupSync {
		return none, &headStageProblem{http.StatusBadRequest, "Invalid parameters", fmt.Sprintf("rid %q is not the one of a sync resource", rid)}
	}
	if _, err := uuid.Parse(stage); err != nil {
		return none, &headStageProblem{http.StatusBadRequest, "Invalid parameters", fmt.Sprintf("name %q is not a session id", stage)}
	}
	if !p.Exists() {
		return none, &headStageProblem{http.StatusNotFound, "Object not found", p.String()}
	}
	reqCtx, cancel := context.WithTimeout(ctx.Request().Context(), headStageTimeout)
	defer cancel()
	result, err := runHeadStage(reqCtx, ctx, p, rid, stage, step, seeded)
	switch {
	case err != nil:
		return none, &headStageProblem{http.StatusInternalServerError, "Head stage", err.Error()}
	case result.Discarded:
		return none, &headStageProblem{http.StatusConflict, "Stage discarded", result.Error}
	case result.Error != "":
		return none, &headStageProblem{http.StatusInternalServerError, "Head stage", result.Error}
	}
	return result.SyncStage, nil
}

// execHeadStage runs the step as "om <path> instance sync-stage", in a
// process of its own: the step takes the lock of the object, which the
// daemon holding it would lose to any other handle it closes on its file.
func execHeadStage(ctx context.Context, ectx echo.Context, p naming.Path, rid, stage, step string, seeded bool) (object.SyncStageResult, error) {
	var result object.SyncStageResult
	execname, err := os.Executable()
	if err != nil {
		return result, fmt.Errorf("can't detect om execname: %w", err)
	}
	args := []string{p.String(), "instance", "sync-stage", "--rid", rid, "--name", stage, "--step", step}
	if seeded {
		args = append(args, "--seeded")
	}
	log := naming.LogWithPath(LogHandler(ectx, "PostInstanceResourceHeadStage"), p)
	sessionID := xsession.NewSessionID(uuid.Nil)
	cmd := command.New(
		command.WithName(execname),
		command.WithArgs(args),
		command.WithLogger(log),
		command.WithContext(ctx),
		command.WithBufferedStdout(),
		command.WithVarEnv(
			env.ActionOriginDaemonAPI.Var(),
			sessionID.Var(),
			"OSVC_REQUEST_ID="+fmt.Sprint(ectx.Get("uuid")),
		),
	)
	log.Infof("-> exec %s", cmd)
	if err := cmd.Run(); err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return result, cerr
		}
		return result, fmt.Errorf("%s: %w", cmd, err)
	}
	lines := strings.Split(strings.TrimSpace(string(cmd.Stdout())), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if last == "" {
		return result, errors.New("the step of the stage said nothing")
	}
	if err := json.Unmarshal([]byte(last), &result); err != nil {
		return result, fmt.Errorf("%s: %w", cmd, err)
	}
	return result, nil
}
