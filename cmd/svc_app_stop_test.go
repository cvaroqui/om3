package cmd

import (
	"github.com/opensvc/testhelper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"opensvc.com/opensvc/config"
	"opensvc.com/opensvc/test_conf_helper"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppStop(t *testing.T) {
	cases := map[string]struct {
		extraArgs       []string
		expectedResults string
	}{
		"logInfo": {
			[]string{"--rid", "app#1"},
			"line1",
		},
		"logError": {
			[]string{"--rid", "app#2"},
			"/bin/ls: ",
		},
		"env": {
			[]string{"--rid", "app#env"},
			"FOO=foo\nBAR=bar",
		},
		"cwd": {
			[]string{"--rid", "app#cwd"},
			"/usr",
		},
	}

	getCmd := func(name string) []string {
		args := []string{"svc", "-s", "svcappforking", "stop", "--color", "no", "--local"}
		args = append(args, cases[name].extraArgs...)
		return args
	}

	if name, ok := os.LookupEnv("TC_NAME"); ok == true {
		td, cleanup := testhelper.Tempdir(t)
		defer cleanup()
		test_conf_helper.InstallSvcFile(t, "svcappforking.conf", filepath.Join(td, "etc", "svcappforking.conf"))

		config.Load(map[string]string{"osvc_root_path": td})
		defer config.Load(map[string]string{})
		origHostname := config.Node.Hostname
		config.Node.Hostname = "node1"
		defer func() { config.Node.Hostname = origHostname }()
		config.Node.Hostname = "node1"
		rootCmd.SetArgs(getCmd(name))
		err := rootCmd.Execute()
		require.Nil(t, err)
	}

	t.Run("logInfo", func(t *testing.T) {
		name := "logInfo"
		var msg string
		t.Logf("run 'om %v'", strings.Join(getCmd(name), " "))
		cmd := exec.Command(os.Args[0], "-test.run=TestAppStop")
		cmd.Env = append(os.Environ(), "TC_NAME="+name)
		out, err := cmd.CombinedOutput()
		exitError, ok := err.(*exec.ExitError)
		if ok {
			msg = string(exitError.Stderr)
		} else {
			msg = ""
		}
		require.Nilf(t, err, "err: '%v', stderr: '%v', out='%v'", err, msg, string(out))
		for _, expected := range strings.Split(cases[name].expectedResults, "\n") {
			assert.Containsf(t, string(out), "| "+expected, "got: '%v'", string(out))
		}
	})

	t.Run("logError", func(t *testing.T) {
		name := "logError"
		t.Logf("run 'om %v'", strings.Join(getCmd(name), " "))
		cmd := exec.Command(os.Args[0], "-test.run=TestAppStop")
		cmd.Env = append(os.Environ(), "TC_NAME="+name)
		out, _ := cmd.CombinedOutput()
		for _, expected := range strings.Split(cases[name].expectedResults, "\n") {
			assert.Containsf(t, string(out), "| "+expected, "got: '%v'", string(out))
			for _, line := range strings.Split(string(out), "\n") {
				if strings.Contains(line, "| "+expected) {
					assert.Containsf(t, line, "ERR", "stderr output line not logged with error level")
				}
			}
		}
	})

	t.Run("exit with error", func(t *testing.T) {
		name := "logError"
		t.Logf("run 'om %v'", strings.Join(getCmd(name), " "))
		cmd := exec.Command(os.Args[0], "-test.run=TestAppStop")
		cmd.Env = append(os.Environ(), "TC_NAME="+name)
		_, err := cmd.CombinedOutput()
		assert.NotNil(t, err)
	})

	t.Run("environment", func(t *testing.T) {
		name := "env"
		t.Logf("run 'om %v'", strings.Join(getCmd(name), " "))
		cmd := exec.Command(os.Args[0], "-test.run=TestAppStop")
		cmd.Env = append(os.Environ(), "TC_NAME="+name)
		out, err := cmd.CombinedOutput()
		require.Nil(t, err)
		for _, expected := range strings.Split(cases[name].expectedResults, "\n") {
			assert.Containsf(t, string(out), "| "+expected, "got: '\n%v'", string(out))
		}
	})

	t.Run("cwd", func(t *testing.T) {
		name := "cwd"
		t.Logf("run 'om %v'", strings.Join(getCmd(name), " "))
		cmd := exec.Command(os.Args[0], "-test.run=TestAppStop")
		cmd.Env = append(os.Environ(), "TC_NAME="+name)
		out, err := cmd.CombinedOutput()
		require.Nil(t, err)
		for _, expected := range strings.Split(cases[name].expectedResults, "\n") {
			assert.Containsf(t, string(out), "| "+expected, "got: '\n%v'", string(out))
		}
	})
}
