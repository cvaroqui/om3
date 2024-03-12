package ressync

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/opensvc/om3/core/client"
	"github.com/opensvc/om3/core/driver"
	"github.com/opensvc/om3/core/keywords"
	"github.com/opensvc/om3/core/naming"
	"github.com/opensvc/om3/core/resource"
	"github.com/opensvc/om3/core/status"
	"github.com/opensvc/om3/core/statusbus"
	"github.com/opensvc/om3/util/converters"
	"github.com/opensvc/om3/util/file"
	"github.com/opensvc/om3/util/hostname"
	"github.com/opensvc/om3/util/schedule"
)

type (
	T struct {
		resource.T
		MaxDelay *time.Duration `json:"max_delay"`
		Schedule string         `json:"schedule"`
		Path     naming.Path    `json:"path"`
	}
)

var (
	//go:embed text
	fs embed.FS

	KWMaxDelay = keywords.Keyword{
		Option:        "max_delay",
		DefaultOption: "sync_max_delay",
		Aliases:       []string{"sync_max_delay"},
		Attr:          "MaxDelay",
		Converter:     converters.Duration,
		Text:          keywords.NewText(fs, "text/kw/max_delay"),
	}
	KWSchedule = keywords.Keyword{
		Option:        "schedule",
		DefaultOption: "sync_schedule",
		Attr:          "Schedule",
		Scopable:      true,
		Example:       "00:00-01:00 mon",
		Text:          keywords.NewText(fs, "text/kw/schedule"),
	}

	BaseKeywords = append(
		[]keywords.Keyword{},
		KWMaxDelay,
		KWSchedule,
	)
)

// GetMaxDelay return the configured max_delay if set.
// If not set, return the duration from now to the end of the
// next schedule period.
func (t *T) GetMaxDelay(lastSync time.Time) *time.Duration {
	if t.MaxDelay != nil {
		return t.MaxDelay
	}
	sched := schedule.New(t.Schedule)
	begin, duration, err := sched.Next(schedule.NextWithLast(lastSync))
	if err != nil {
		return nil
	}
	end := begin.Add(duration)
	maxDelay := end.Sub(time.Now())
	if maxDelay < 0 {
		maxDelay = 0
	}
	return &maxDelay
}

func (t *T) StatusLastSync(nodenames []string) status.T {
	state := status.NotApplicable

	if len(nodenames) == 0 {
		t.StatusLog().Info("no target nodes")
		return status.NotApplicable
	}

	for _, nodename := range nodenames {
		if tm, err := t.readLastSync(nodename); err != nil {
			t.StatusLog().Error("%s last sync: %s", nodename, err)
		} else if tm.IsZero() {
			t.StatusLog().Warn("%s never synced", nodename)
		} else {
			maxDelay := t.GetMaxDelay(tm)
			if maxDelay == nil || *maxDelay == 0 {
				t.StatusLog().Info("no schedule and no max delay")
				continue
			}
			elapsed := time.Now().Sub(tm)
			if elapsed > *maxDelay {
				t.StatusLog().Warn("%s last sync at %s (>%s after last)", nodename, tm, maxDelay)
				state.Add(status.Warn)
			} else {
				//t.StatusLog().Info("%s last sync at %s (%s after last)", nodename, tm, maxDelay)
				state.Add(status.Up)
			}
		}
	}
	return state
}

func (t T) WritePeerLastSync(peer string, peers []string) error {
	head := t.GetObjectDriver().VarDir()
	lastSyncFile := t.lastSyncFile(peer)
	lastSyncFileSrc := t.lastSyncFile(hostname.Hostname())
	schedTimestampFile := filepath.Join(head, "scheduler", "last_sync_update_"+t.RID())
	now := time.Now()
	if err := file.Touch(lastSyncFile, now); err != nil {
		return err
	}
	if err := file.Touch(lastSyncFileSrc, now); err != nil {
		return err
	}
	if err := file.Touch(schedTimestampFile, now); err != nil {
		return err
	}

	c, err := client.New(client.WithURL(peer))
	if err != nil {
		return err
	}

	send := func(filename, nodename string) error {
		file, err := os.Open(filename)
		if err != nil {
			return err
		}
		defer file.Close()
		ctx := context.Background()
		response, err := c.PostInstanceStateFileWithBody(ctx, nodename, t.Path.Namespace, t.Path.Kind, t.Path.Name, "application/octet-stream", file, func(ctx context.Context, req *http.Request) error {
			req.Header.Add("x-relative-path", filename[len(head):])
			return nil
		})
		if err != nil {
			return err
		}
		if response.StatusCode != http.StatusNoContent {
			return fmt.Errorf("unexpected response: %s", response)
		}
		return nil
	}

	var errs error

	for _, nodename := range peers {
		for _, filename := range []string{lastSyncFile, lastSyncFileSrc, schedTimestampFile} {
			if err := send(filename, nodename); err != nil {
				errs = errors.Join(errs, fmt.Errorf("failed to send state file %s to node %s: %w", filename, nodename, err))
			}
			t.Log().Infof("state file %s sent to node %s", filename, nodename)
		}
	}

	return errs
}

func (t T) WriteLastSync(nodename string) error {
	p := t.lastSyncFile(nodename)
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	return nil
}

func (t T) readLastSync(nodename string) (time.Time, error) {
	var tm time.Time
	p := t.lastSyncFile(nodename)
	info, err := os.Stat(p)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return tm, nil
	case err != nil:
		return tm, err
	default:
		return info.ModTime(), nil
	}
}

func (t T) lastSyncFile(nodename string) string {
	return filepath.Join(t.VarDir(), "last_sync_"+nodename)
}

func (t *T) GetTargetPeernames(target, nodes, drpNodes []string) []string {
	nodenames := make([]string, 0)
	localhost := hostname.Hostname()
	for _, nodename := range t.GetTargetNodenames(target, nodes, drpNodes) {
		if nodename != localhost {
			nodenames = append(nodenames, nodename)
		}
	}
	return nodenames
}

func (t *T) GetTargetNodenames(target, nodes, drpNodes []string) []string {
	nodenames := make([]string, 0)
	targetMap := make(map[string]any)
	for _, target := range target {
		targetMap[target] = nil
	}
	if _, ok := targetMap["nodes"]; ok {
		nodenames = append(nodenames, nodes...)
	}
	if _, ok := targetMap["drpnodes"]; ok {
		nodenames = append(nodenames, drpNodes...)
	}
	return nodenames
}

func (t *T) IsInstanceSufficientlyStarted(ctx context.Context) (v bool, rids []string) {
	sb := statusbus.FromContext(ctx)
	o := t.GetObjectDriver()
	l := o.ResourcesByDrivergroups([]driver.Group{
		driver.GroupIP,
		driver.GroupFS,
		driver.GroupShare,
		driver.GroupDisk,
		driver.GroupContainer,
	})
	v = true
	for _, r := range l {
		switch r.ID().DriverGroup() {
		case driver.GroupIP:
		case driver.GroupFS:
		case driver.GroupShare:
		case driver.GroupDisk:
			switch r.Manifest().DriverID.Name {
			case "drbd":
				continue
			case "scsireserv":
				continue
			}
		case driver.GroupContainer:
		default:
			continue
		}
		st := sb.Get(r.RID())
		switch st {
		case status.Up:
		case status.StandbyUp:
		case status.NotApplicable:
		default:
			// required resource is not up
			rids = append(rids, r.RID())
			v = false
		}
	}
	return
}
