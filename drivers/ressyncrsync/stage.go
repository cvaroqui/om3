package ressyncrsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/opensvc/om3/v3/core/client"
	"github.com/opensvc/om3/v3/core/resource"
	"github.com/opensvc/om3/v3/daemon/api"
	"github.com/opensvc/om3/v3/util/hostname"
	"github.com/opensvc/om3/v3/util/xsession"
)

// A transfer written into the destination of a peer leaves it, interrupted,
// holding a part of the old tree and a part of the new: a failover during
// the transfer would start the service on it. So the peer is asked for a
// stage of the head holding the destination before the transfer, which is
// written into the stage, and for its commit once the transfer is done: the
// head of the peer is at any time what the last transfer written whole
// left.
//
// The peer holds no lock meanwhile. A start of its instance discards the
// stage, with the rsync receiving into it, and starts on the head as last
// committed: the commit asked for later is told the stage was discarded,
// and the run fails, the transfer being to do again from the node now
// running the service, if any.
//
// A peer whose head stages none is written in place, as before, its head
// marked incomplete from before the first byte until the commit, which a
// start of its applications refuses to run on.

const (
	stageAuto  = "auto"
	stageNever = "never"

	// stageDiscardTimeout bounds the discard of the stage of a transfer
	// failed or cancelled: a sync interrupted for an action to run is
	// given a few seconds to end.
	stageDiscardTimeout = 5 * time.Second

	// stageTimeout bounds the opening and the commit of a stage: the
	// peer waits for the lock of its instance, then switches its head.
	stageTimeout = 5 * time.Minute
)

type (
	// peerStager opens, commits and discards the stage of a transfer
	// named name on a peer.
	peerStager interface {
		// Open opens the stage, one holding the destination as
		// committed only with seededOnly.
		Open(ctx context.Context, name string, seededOnly bool) (resource.SyncStage, error)
		Commit(ctx context.Context, name string) error
		Discard(ctx context.Context, name string) error
	}

	// openedStage is the stage a transfer is written into.
	openedStage struct {
		resource.SyncStage
		stager peerStager
		name   string
	}

	// localStager stages on this node, the target local: the action
	// holds the lock of the object already, and asks the resources.
	localStager struct {
		t *T
	}

	// apiStager stages on the peer nodename, through the api of the
	// daemon, which passes the request on to it.
	apiStager struct {
		t        *T
		c        *client.T
		nodename string
	}
)

// errStageUnreachable is the error of a stage the peer could not be asked
// for: the daemon of this node, or the one of the peer, does not answer.
var errStageUnreachable = errors.New("the daemon is not reachable")

// SyncDestination implements resource.SyncDestination.
func (t *T) SyncDestination() (string, string) {
	return t.Dst, t.DstFS
}

// stageMode returns how the transfers are written on the peers, as the stage
// keyword says.
func (t *T) stageMode() string {
	if t.Stage == stageNever {
		return stageNever
	}
	return stageAuto
}

// peerStager returns what opens the stage of a transfer on nodename.
func (t *T) peerStager(nodename string) (peerStager, error) {
	if t.newPeerStager != nil {
		return t.newPeerStager(nodename)
	}
	if nodename == hostname.Hostname() {
		return localStager{t: t}, nil
	}
	// The peer waits for the lock of its instance before it answers.
	c, err := client.New(client.WithTimeout(stageTimeout))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", errStageUnreachable, err)
	}
	return apiStager{t: t, c: c, nodename: nodename}, nil
}

// openStage asks nodename for the stage of the transfer, and says whether
// it was asked: not with the stage keyword set to never, nor when no daemon
// answers, the transfer being then written in place, as it is with the
// daemons down, unmarked.
func (t *T) openStage(ctx context.Context, nodename string) (openedStage, bool, error) {
	if t.stageMode() == stageNever {
		return openedStage{}, false, nil
	}
	stager, err := t.peerStager(nodename)
	if err == nil {
		name := xsession.SessionID().String()
		var stage resource.SyncStage
		if stage, err = stager.Open(ctx, name, leavesOut(t.fullOptions())); err == nil {
			if stage.Staged {
				t.Log().Infof("%s: sync into the stage %s of %s, committed once written whole", nodename, stage.Dir, stage.HeadRID)
			} else {
				t.Log().Infof("%s: sync in place, no stage of %s: its head is marked incomplete meanwhile", nodename, stage.Dir)
			}
			return openedStage{SyncStage: stage, stager: stager, name: name}, true, nil
		}
	}
	if errors.Is(err, errStageUnreachable) {
		t.Log().Warnf("%s: sync in place, with no stage and no mark: %s", nodename, err)
		return openedStage{}, false, nil
	}
	return openedStage{}, false, fmt.Errorf("stage: %w", err)
}

// stageOptions returns the options of a transfer into stage on nodename.
//
// A stage seeded holds the destination as committed: the options are the
// ones of a transfer in place, --inplace with them, which spares a clone
// the copy of the files it changes.
//
// A stage with a link destination is empty, and hardlinks from the committed
// tree what did not change: an option writing into the files in place would
// write through the links into the committed tree, and is refused.
func stageOptions(options []string, stage resource.SyncStage, nodename string) ([]string, error) {
	if stage.LinkDest == "" {
		return options, nil
	}
	refused := make([]string, 0)
	for _, option := range options {
		name, _, _ := strings.Cut(option, "=")
		if slices.Contains([]string{"--inplace", "--append", "--append-verify"}, name) && !slices.Contains(refused, name) {
			refused = append(refused, name)
		}
	}
	if len(refused) > 0 {
		return nil, fmt.Errorf("the option %s writes into the files of the destination, which the stage of %s on %s hardlinks from the committed tree: remove it from the options, or sync in place with stage=never", strings.Join(refused, " "), stage.HeadRID, nodename)
	}
	return append(slices.Clone(options), "--link-dest="+stage.LinkDest), nil
}

// partialOptions are the rsync options of a transfer leaving a part of the
// destination as it is: the files it filters out, skips, or does not write
// at all.
var partialOptions = []string{
	"--dry-run", "--list-only",
	"--exclude", "--exclude-from", "--include", "--include-from", "--filter", "--files-from", "--cvs-exclude",
	"--existing", "--ignore-non-existing", "--ignore-existing", "--update",
	"--max-size", "--min-size",
}

// leavesOut says the rsync options have the transfer leave a part of the
// destination as it is, which an empty stage switched to would not hold: the
// files a filter keeps out of the transfer, with -C, -f and -F too, the ones
// it skips, as -u does, and all of them with -n.
func leavesOut(options []string) bool {
	for i, o := range options {
		if i > 0 && (options[i-1] == "-e" || options[i-1] == "--rsh") {
			// The remote shell command, not an option.
			continue
		}
		switch {
		case strings.HasPrefix(o, "--"):
			name, _, _ := strings.Cut(o, "=")
			if slices.Contains(partialOptions, name) {
				return true
			}
		case strings.HasPrefix(o, "-") && strings.ContainsAny(o[1:], "CFfnu"):
			return true
		}
	}
	return false
}

func (t localStager) Open(ctx context.Context, name string, seededOnly bool) (resource.SyncStage, error) {
	return resource.OpenSyncStage(ctx, t.t.GetObjectDriver(), t.t, name, seededOnly)
}

func (t localStager) Commit(ctx context.Context, name string) error {
	return resource.CommitSyncStage(ctx, t.t.GetObjectDriver(), t.t, name)
}

func (t localStager) Discard(ctx context.Context, name string) error {
	return resource.DiscardSyncStage(ctx, t.t.GetObjectDriver(), t.t, name)
}

func (t apiStager) Open(ctx context.Context, name string, seededOnly bool) (resource.SyncStage, error) {
	var stage resource.SyncStage
	p := t.t.Path
	params := api.PostInstanceResourceHeadStageParams{Name: name}
	if seededOnly {
		params.Seeded = &seededOnly
	}
	response, err := t.c.PostInstanceResourceHeadStage(ctx, t.nodename, p.Namespace, p.Kind, p.Name, t.t.RID(), &params)
	if err != nil {
		// A daemon not listening was asked nothing: one that did not
		// answer in time may have opened the stage.
		var opErr *net.OpError
		if errors.As(err, &opErr) && opErr.Op == "dial" {
			return stage, fmt.Errorf("%w: %s", errStageUnreachable, err)
		}
		return stage, err
	}
	defer drainClose(response.Body)
	if response.StatusCode != http.StatusOK {
		return stage, problemOf(response)
	}
	var answer api.InstanceHeadStage
	if err := json.NewDecoder(response.Body).Decode(&answer); err != nil {
		return stage, err
	}
	return resource.SyncStage{
		Staged:   answer.Staged,
		Dir:      answer.Dir,
		Seeded:   answer.Seeded,
		LinkDest: answer.LinkDest,
		HeadRID:  answer.HeadRID,
	}, nil
}

func (t apiStager) Commit(ctx context.Context, name string) error {
	p := t.t.Path
	params := api.PostInstanceResourceHeadCommitParams{Name: name}
	response, err := t.c.PostInstanceResourceHeadCommit(ctx, t.nodename, p.Namespace, p.Kind, p.Name, t.t.RID(), &params)
	if err != nil {
		return err
	}
	defer drainClose(response.Body)
	if response.StatusCode != http.StatusNoContent {
		return problemOf(response)
	}
	return nil
}

func (t apiStager) Discard(ctx context.Context, name string) error {
	p := t.t.Path
	params := api.PostInstanceResourceHeadDiscardParams{Name: name}
	response, err := t.c.PostInstanceResourceHeadDiscard(ctx, t.nodename, p.Namespace, p.Kind, p.Name, t.t.RID(), &params)
	if err != nil {
		return err
	}
	defer drainClose(response.Body)
	if response.StatusCode != http.StatusNoContent {
		return problemOf(response)
	}
	return nil
}

// problemOf returns the error an answer of the api tells: the one of a
// stage discarded, of a peer the daemon could not pass the request on to,
// or what the peer said.
func problemOf(response *http.Response) error {
	var problem api.Problem
	b, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err := json.Unmarshal(b, &problem); err != nil || problem.Title == "" {
		return fmt.Errorf("unexpected response: %s", response.Status)
	}
	switch {
	case response.StatusCode == http.StatusConflict:
		return fmt.Errorf("%w: %s", resource.ErrHeadStageDiscarded, problem.Detail)
	case problem.Title == "Request peer" || problem.Title == "New client":
		return fmt.Errorf("%w: %s", errStageUnreachable, problem.Detail)
	default:
		return fmt.Errorf("%s: %s", problem.Title, problem.Detail)
	}
}

// drainClose reads what is left of a response body before closing it, so the
// connection is reused for the next request.
func drainClose(rc io.ReadCloser) {
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
}
