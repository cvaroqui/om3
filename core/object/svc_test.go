package object_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"opensvc.com/opensvc/cmd"
	"opensvc.com/opensvc/core/actioncontext"
	"opensvc.com/opensvc/core/object"
	"opensvc.com/opensvc/testhelper"

	_ "opensvc.com/opensvc/core/driverdb"
	"opensvc.com/opensvc/core/path"
	"opensvc.com/opensvc/core/slog"
	"opensvc.com/opensvc/util/file"
	"opensvc.com/opensvc/util/key"
	"opensvc.com/opensvc/util/xsession"
)

var sectionApp0 = []byte(`
[app#0]
start = /usr/bin/touch {env.flag0}
stop = /usr/bin/rm -f {env.flag0}
check = /usr/bin/test -f {env.flag0}
`)

var sectionApp1 = []byte(`
[app#1]
start = /usr/bin/touch {env.flag1}
stop = /usr/bin/rm -f {env.flag1}
check = /usr/bin/test -f {env.flag1}
`)

var sectionEnv = []byte(`
[env]
flag0 = /tmp/{fqdn}.0
flag1 = /tmp/{fqdn}.1
`)

func TestMain(m *testing.M) {
	testhelper.Main(m, cmd.ExecuteArgs)
}

func TestAppStart(t *testing.T) {
	testhelper.Setup(t)
	t.Run("conf1", func(t *testing.T) {
		var conf []byte
		conf = append(conf, sectionApp0...)
		conf = append(conf, sectionApp1...)
		conf = append(conf, sectionEnv...)

		p, err := path.Parse("conf1")
		assert.NoError(t, err)

		s, err := object.NewSvc(p, object.WithConfigData(conf))
		assert.NoError(t, err)

		fpath := s.Config().GetString(key.T{"env", "flag0"})
		assert.NotEqual(t, fpath, "")

		require.NoErrorf(t, os.RemoveAll(fpath), "%s should not exist before start", fpath)

		ctx := context.Background()
		ctx = actioncontext.WithForce(ctx, true)
		ctx = actioncontext.WithRID(ctx, "app#0")
		err = s.Start(ctx)
		assert.NoErrorf(t, err, "Start() should not err")
		require.True(t, file.Exists(fpath), "%s should exist after start", fpath)
		events, err := slog.GetEventsFromFile(p.LogFile(), map[string]interface{}{"sid": xsession.ID})
		assert.NoError(t, err)
		assert.Truef(t, events.MatchString("cmd", ".*touch.*"), "logs should contain a cmd~/touch/ event")
	})
}
