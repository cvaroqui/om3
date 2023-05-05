package commands

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/pkg/errors"

	"github.com/opensvc/om3/core/client"
	"github.com/opensvc/om3/core/event"
	"github.com/opensvc/om3/core/object"
	"github.com/opensvc/om3/daemon/api"
	"github.com/opensvc/om3/daemon/daemonenv"
	"github.com/opensvc/om3/daemon/msgbus"
	"github.com/opensvc/om3/util/hostname"
)

type (
	CmdDaemonLeave struct {
		CmdDaemonCommon

		// Timeout is the maximum duration for leave
		Timeout time.Duration

		// ApiNode is a cluster node where the leave request will be posted
		ApiNode string

		cli       *client.T
		localhost string
		evReader  event.ReadCloser
	}
)

func (t *CmdDaemonLeave) Run() (err error) {
	if err = t.checkParams(); err != nil {
		return err
	}
	t.cli, err = client.New(
		client.WithURL(daemonenv.UrlHttpNode(t.ApiNode)),
	)
	if err != nil {
		return
	}

	if t.isRunning() {
		if err := t.nodeDrain(); err != nil {
			return err
		}
	}

	t.localhost = hostname.Hostname()
	ctx, cancel := context.WithTimeout(context.Background(), t.Timeout)
	defer cancel()

	if err := t.setEvReader(); err != nil {
		return err
	}
	defer func() {
		_ = t.evReader.Close()
	}()

	if err := t.leave(); err != nil {
		return err
	}
	if err := t.waitResult(ctx); err != nil {
		return err
	}

	if err := t.stopDaemon(); err != nil {
		return err
	}

	if err := t.backupLocalConfig(".pre-daemon-leave"); err != nil {
		return err
	}

	if err := t.deleteLocalConfig(); err != nil {
		return err
	}

	if err := t.startDaemon(); err != nil {
		return err
	}
	return nil
}

func (t *CmdDaemonLeave) setEvReader() (err error) {
	filters := []string{
		"LeaveSuccess,removed=" + t.localhost + ",node=" + t.ApiNode,
		"LeaveError,leave-node=" + t.localhost,
		"LeaveIgnored,leave-node=" + t.localhost,
	}

	t.evReader, err = t.cli.NewGetEvents().
		SetRelatives(false).
		SetFilters(filters).
		SetDuration(t.Timeout).
		GetReader()
	return
}

func (t *CmdDaemonLeave) waitResult(ctx context.Context) error {
	for {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			ev, err := t.evReader.Read()
			if err != nil {
				return err
			}
			switch ev.Kind {
			case (&msgbus.LeaveSuccess{}).Kind():
				_, _ = fmt.Fprintf(os.Stdout, "Cluster nodes updated\n")
				return nil
			case (&msgbus.LeaveError{}).Kind():
				err := errors.Errorf("leave error: %s", ev.Data)
				return err
			case (&msgbus.LeaveIgnored{}).Kind():
				// TODO parse Reason
				_, _ = fmt.Fprintf(os.Stdout, "Leave ignored: %s", ev.Data)
				return nil
			default:
				return errors.Errorf("unexpected event %s %v", ev.Kind, ev.Data)
			}
		}
	}
}

func (t *CmdDaemonLeave) leave() error {
	_, _ = fmt.Fprintf(os.Stdout, "Daemon leave\n")
	params := api.PostDaemonLeaveParams{
		Node: t.localhost,
	}
	if resp, err := t.cli.PostDaemonLeave(context.Background(), &params); err != nil {
		return errors.Wrapf(err, "Daemon leave error: %s", resp)
	}
	return nil
}

func (t *CmdDaemonLeave) checkParams() error {
	if t.ApiNode == "" {
		ccfg, err := object.NewCluster(object.WithVolatile(true))
		if err != nil {
			return err
		}
		for _, node := range ccfg.ClusterNodes() {
			if node != hostname.Hostname() {
				t.ApiNode = node
				return nil
			}
		}
		return errors.New("unable to find api node to post leave request")
	}
	return nil
}
