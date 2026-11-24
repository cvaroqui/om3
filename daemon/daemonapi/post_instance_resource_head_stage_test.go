package daemonapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opensvc/om3/v3/core/naming"
	"github.com/opensvc/om3/v3/core/object"
	"github.com/opensvc/om3/v3/core/rawconfig"
	"github.com/opensvc/om3/v3/core/resource"
	"github.com/opensvc/om3/v3/daemon/api"
	"github.com/opensvc/om3/v3/daemon/rbac"
	"github.com/opensvc/om3/v3/testhelper"
	"github.com/opensvc/om3/v3/util/plog"
)

const headStageTestName = "11111111-2222-3333-4444-555555555555"

func headStageTestContext(grants rbac.Grants) (echo.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	ctx := echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/", nil), rec)
	ctx.Set("grants", grants)
	ctx.Set("logger", plog.NewDefaultLogger())
	return ctx, rec
}

// headStageCalls are the three requests of the stage of a transfer.
func headStageCalls(a *DaemonAPI, rid, name string) map[string]func(echo.Context) error {
	return map[string]func(echo.Context) error{
		object.SyncStageOpen: func(ctx echo.Context) error {
			return a.PostInstanceResourceHeadStage(ctx, "localhost", "test", naming.KindSvc, "s1", rid, api.PostInstanceResourceHeadStageParams{Name: name})
		},
		object.SyncStageCommit: func(ctx echo.Context) error {
			return a.PostInstanceResourceHeadCommit(ctx, "localhost", "test", naming.KindSvc, "s1", rid, api.PostInstanceResourceHeadCommitParams{Name: name})
		},
		object.SyncStageDiscard: func(ctx echo.Context) error {
			return a.PostInstanceResourceHeadDiscard(ctx, "localhost", "test", naming.KindSvc, "s1", rid, api.PostInstanceResourceHeadDiscardParams{Name: name})
		},
	}
}

// The stage of a transfer is opened, committed and discarded by root alone,
// as the nodes of the cluster are: the administrator of the namespace of
// the object is refused, before anything runs.
func TestHeadStageNeedsTheRootGrant(t *testing.T) {
	ran := false
	defer func(fn func(context.Context, echo.Context, naming.Path, string, string, string, bool) (object.SyncStageResult, error)) {
		runHeadStage = fn
	}(runHeadStage)
	runHeadStage = func(context.Context, echo.Context, naming.Path, string, string, string, bool) (object.SyncStageResult, error) {
		ran = true
		return object.SyncStageResult{}, nil
	}
	a := &DaemonAPI{localhost: "node1"}
	for _, grants := range []rbac.Grants{
		rbac.NewGrants("admin:test"),
		rbac.NewGrants("operator:test"),
		rbac.NewGrants("guest:test"),
		rbac.NewGrants("squatter"),
		{},
	} {
		for step, call := range headStageCalls(a, "sync#1", headStageTestName) {
			ctx, rec := headStageTestContext(grants)
			require.NoError(t, call(ctx))
			assert.Equal(t, http.StatusForbidden, rec.Code, "%s with %v", step, grants)
		}
	}
	assert.False(t, ran)
}

// The step runs for root, on a sync resource of an object of the node, the
// name of the transfer being a session id: the rid and the name are the
// arguments of a command. A stage discarded since it was opened is a
// conflict, which the node sending tells from a failure.
func TestHeadStageAnswers(t *testing.T) {
	testhelper.Setup(t)
	t.Cleanup(func() { rawconfig.Load(map[string]string{}) })
	p := naming.Path{Namespace: "test", Kind: naming.KindSvc, Name: "s1"}
	require.NoError(t, os.MkdirAll(filepath.Dir(p.ConfigFile()), 0755))
	require.NoError(t, os.WriteFile(p.ConfigFile(), []byte("[DEFAULT]\nid = 11111111-1111-1111-1111-111111111111\n"), 0644))

	var steps []string
	result := object.SyncStageResult{SyncStage: resource.SyncStage{Staged: true, Dir: "/srv/.data.om-stage", LinkDest: "/srv/data", HeadRID: "fs#1"}}
	defer func(fn func(context.Context, echo.Context, naming.Path, string, string, string, bool) (object.SyncStageResult, error)) {
		runHeadStage = fn
	}(runHeadStage)
	runHeadStage = func(_ context.Context, _ echo.Context, path naming.Path, rid, name, step string, _ bool) (object.SyncStageResult, error) {
		assert.Equal(t, p, path)
		assert.Equal(t, "sync#1", rid)
		assert.Equal(t, headStageTestName, name)
		steps = append(steps, step)
		return result, nil
	}
	a := &DaemonAPI{localhost: "node1"}
	root := rbac.Grants{rbac.GrantRoot}

	calls := headStageCalls(a, "sync#1", headStageTestName)
	ctx, rec := headStageTestContext(root)
	require.NoError(t, calls[object.SyncStageOpen](ctx))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var answer api.InstanceHeadStage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &answer))
	assert.Equal(t, api.InstanceHeadStage{Staged: true, Dir: "/srv/.data.om-stage", LinkDest: "/srv/data", HeadRID: "fs#1"}, answer)
	for _, step := range []string{object.SyncStageCommit, object.SyncStageDiscard} {
		ctx, rec := headStageTestContext(root)
		require.NoError(t, calls[step](ctx))
		assert.Equal(t, http.StatusNoContent, rec.Code, step)
	}
	assert.Equal(t, []string{object.SyncStageOpen, object.SyncStageCommit, object.SyncStageDiscard}, steps)

	result = object.SyncStageResult{Error: headStageTestName + ": stage discarded", Discarded: true}
	ctx, rec = headStageTestContext(root)
	require.NoError(t, calls[object.SyncStageCommit](ctx))
	assert.Equal(t, http.StatusConflict, rec.Code)
	result = object.SyncStageResult{Error: "stage fs#1: tank/data is not mounted"}
	ctx, rec = headStageTestContext(root)
	require.NoError(t, calls[object.SyncStageOpen](ctx))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "is not mounted")

	steps = nil
	for name, calls := range map[string]map[string]func(echo.Context) error{
		"a rid of another group":    headStageCalls(a, "fs#1", headStageTestName),
		"a rid that is none":        headStageCalls(a, "--force", headStageTestName),
		"a name that is no session": headStageCalls(a, "sync#1", "--name"),
	} {
		for step, call := range calls {
			ctx, rec := headStageTestContext(root)
			require.NoError(t, call(ctx))
			assert.Equal(t, http.StatusBadRequest, rec.Code, "%s: %s", name, step)
		}
	}
	require.NoError(t, os.Remove(p.ConfigFile()))
	for step, call := range calls {
		ctx, rec := headStageTestContext(root)
		require.NoError(t, call(ctx))
		assert.Equal(t, http.StatusNotFound, rec.Code, "no object: %s", step)
	}
	assert.Empty(t, steps, "nothing runs for a request refused")
}
