package daemonapi

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"

	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/ssh"

	"github.com/opensvc/om3/core/client"
)

func (a *DaemonAPI) GetNodeSSHHostkeys(ctx echo.Context, nodename string) error {
	if v, err := assertRoot(ctx); !v {
		return err
	}
	nodename = a.parseNodename(nodename)
	if a.localhost == nodename {
		return a.getLocalSSHHostkeys(ctx)
	}
	return a.proxy(ctx, nodename, func(c *client.T) (*http.Response, error) {
		return c.GetNodeSSHHostkeys(ctx.Request().Context(), nodename)
	})
}

func (a *DaemonAPI) getLocalSSHHostkeys(ctx echo.Context) error {
	log := LogHandler(ctx, "GetNodeSSHHostkeys")
	b := bytes.NewBuffer(nil)
	pubFiles, err := filepath.Glob("/etc/ssh/ssh_host_*_key.pub")
	if err != nil {
		return err
	}
	for _, pubFile := range pubFiles {
		data, err := os.ReadFile(pubFile)
		if err != nil {
			log.Warnf("read %s: %s", pubFile, err)
			continue
		}
		if len(data) == 0 {
			continue
		}
		_, _, _, _, err = ssh.ParseAuthorizedKey(bytes.TrimSpace(data))
		if err != nil {
			log.Warnf("invalid pubkey in file %s: %s", pubFile, err)
			continue
		}
		b.Write(data)
		b.Write([]byte("\n"))
	}
	return ctx.String(http.StatusOK, b.String())
}
