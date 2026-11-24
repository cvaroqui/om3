package daemonapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/opensvc/om3/v3/core/client"
	"github.com/opensvc/om3/v3/core/env"
	"github.com/opensvc/om3/v3/core/instance"
	"github.com/opensvc/om3/v3/core/naming"
	"github.com/opensvc/om3/v3/core/object"
	"github.com/opensvc/om3/v3/core/topology"
	"github.com/opensvc/om3/v3/daemon/api"
	"github.com/opensvc/om3/v3/daemon/discovered"
	"github.com/opensvc/om3/v3/daemon/msgbus"
	"github.com/opensvc/om3/v3/daemon/session"
	"github.com/opensvc/om3/v3/util/command"
	"github.com/opensvc/om3/v3/util/pubsub"
	"github.com/opensvc/om3/v3/util/xsession"
)

// promotedConfigTimeout is how long a promotion waits for the instance
// monitor of the node to see the object is no longer a replica, before it
// asks it to start it.
const promotedConfigTimeout = 15 * time.Second

// PostObjectActionPromote promotes a replica: the object takes over from
// the one it followed, its source lost.
//
// An administrator of the namespace asks it, and it is the one request
// that has the replica_of and replica_grants keywords unset for one: root
// alone writes them, because unset they let the daemon start the object
// beside its source. The promotion is what says the source is lost, and it
// takes them away only once its cluster is recorded in the store as the
// primary of the object, the record the source reads to stop writing: an
// administrator can not turn a replica into a running object without the
// fence going up first, nor by any other request.
//
// The node serving it is the placement leader of the replica, the node its
// follow runs on and the one its start begins with.
func (a *DaemonAPI) PostObjectActionPromote(ctx echo.Context, namespace string, kind naming.Kind, name string, params api.PostObjectActionPromoteParams) error {
	if v, err := assertAdmin(ctx, namespace); !v {
		return err
	}
	p, err := naming.NewPath(namespace, kind, name)
	if err != nil {
		return JSONProblemf(ctx, http.StatusBadRequest, "Invalid parameters", "%s", err)
	}
	switch nodename := promoteNode(a.localhost, instance.MonitorData.GetByPath(p), hearsPeer); {
	case nodename == a.localhost:
		return a.postLocalObjectActionPromote(ctx, p, params)
	case nodename != "":
		// The peer holds the request for the time it waits for a
		// restore.
		return a.proxyWaiting(ctx, nodename, params.WaitRestore, func(c *client.T) (*http.Response, error) {
			return c.PostObjectActionPromote(ctx.Request().Context(), namespace, kind, name, &params)
		})
	case !discovered.Settled():
		return JSONProblemExpand(ctx, ErrDiscovering)
	}
	return JSONProblem(ctx, http.StatusNotFound, "object not found", "")
}

// promoteNode returns the node promoting the replica whose instances have
// the monitors: its placement leader when a node says it is and this node
// hears it, this node when it has an instance, and else the node an
// orchestration is handed to. It returns "" for an object with no instance.
func promoteNode(localhost string, monitors map[string]*instance.Monitor, hears func(localhost, nodename string) bool) string {
	if m, ok := monitors[localhost]; ok && m != nil && m.IsLeader {
		return localhost
	}
	nodes := make([]string, 0, len(monitors))
	for nodename := range monitors {
		nodes = append(nodes, nodename)
	}
	slices.Sort(nodes)
	for _, nodename := range nodes {
		if nodename != localhost && monitors[nodename] != nil && monitors[nodename].IsLeader && hears(localhost, nodename) {
			return nodename
		}
	}
	if _, ok := monitors[localhost]; ok {
		return localhost
	}
	return actionNode(localhost, monitors, hears)
}

// promoteOptions returns the options of the promotion the request asks.
func promoteOptions(params api.PostObjectActionPromoteParams) (object.PromoteOptions, error) {
	var opts object.PromoteOptions
	opts.AbortRestore = params.AbortRestore != nil && *params.AbortRestore
	opts.DryRun = params.DryRun != nil && *params.DryRun
	opts.Force = params.Force != nil && *params.Force
	if params.WaitRestore != nil {
		if err := opts.SetWaitRestore(*params.WaitRestore); err != nil {
			return opts, err
		}
	}
	if opts.AbortRestore && opts.WaitRestore {
		return opts, fmt.Errorf("abort_restore and wait_restore say opposite things")
	}
	return opts, nil
}

// promoteRefusal is a promotion an instance refused: a state of the replica
// to fix or to force, answered as a conflict.
type promoteRefusal struct {
	detail string
}

func (t promoteRefusal) Error() string {
	return t.detail
}

// execPromote runs the promotion of the instance of p on this node, as opts
// say, and returns what it found and did.
//
// It runs as an om process, as every action the daemon has a node do: the
// promotion takes the lock of the object to know the follow done with its
// heads, and the locks of a file are the ones of a process, which loses
// them when it closes any handle on the file. The daemon opens that file
// for other requests of the object; the command opens it once.
//
// The process ends with the request: a requester gone while it waits for a
// restore leaves the replica as it was.
func (a *DaemonAPI) execPromote(ctx echo.Context, p naming.Path, opts object.PromoteOptions) (object.PromoteReport, error) {
	var result object.PromoteResult
	execname, err := os.Executable()
	if err != nil {
		return result.Report, fmt.Errorf("can't detect om execname: %w", err)
	}
	args := []string{p.String(), "instance", "promote"}
	if opts.AbortRestore {
		args = append(args, "--abort-restore")
	}
	if v := opts.WaitRestoreValue(); v != "" {
		args = append(args, "--wait-restore="+v)
	}
	if opts.DryRun {
		args = append(args, "--dry-run")
	}
	if opts.Force {
		args = append(args, "--force")
	}
	if opts.SettleOnly {
		args = append(args, "--settle-only")
	}
	log := naming.LogWithPath(LogHandler(ctx, "PostObjectActionPromote"), p)
	sessionID := xsession.NewSessionID(uuid.Nil)
	cmd := command.New(
		command.WithName(execname),
		command.WithArgs(args),
		command.WithLogger(log),
		command.WithContext(ctx.Request().Context()),
		command.WithBufferedStdout(),
		command.WithVarEnv(
			env.ActionOriginDaemonAPI.Var(),
			sessionID.Var(),
			"OSVC_REQUEST_ID="+fmt.Sprint(ctx.Get("uuid")),
		),
	)
	log.Infof("-> exec %s", cmd)
	if err := cmd.Run(); err != nil {
		if cerr := ctx.Request().Context().Err(); cerr != nil {
			return result.Report, cerr
		}
		return result.Report, fmt.Errorf("%s: %w", cmd, err)
	}
	if result, err = parsePromoteResult(cmd.Stdout()); err != nil {
		return result.Report, fmt.Errorf("%s: %w", cmd, err)
	}
	switch {
	case result.Error == "":
		return result.Report, nil
	case result.Refused:
		return result.Report, promoteRefusal{detail: result.Error}
	default:
		return result.Report, errors.New(result.Error)
	}
}

// parsePromoteResult reads what the promotion of an instance printed: one
// json document, the last line of its output.
func parsePromoteResult(b []byte) (object.PromoteResult, error) {
	var result object.PromoteResult
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if last == "" {
		return result, fmt.Errorf("the promotion of the instance said nothing")
	}
	if err := json.Unmarshal([]byte(last), &result); err != nil {
		return result, fmt.Errorf("the promotion of the instance said %q: %w", last, err)
	}
	return result, nil
}

// promoteProblem answers the error of a promotion.
func promoteProblem(ctx echo.Context, err error) error {
	var refusal promoteRefusal
	switch {
	case errors.As(err, &refusal):
		return JSONProblemf(ctx, http.StatusConflict, "Promote", "%s", err)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return JSONProblemf(ctx, http.StatusRequestTimeout, "Promote", "%s", err)
	default:
		return JSONProblemf(ctx, http.StatusInternalServerError, "Promote", "%s", err)
	}
}

// PostInstanceActionPromote settles the restore a follow of the replica is
// running on a node, for the promotion of the object: it records nothing
// and changes no configuration, so the keywords only root writes stay where
// they are. The promotion of the object asks it of each node keeping a copy
// of its own, as the requester, an administrator of the namespace.
func (a *DaemonAPI) PostInstanceActionPromote(ctx echo.Context, nodename, namespace string, kind naming.Kind, name string, params api.PostInstanceActionPromoteParams) error {
	if v, err := assertAdmin(ctx, namespace); !v {
		return err
	}
	nodename = a.parseNodename(nodename)
	if a.localhost != nodename {
		return a.proxyWaiting(ctx, nodename, params.WaitRestore, func(c *client.T) (*http.Response, error) {
			return c.PostInstanceActionPromote(ctx.Request().Context(), nodename, namespace, kind, name, &params)
		})
	}
	p, err := naming.NewPath(namespace, kind, name)
	if err != nil {
		return JSONProblemf(ctx, http.StatusBadRequest, "Invalid parameters", "%s", err)
	}
	opts, err := promoteOptions(api.PostObjectActionPromoteParams(params))
	if err != nil {
		return JSONProblemf(ctx, http.StatusBadRequest, "Invalid parameters", "%s", err)
	}
	opts.SettleOnly = true
	report, err := a.execPromote(ctx, p, opts)
	if err != nil {
		return promoteProblem(ctx, err)
	}
	return ctx.JSON(http.StatusOK, promotionAnswer(report))
}

// followerNodes returns the nodes of the replica whose instances have the
// monitors, this node aside, that a promotion has settle their own follow
// before it records the primary, and the ones it can not ask, not heard:
// a node down holds no restore in flight, and a disaster is no time to wait
// for one. Both are sorted.
//
// Only a replica keeping a copy per node has them, a flex one: the nodes of
// a failover replica hold one copy, which its placement leader follows.
func followerNodes(localhost string, flex bool, monitors map[string]*instance.Monitor, hears func(localhost, nodename string) bool) (asked, skipped []string) {
	if !flex {
		return nil, nil
	}
	for nodename := range monitors {
		switch {
		case nodename == localhost:
		case hears(localhost, nodename):
			asked = append(asked, nodename)
		default:
			skipped = append(skipped, nodename)
		}
	}
	slices.Sort(asked)
	slices.Sort(skipped)
	return asked, skipped
}

// settleFollowers has each of nodes settle its follow, all at once, and
// returns what each did, in the order of nodes. The first refusal, or else
// the first failure, is returned once all answered, naming its node: the
// primary is not recorded while a node may still be writing its heads.
func settleFollowers(nodes []string, settle func(nodename string) (api.Promotion, error)) ([]api.Promotion, error) {
	answers := make([]api.Promotion, len(nodes))
	errs := make([]error, len(nodes))
	var wg sync.WaitGroup
	for i, nodename := range nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			answers[i], errs[i] = settle(nodename)
		}()
	}
	wg.Wait()
	var failure error
	for i, err := range errs {
		var refusal promoteRefusal
		switch {
		case err == nil:
		case errors.As(err, &refusal):
			return answers, promoteRefusal{detail: fmt.Sprintf("node %s: %s", nodes[i], refusal.detail)}
		case failure == nil:
			failure = fmt.Errorf("node %s: %w", nodes[i], err)
		}
	}
	return answers, failure
}

// settleOn asks the node nodename to settle its follow of p, as the
// requester.
func (a *DaemonAPI) settleOn(ctx echo.Context, p naming.Path, nodename string, params api.PostObjectActionPromoteParams) (api.Promotion, error) {
	c, err := a.newProxyClient(ctx, nodename, client.WithTimeout(0))
	if err != nil {
		return api.Promotion{}, err
	}
	instanceParams := api.PostInstanceActionPromoteParams(params)
	resp, err := c.PostInstanceActionPromoteWithResponse(ctx.Request().Context(), nodename, p.Namespace, p.Kind, p.Name, &instanceParams)
	switch {
	case err != nil:
		return api.Promotion{}, err
	case resp.JSON200 != nil:
		return *resp.JSON200, nil
	case resp.JSON409 != nil:
		return api.Promotion{}, promoteRefusal{detail: resp.JSON409.Detail}
	case resp.JSON400 != nil:
		return api.Promotion{}, errors.New(resp.JSON400.Detail)
	case resp.JSON403 != nil:
		return api.Promotion{}, errors.New(resp.JSON403.Detail)
	case resp.JSON408 != nil:
		return api.Promotion{}, fmt.Errorf("%s: %w", resp.JSON408.Detail, context.DeadlineExceeded)
	case resp.JSON500 != nil:
		return api.Promotion{}, errors.New(resp.JSON500.Detail)
	default:
		return api.Promotion{}, fmt.Errorf("unexpected response: %s", resp.Status())
	}
}

func (a *DaemonAPI) postLocalObjectActionPromote(ctx echo.Context, p naming.Path, params api.PostObjectActionPromoteParams) error {
	log := naming.LogWithPath(LogHandler(ctx, "PostObjectActionPromote"), p)
	opts, err := promoteOptions(params)
	if err != nil {
		return JSONProblemf(ctx, http.StatusBadRequest, "Invalid parameters", "%s", err)
	}
	// Every node keeping a copy of its own settles its follow first:
	// the primary is recorded once no node writes its heads.
	flex := false
	if cfg := instance.ConfigData.GetByPathAndNode(p, a.localhost); cfg != nil && cfg.ActorConfig != nil {
		flex = cfg.ActorConfig.Topology == topology.Flex
	}
	asked, skipped := followerNodes(a.localhost, flex, instance.MonitorData.GetByPath(p), hearsPeer)
	followers, err := settleFollowers(asked, func(nodename string) (api.Promotion, error) {
		return a.settleOn(ctx, p, nodename, params)
	})
	if err != nil {
		return promoteProblem(ctx, err)
	}
	if len(skipped) > 0 {
		log.Warnf("%s: promote without settling the follow of %s, not heard", p, strings.Join(skipped, " "))
	}
	report, err := a.execPromote(ctx, p, opts)
	if err != nil {
		return promoteProblem(ctx, err)
	}
	answer := promotionAnswer(report)
	if len(followers) > 0 {
		answer.Followers = &followers
	}
	if len(skipped) > 0 {
		answer.SkippedNodes = &skipped
	}
	if opts.DryRun {
		return ctx.JSON(http.StatusOK, answer)
	}
	if report.Epoch == 0 {
		// The keywords only root writes are taken away for a primary
		// recorded, and for nothing else.
		return JSONProblemf(ctx, http.StatusInternalServerError, "Promote", "the promotion of the instance of %s recorded no primary: its configuration is left a replica's", p)
	}
	// The cluster is recorded as the primary from here: the source writes
	// no run any more, and this replica follows none. What is left makes
	// of it an object of this cluster, and a promotion failing from here
	// is finished by asking it again.
	if err := a.commitPromotedConfig(p); err != nil {
		return JSONProblemf(ctx, http.StatusInternalServerError, "Promote", "the cluster is recorded as the primary of %s, and its configuration is a replica's still: %s: ask the promotion again to finish it", p, err)
	}
	log.Infof("%s: promoted by %s, epoch %d, on run %s: no longer a replica", p, userFromContext(ctx).Username, report.Epoch, report.FromRunID)
	a.Bus.Pub(&msgbus.ObjectReplicaPromoted{Path: p, Node: a.localhost, Epoch: report.Epoch, FromRunID: report.FromRunID},
		pubsub.Label{"namespace", p.Namespace}, pubsub.Label{"path", p.String()}, a.LabelLocalhost, labelOriginAPI)
	id, queued, err := a.startPromoted(ctx.Request().Context(), p)
	if errors.Is(err, errPromotedStarted) {
		log.Infof("%s: promoted, and started already: no start to orchestrate", p)
		return ctx.JSON(http.StatusOK, answer)
	}
	if err != nil {
		log.Warnf("%s: promoted, and its start is not orchestrated: %s", p, err)
		refused := err.Error()
		answer.OrchestrationRefused = &refused
		return ctx.JSON(http.StatusOK, answer)
	}
	if queued {
		log.Infof("%s: the instance monitor did not read the start of the promoted object in time: answer it queued", p)
	}
	answer.OrchestrationID = &id
	answer.OrchestrationNode = &a.localhost
	return ctx.JSON(http.StatusOK, answer)
}

// commitPromotedConfig writes the configuration of the replica p as its
// promotion leaves it. It is written by the daemon, for the requester the
// promotion was let through for: no keyword policy judges it, the keywords
// taken away being the ones only root writes.
func (a *DaemonAPI) commitPromotedConfig(p naming.Path) error {
	var err error
	for range 3 {
		if err = a.commitPromotedConfigOnce(p); !errors.Is(err, ErrConfigChanged) {
			return err
		}
	}
	return err
}

func (a *DaemonAPI) commitPromotedConfigOnce(p naming.Path) error {
	base, err := readConfigBase(p)
	if err != nil {
		return err
	}
	current, err := os.ReadFile(p.ConfigFile())
	if err != nil {
		return err
	}
	body, err := object.PromotedConfig(p, current)
	if err != nil {
		return err
	}
	o, err := object.New(p, object.WithConfigData(body))
	if err != nil {
		return err
	}
	cfg := o.(object.Configurer).Config()
	if err := base.commit(cfg, cfg.RecommitInvalid); err != nil {
		return err
	}
	a.announceConfigFileWritten(p)
	return nil
}

// errPromotedStarted says the object promoted has nothing to start: an
// operator started the replica before promoting it, as forced.
var errPromotedStarted = errors.New("already started")

// startPromoted asks the instance monitor of this node to orchestrate the
// start of the object p, just promoted, and returns the id of the
// orchestration, and whether the monitor took it on yet.
//
// The monitor refuses to start a replica, and learns the object is no
// longer one from the instance configuration the write of its
// configuration ends up publishing: the start is asked once that is
// published, and asked again when the monitor, not there yet, refuses it as
// the start of a replica.
func (a *DaemonAPI) startPromoted(ctx context.Context, p naming.Path) (uuid.UUID, bool, error) {
	deadline := time.Now().Add(promotedConfigTimeout)
	for time.Now().Before(deadline) {
		if cfg := instance.ConfigData.GetByPathAndNode(p, a.localhost); cfg != nil && cfg.ActorConfig != nil && !cfg.ActorConfig.DRP {
			break
		}
		select {
		case <-ctx.Done():
			return uuid.Nil, false, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	for {
		globalExpect := instance.MonitorGlobalExpectStarted
		value := instance.MonitorUpdate{
			GlobalExpect:             &globalExpect,
			CandidateOrchestrationID: uuid.New(),
		}
		setCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		msg, setImonErr := msgbus.NewSetInstanceMonitorWithErr(setCtx, p, a.localhost, value)
		a.Bus.Pub(msg, pubsub.Label{"namespace", p.Namespace}, pubsub.Label{"path", p.String()}, labelOriginAPI)
		err := setImonErr.Receive()
		cancel()
		record := func() {
			session.AddOrchestrationIfUnknown(session.Orchestration{
				OrchestrationID: value.CandidateOrchestrationID.String(),
				Node:            a.localhost,
				Path:            p.String(),
				Expect:          globalExpect.String(),
			})
		}
		switch {
		case err == nil:
			record()
			return value.CandidateOrchestrationID, false, nil
		case ctx.Err() != nil:
			return uuid.Nil, false, ctx.Err()
		case errors.Is(err, context.DeadlineExceeded):
			// Published, and read later by a monitor busy: a
			// timeout is no refusal.
			record()
			return value.CandidateOrchestrationID, true, nil
		case strings.Contains(err.Error(), errPromotedStarted.Error()):
			return uuid.Nil, false, errPromotedStarted
		case strings.Contains(err.Error(), "is a replica") && time.Now().Before(deadline):
			select {
			case <-ctx.Done():
				return uuid.Nil, false, ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		default:
			return uuid.Nil, false, err
		}
	}
}

// promotionAnswer returns the report of a promotion as the api answers it.
func promotionAnswer(r object.PromoteReport) api.Promotion {
	str := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	tm := func(t time.Time) *time.Time {
		if t.IsZero() {
			return nil
		}
		return &t
	}
	dur := func(d time.Duration) *string {
		if d <= 0 {
			return nil
		}
		s := d.Round(time.Millisecond).String()
		return &s
	}
	answer := api.Promotion{
		Path:             r.Path,
		Node:             r.Node,
		Store:            str(r.Store),
		SourceClusterID:  str(r.SourceClusterID),
		SourceObjectID:   str(r.SourceObjectID),
		HeldRunID:        str(r.HeldRunID),
		HeldRunCreatedAt: tm(r.HeldRunCreatedAt),
		HeldRunSeenAt:    tm(r.HeldRunSeenAt),
		Action:           api.PromotionAction(r.Action),
		WaitFor:          dur(r.WaitFor),
		Reason:           r.Reason,
		Override:         str(r.Override),
		DryRun:           r.DryRun,
		Waited:           dur(r.Waited),
		FromRunID:        str(r.FromRunID),
	}
	promoteWait := r.PromoteWait.String()
	answer.PromoteWait = &promoteWait
	if r.Aborted {
		answer.Aborted = &r.Aborted
	}
	if r.Epoch > 0 {
		answer.Epoch = &r.Epoch
	}
	if r.Restore != nil {
		restore := api.PromotionRestore{
			RunID:      r.Restore.RunID,
			Pid:        r.Restore.PID,
			StartedAt:  r.Restore.StartedAt,
			BytesTotal: r.Restore.BytesTotal,
			BytesDone:  r.Restore.BytesDone,
			Percent:    r.Restore.Percent(),
			InPlace:    r.Restore.InPlace,
		}
		if r.Restore.RemainingKnown {
			restore.BytesPerSecond = &r.Restore.BytesPerSecond
			remaining := r.Restore.Remaining.Round(time.Second).String()
			restore.Remaining = &remaining
		}
		answer.Restore = &restore
	}
	return answer
}
