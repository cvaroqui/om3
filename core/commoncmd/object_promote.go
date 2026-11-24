package commoncmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/opensvc/om3/v3/core/actionrouter"
	"github.com/opensvc/om3/v3/core/client"
	"github.com/opensvc/om3/v3/core/naming"
	"github.com/opensvc/om3/v3/core/objectselector"
	"github.com/opensvc/om3/v3/core/output"
	"github.com/opensvc/om3/v3/core/rawconfig"
	"github.com/opensvc/om3/v3/daemon/api"
)

type (
	// CmdObjectPromote is the promotion of a replica.
	CmdObjectPromote struct {
		OptsGlobal
		AbortRestore bool
		WaitRestore  string
		DryRun       bool
		Force        bool
		Wait         bool
		Time         time.Duration
	}

	// CmdObjectReplica makes of an object the replica of another.
	CmdObjectReplica struct {
		OptsGlobal
		Of     string
		Source string
	}
)

// waitRestoreEnd is the value of --wait-restore given with no duration:
// wait for the end of the restore.
const waitRestoreEnd = "end"

func NewCmdObjectPromote(kind string) *cobra.Command {
	var options CmdObjectPromote
	cmd := &cobra.Command{
		Use:   "promote",
		Short: "make of a replica the object running, its source lost",
		Long: `Promote a replica: make of it the object running, its source lost.

A replica, created with "deploy --store --snapshot --replica", follows the
snapshots the object of another cluster writes into a store. Its promotion:

1. settles the follow that may be restoring its heads, on each node of a
   replica keeping a copy per node. What is left of the
   restore is weighed on the throughput it had so far: within the
   promote_wait of the sync.store resource, the promotion waits for it,
   that long at most, and the object starts on the snapshot it was
   restoring. Otherwise the restore is ended, and the object starts on the
   snapshot it held before. --abort-restore and --wait-restore take over.
2. records this cluster in the store as the primary of the object: the
   source reads it before each of its snapshots, and writes none from then
   on, back from the disaster or not.
3. takes the replica_of and replica_grants keywords away, so the object is
   one of this cluster, running on the nodes its replica was on.
4. orchestrates its start.

A restore writing a head in place is not ended: it would leave the head
incomplete, and the promotion is refused unless told to wait for its end
with --wait-restore, or to start on the heads as they are with --force.

--dry-run says what the promotion finds and would do, and changes nothing.

Needs the admin grant of the namespace.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	CmdOrchestrated(cmd)
	flags := cmd.Flags()
	AddFlagsGlobal(flags, &options.OptsGlobal)
	flags.BoolVar(&options.AbortRestore, "abort-restore", false, "end the restore in flight, and start on the snapshot the replica held before")
	flags.StringVar(&options.WaitRestore, "wait-restore", "", "wait for the restore in flight, the duration given at most before ending it, or for its end when given none")
	flags.Lookup("wait-restore").NoOptDefVal = waitRestoreEnd
	flags.BoolVar(&options.DryRun, "dry-run", false, "say what the promotion finds and would do, and change nothing")
	flags.BoolVar(&options.Force, "force", false, "promote on heads holding no snapshot restored whole, as they are, and end a restore writing a head in place")
	FlagTime(flags, &options.Time)
	FlagWait(flags, &options.Wait)
	cmd.MarkFlagsMutuallyExclusive("abort-restore", "wait-restore")
	return cmd
}

func (t *CmdObjectPromote) Run(kind string) error {
	if t.WaitRestore != "" && t.WaitRestore != waitRestoreEnd {
		if d, err := time.ParseDuration(t.WaitRestore); err != nil || d <= 0 {
			return fmt.Errorf("--wait-restore %q: want a duration, or no value to wait for the end of the restore", t.WaitRestore)
		}
	}
	// The daemon holds the request for the time it waits for a restore.
	c, err := client.New(client.WithTimeout(0))
	if err != nil {
		return err
	}
	paths, err := objectselector.New(MergeSelector("", t.ObjectSelector, kind, ""), objectselector.WithClient(c)).MustExpand()
	if err != nil {
		return err
	}
	ctx := context.Background()
	promotions := make([]api.Promotion, 0, len(paths))
	var errs error
	for _, p := range paths {
		promotion, err := t.promote(ctx, c, p)
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}
		promotions = append(promotions, promotion)
	}
	if len(promotions) > 0 {
		if err := (output.Renderer{
			Output:   t.Output,
			Color:    t.Color,
			Data:     promotions,
			Colorize: rawconfig.Colorize,
			HumanRenderer: func() string {
				var b strings.Builder
				for _, promotion := range promotions {
					b.WriteString(renderPromotion(promotion))
				}
				return b.String()
			},
		}).Print(); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	for _, promotion := range promotions {
		switch {
		case promotion.DryRun:
		case promotion.OrchestrationRefused != nil:
			errs = errors.Join(errs, fmt.Errorf("%s: promoted, and not started: %s", promotion.Path, *promotion.OrchestrationRefused))
		case t.Wait && promotion.OrchestrationID != nil:
			wctx, cancel := ctx, context.CancelFunc(func() {})
			if t.Time > 0 {
				wctx, cancel = context.WithTimeout(ctx, t.Time)
			}
			node := ""
			if promotion.OrchestrationNode != nil {
				node = *promotion.OrchestrationNode
			}
			if err := actionrouter.WaitOrchestration(wctx, c, *promotion.OrchestrationID, node); err != nil {
				errs = errors.Join(errs, fmt.Errorf("%s: wait for the start of the object promoted: %w", promotion.Path, err))
			}
			cancel()
		}
	}
	return errs
}

func (t *CmdObjectPromote) promote(ctx context.Context, c *client.T, p naming.Path) (api.Promotion, error) {
	params := api.PostObjectActionPromoteParams{}
	if t.AbortRestore {
		params.AbortRestore = &t.AbortRestore
	}
	if t.WaitRestore != "" {
		params.WaitRestore = &t.WaitRestore
	}
	if t.DryRun {
		params.DryRun = &t.DryRun
	}
	if t.Force {
		params.Force = &t.Force
	}
	resp, err := c.PostObjectActionPromoteWithResponse(ctx, p.Namespace, p.Kind, p.Name, &params)
	switch {
	case err != nil:
		return api.Promotion{}, fmt.Errorf("%s: %w", p, err)
	case resp.JSON200 != nil:
		return *resp.JSON200, nil
	case resp.JSON400 != nil:
		return api.Promotion{}, fmt.Errorf("%s: %s", p, *resp.JSON400)
	case resp.JSON401 != nil:
		return api.Promotion{}, fmt.Errorf("%s: %s", p, *resp.JSON401)
	case resp.JSON403 != nil:
		return api.Promotion{}, fmt.Errorf("%s: %s", p, *resp.JSON403)
	case resp.JSON404 != nil:
		return api.Promotion{}, fmt.Errorf("%s: %s", p, *resp.JSON404)
	case resp.JSON408 != nil:
		return api.Promotion{}, fmt.Errorf("%s: %s", p, *resp.JSON408)
	case resp.JSON409 != nil:
		return api.Promotion{}, fmt.Errorf("%s: %s", p, *resp.JSON409)
	case resp.JSON500 != nil:
		return api.Promotion{}, fmt.Errorf("%s: %s", p, *resp.JSON500)
	default:
		return api.Promotion{}, fmt.Errorf("%s: unexpected response: %s", p, resp.Status())
	}
}

// renderPromotion returns what a promotion found, decided and did, a line
// per thing: the snapshot the replica held, dated on the clock of the
// source, the restore in flight, weighed on the clock of the node, what the
// promotion makes of it, and how to have it do otherwise.
func renderPromotion(r api.Promotion) string {
	var b strings.Builder
	line := func(label, format string, args ...any) {
		fmt.Fprintf(&b, "  %-10s %s\n", label, fmt.Sprintf(format, args...))
	}
	deref := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	short := func(id string) string {
		if len(id) > 8 {
			return id[:8]
		}
		return id
	}
	// The other nodes of a replica keeping a copy per node first: each
	// settled its own restore before the primary was recorded.
	if r.Followers != nil {
		for _, follower := range *r.Followers {
			b.WriteString(renderPromotion(follower))
		}
	}
	if r.SkippedNodes != nil && len(*r.SkippedNodes) > 0 {
		fmt.Fprintf(&b, "%s: %s not heard, their restore not settled\n", r.Path, strings.Join(*r.SkippedNodes, " "))
	}
	if r.Epoch == nil && !r.DryRun {
		fmt.Fprintf(&b, "%s: restore settled on %s\n", r.Path, r.Node)
	} else {
		fmt.Fprintf(&b, "%s: promotion on %s\n", r.Path, r.Node)
	}
	if r.Store != nil {
		line("follows", "the object %s of the cluster %s, in store %s", deref(r.SourceObjectID), deref(r.SourceClusterID), *r.Store)
	}
	switch {
	case r.HeldRunID == nil:
		line("committed", "no snapshot of the source restored whole")
	default:
		s := "snapshot " + short(*r.HeldRunID)
		if r.HeldRunCreatedAt != nil {
			s += fmt.Sprintf(", begun at %s on the clock of the source", r.HeldRunCreatedAt.Format(time.RFC3339))
		}
		if r.HeldRunSeenAt != nil {
			s += fmt.Sprintf(", first listed at %s on the clock of %s", r.HeldRunSeenAt.Format(time.RFC3339), r.Node)
		}
		line("committed", "%s", s)
	}
	if restore := r.Restore; restore == nil {
		line("in flight", "no restore")
	} else {
		s := fmt.Sprintf("snapshot %s, %.0f%% of %.1f MiB restored", short(restore.RunID), restore.Percent, float64(restore.BytesTotal)/(1<<20))
		if restore.BytesPerSecond != nil {
			s += fmt.Sprintf(", %.1f MiB/s", *restore.BytesPerSecond/(1<<20))
		}
		if restore.Remaining != nil {
			s += fmt.Sprintf(", about %s left", *restore.Remaining)
		} else {
			s += ", time left unknown"
		}
		s += fmt.Sprintf(", since %s on the clock of %s, pid %d", restore.StartedAt.Format(time.RFC3339), r.Node, restore.Pid)
		if restore.InPlace {
			s += ", writing a head in place"
		}
		line("in flight", "%s", s)
		line("wait", "promote_wait %s", deref(r.PromoteWait))
	}
	action := string(r.Action)
	switch {
	case r.Action == api.PromotionActionWait && r.WaitFor != nil:
		action += " " + *r.WaitFor + " at most"
	case r.Action == api.PromotionActionWait:
		action += " for its end"
	}
	line("action", "%s: %s", action, r.Reason)
	if r.Override != nil {
		line("override", "%s", *r.Override)
	}
	if r.DryRun {
		line("dry run", "nothing changed")
		return b.String()
	}
	if r.Waited != nil {
		line("waited", "%s", *r.Waited)
	}
	if r.Aborted != nil && *r.Aborted {
		line("aborted", "the restore in flight was ended")
	}
	if r.Epoch != nil {
		on := "the heads as they are"
		if r.FromRunID != nil {
			on = "snapshot " + short(*r.FromRunID)
		}
		line("promoted", "this cluster is the primary of the object, epoch %d, on %s", *r.Epoch, on)
	}
	switch {
	case r.OrchestrationID != nil:
		line("start", "orchestration %s on %s", *r.OrchestrationID, deref(r.OrchestrationNode))
	case r.OrchestrationRefused != nil:
		line("start", "not orchestrated: %s", *r.OrchestrationRefused)
	}
	return b.String()
}

func NewCmdObjectReplica(kind string) *cobra.Command {
	var options CmdObjectReplica
	cmd := &cobra.Command{
		Use:   "replica",
		Short: "make of the object the replica of the one another cluster runs",
		Long: `Make of the object the replica of the object another cluster runs.

The way back of a cluster a replica was promoted over: its object, stopped,
is made the replica of the one promoted, and follows its snapshots in the
store --of names until promoted in its turn.

The object takes the configuration of the latest snapshot of its source,
judged as a write of the requester, and keeps what is its own: its id, and
what it recorded of the things it made. The daemon sets its replica_of and
replica_grants keywords, the grants of the requester, which the
configurations it follows from then on are judged under. Its instances stay
on its nodes, which that configuration names as they were.

The source is the object the store records as the primary of this one.
--source names another.

Refused while a resource using the heads of the object is up, and for an
object that is a replica already. Needs the admin grant of the namespace,
here and on the namespace of the object followed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return options.Run(kind)
		},
	}
	flags := cmd.Flags()
	AddFlagsGlobal(flags, &options.OptsGlobal)
	flags.StringVar(&options.Of, "of", "", "the store to follow the source in, as a store keyword names it")
	flags.StringVar(&options.Source, "source", "", "the object to follow, as <cluster id>/<object id>, when it is not the primary the store records for this object")
	_ = cmd.MarkFlagRequired("of")
	return cmd
}

// replicaSource reads the value of --source: the id of a cluster and the
// id of an object, separated by a slash, a comma or spaces.
func replicaSource(s string) (clusterID, objectID string, err error) {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == ',' || r == ' ' })
	if len(fields) != 2 {
		return "", "", fmt.Errorf("--source %q: want <cluster id>/<object id>", s)
	}
	return fields[0], fields[1], nil
}

func (t *CmdObjectReplica) Run(kind string) error {
	params := api.PostObjectActionReplicaParams{Store: &t.Of}
	if t.Source != "" {
		clusterID, objectID, err := replicaSource(t.Source)
		if err != nil {
			return err
		}
		params.SourceClusterId, params.SourceObjectId = &clusterID, &objectID
	}
	c, err := client.New()
	if err != nil {
		return err
	}
	paths, err := objectselector.New(MergeSelector("", t.ObjectSelector, kind, ""), objectselector.WithClient(c)).MustExpand()
	if err != nil {
		return err
	}
	var errs error
	for _, p := range paths {
		resp, err := c.PostObjectActionReplicaWithResponse(context.Background(), p.Namespace, p.Kind, p.Name, &params)
		switch {
		case err != nil:
			errs = errors.Join(errs, fmt.Errorf("%s: %w", p, err))
		case resp.StatusCode() == 204:
			fmt.Printf("%s: a replica, following its source in store %s\n", p, t.Of)
		case resp.JSON400 != nil:
			errs = errors.Join(errs, fmt.Errorf("%s: %s", p, *resp.JSON400))
		case resp.JSON401 != nil:
			errs = errors.Join(errs, fmt.Errorf("%s: %s", p, *resp.JSON401))
		case resp.JSON403 != nil:
			errs = errors.Join(errs, fmt.Errorf("%s: %s", p, *resp.JSON403))
		case resp.JSON404 != nil:
			errs = errors.Join(errs, fmt.Errorf("%s: %s", p, *resp.JSON404))
		case resp.JSON409 != nil:
			errs = errors.Join(errs, fmt.Errorf("%s: %s", p, *resp.JSON409))
		case resp.JSON500 != nil:
			errs = errors.Join(errs, fmt.Errorf("%s: %s", p, *resp.JSON500))
		default:
			errs = errors.Join(errs, fmt.Errorf("%s: unexpected response: %s", p, resp.Status()))
		}
	}
	return errs
}
