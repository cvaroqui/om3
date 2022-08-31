package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"opensvc.com/opensvc/testhelper"
)

func TestAppPrintStatusFlatJson(t *testing.T) {
	type logT struct {
		Level   string
		Message string
	}
	cases := map[string][]logT{
		"withStatusLog": {
			{"info", "FOO"},
		},
		"withoutStatusLog": {},
		"withStatusLogStderr": {
			{"warn", "line1"},
			{"warn", "line2"},
		},
		"withStatusLogAndTimeout": {
			{"warn", "DeadlineExceeded"},
		},
	}
	env := testhelper.Setup(t)
	env.InstallFile("../testdata/svcapp_print_status_status_log.conf", "etc/svcapp.conf")
	args := []string{"svcapp", "print", "status", "-r", "--format", "flat_json"}
	t.Logf("run 'om %v'", strings.Join(args, " "))
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), "GO_TEST_MODE=off", "OSVC_ROOT_PATH="+env.Root)
	out, err := cmd.CombinedOutput()
	require.Nil(t, err, "got: \n%v", string(out))

	for name := range cases {
		t.Run(name, func(t *testing.T) {
			for i, log := range cases[name] {
				prefix := fmt.Sprintf("status.resources.'app#%s'.log[[]%d].", name, i)
				assert.Regexpf(t, prefix+"level = \""+log.Level+"\"", string(out), "got:\n%v", string(out))
				assert.Regexpf(t, prefix+"message = \""+log.Message, string(out), "got:\n%v", string(out))
			}
			line := fmt.Sprintf("status.resources.'app#%s'.log[%d].", name, len(cases[name]))
			assert.NotContainsf(t, string(out), line, "got:\n%v", string(out))
		})
	}
}
