package object_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opensvc/om3/v3/core/actioncontext"
	"github.com/opensvc/om3/v3/core/cluster"
	"github.com/opensvc/om3/v3/core/naming"
	"github.com/opensvc/om3/v3/core/object"
	"github.com/opensvc/om3/v3/core/resource"
	_ "github.com/opensvc/om3/v3/drivers/resappforking"
	_ "github.com/opensvc/om3/v3/drivers/resfsflag"
	"github.com/opensvc/om3/v3/testhelper"
	"github.com/opensvc/om3/v3/util/hostname"
)

// A head a restore in place emptied and did not finish refuses a start of
// the resources using the heads. A start of the heads alone, which a
// restore needs up, is let through, and a forced start takes the head as it
// is, clearing its mark.
func TestStartRefusesAnIncompleteHead(t *testing.T) {
	env := testhelper.Setup(t)
	nodesInfo := fmt.Sprintf(`{%q: {"labels": {}}}`, hostname.Hostname())
	require.NoError(t, os.WriteFile(filepath.Join(env.Root, "var", "nodes_info.json"), []byte(nodesInfo), 0o644))
	cluster.ConfigData.Set(&cluster.Config{Name: "cluster1", Nodes: []string{hostname.Hostname()}})
	p, err := naming.ParsePath("test/svc/incomplete")
	require.NoError(t, err)
	o, err := object.NewSvc(p, object.WithConfigData([]byte(fmt.Sprintf(`[DEFAULT]
nodes = %s

[fs#1]
type = flag

[fs#2]
type = flag

[app#1]
type = forking
start = /bin/true
`, hostname.Hostname()))))
	require.NoError(t, err)
	o.ConfigureResources()
	varDir := o.ResourceByID("fs#2").VarDir()
	marked := func() bool {
		_, ok := resource.HeadIncomplete(varDir)
		return ok
	}
	ctx := context.Background()

	require.NoError(t, resource.MarkHeadIncomplete(varDir, "emptied by run abc"))
	err = o.Start(ctx)
	require.ErrorContains(t, err, "fs#2: incomplete head")
	assert.ErrorContains(t, err, "emptied by run abc")
	assert.ErrorContains(t, err, "--force")
	assert.ErrorContains(t, o.Start(actioncontext.WithRID(ctx, "app#1")), "incomplete head")
	assert.True(t, marked(), "a start refused leaves the mark")

	require.NoError(t, o.Start(actioncontext.WithRID(ctx, "fs#1,fs#2")), "the heads alone, as a restore needs them")
	require.NoError(t, o.Start(actioncontext.WithTo(ctx, "fs")), "to the heads")
	assert.True(t, marked(), "a start of the heads leaves the mark")

	require.NoError(t, o.Start(actioncontext.WithForce(ctx, true)))
	assert.False(t, marked(), "a forced start takes the head as complete")
	require.NoError(t, o.Start(ctx))
}
