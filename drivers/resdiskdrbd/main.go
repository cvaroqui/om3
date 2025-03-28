//go:build linux

package resdiskdrbd

import (
	"context"
	// Necessary to use go:embed
	_ "embed"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"

	"github.com/opensvc/om3/core/actionrollback"
	"github.com/opensvc/om3/core/client"
	"github.com/opensvc/om3/core/naming"
	"github.com/opensvc/om3/core/network"
	"github.com/opensvc/om3/core/object"
	"github.com/opensvc/om3/core/provisioned"
	"github.com/opensvc/om3/core/resource"
	"github.com/opensvc/om3/core/status"
	"github.com/opensvc/om3/daemon/api"
	"github.com/opensvc/om3/drivers/resdisk"
	"github.com/opensvc/om3/util/capabilities"
	"github.com/opensvc/om3/util/device"
	"github.com/opensvc/om3/util/drbd"
	"github.com/opensvc/om3/util/file"
	"github.com/opensvc/om3/util/hostname"
	"github.com/opensvc/om3/util/key"
	"github.com/opensvc/om3/util/waitfor"
)

type (
	T struct {
		resdisk.T
		Path     naming.Path `json:"path"`
		Nodes    []string    `json:"nodes"`
		Res      string      `json:"res"`
		Disk     string      `json:"disk"`
		MaxPeers int         `json:"max_peers"`
		Addr     string      `json:"addr"`
		Port     int         `json:"port"`
		Network  string      `json:"network"`
	}
	DRBDDriver interface {
		Adjust() error
		Attach() error
		Connect() error
		ConnState() (string, error)
		CreateMD(int) error
		DetachForce() error
		Disconnect() error
		DiskStates() ([]string, error)
		Down() error
		HasMD() (bool, error)
		IsDefined() (bool, error)
		Primary() error
		PrimaryForce() error
		Role() (string, error)
		Secondary() error
		Up() error
		WipeMD() error
	}
	ConfRes struct {
		Name  string
		Hosts []ConfResOn
	}
	ConfResOn struct {
		Name   string
		Addr   string
		Device string
		Disk   string
		NodeId int
	}
)

var (
	WaitKnownDiskStatesDelay   = time.Second * 1
	WaitKnownDiskStatesTimeout = time.Second * 5

	MaxNodes = 32

	//go:embed text/template/res9
	resTemplateTextV9 string

	//go:embed text/template/res8
	resTemplateTextV8 string
)

func New() resource.Driver {
	t := &T{}
	return t
}

func (t *T) Name() string {
	if t.Path.Namespace != "root" {
		return fmt.Sprintf(
			"%s.%s.%s",
			strings.ToLower(t.Path.Namespace),
			strings.Split(t.Path.Name, ".")[0],
			strings.ReplaceAll(t.RID(), "#", "."),
		)
	} else {
		return fmt.Sprintf(
			"%s.%s",
			strings.Split(t.Path.Name, ".")[0],
			strings.ReplaceAll(t.RID(), "#", "."),
		)
	}
}

func (t *T) Info(ctx context.Context) (resource.InfoKeys, error) {
	m := resource.InfoKeys{
		{Key: "res", Value: t.Res},
	}
	return m, nil
}

func (t *T) WaitKnownDiskStates(dev DRBDDriver) error {
	check := func() (bool, error) {
		states, err := dev.DiskStates()
		if err != nil {
			return false, err
		}
		for _, state := range states {
			if state == "Diskless/DUnknown" {
				return false, nil
			}
		}
		return true, nil
	}
	limit := time.Now().Add(WaitKnownDiskStatesTimeout)
	for {
		ok, err := check()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().Add(WaitKnownDiskStatesDelay).After(limit) {
			return fmt.Errorf("timeout waiting for peers to have a known dstate")
		}
		time.Sleep(WaitKnownDiskStatesDelay)
	}
}

// DownForce is called by the unprovisioner. Dataloss is not an issue there,
// so forced detach can be tried.
func (t *T) DownForce(ctx context.Context) error {
	dev := t.drbd()
	if err := dev.Disconnect(); err != nil {
		return err
	}
	if err := dev.DetachForce(); err != nil {
		return err
	}
	if err := dev.Down(); err != nil {
		return err
	}
	return nil
}

func (t *T) Down(ctx context.Context) error {
	dev := t.drbd()
	if err := dev.Down(); err != nil {
		return err
	}
	// flush devtree caches
	return nil
}

func (t *T) Up(ctx context.Context) error {
	dev := t.drbd()
	if err := dev.Up(); err != nil {
		return err
	}
	if err := t.WaitKnownDiskStates(dev); err != nil {
		return err
	}
	// flush devtree caches
	return nil
}

func (t *T) GoSecondary(ctx context.Context) error {
	dev := t.drbd()
	role, err := dev.Role()
	if err != nil {
		return err
	}
	if role == "Secondary" {
		return nil
	}
	if err := dev.Secondary(); err != nil {
		return err
	}
	actionrollback.Register(ctx, func(ctx context.Context) error {
		return dev.Primary()
	})
	return nil
}

func (t *T) isConfigured() bool {
	cf := drbd.ResConfigFile(t.Res)
	return file.Exists(cf)
}

func (t *T) StopStandby(ctx context.Context) error {
	if !t.isConfigured() {
		t.Log().Infof("skip: resource not configured")
		return nil
	}
	dev := t.drbd()
	if ok, err := dev.IsDefined(); err != nil {
		return err
	} else if !ok {
		t.Log().Infof("skip: resource not defined (for this host)")
		return nil
	}
	if err := t.StartConnection(ctx); err != nil {
		return fmt.Errorf("start connection: %s", err)
	}
	return t.GoSecondary(ctx)
}

func (t *T) StartStandby(ctx context.Context) error {
	dev := t.drbd()
	if err := t.StartConnection(ctx); err != nil {
		return fmt.Errorf("start connection: %s", err)
	}
	role, err := dev.Role()
	if err != nil {
		return err
	}
	if role == "Primary" {
		return nil
	}
	return dev.Secondary()
}

func (t *T) Start(ctx context.Context) error {
	if !t.isConfigured() {
		t.Log().Infof("skip: resource not configured")
		return nil
	}
	dev := t.drbd()
	if err := t.StartConnection(ctx); err != nil {
		return fmt.Errorf("start connection: %s", err)
	}
	role, err := dev.Role()
	if err != nil {
		return err
	}
	if role == "Primary" {
		return nil
	}
	if err := dev.Primary(); err != nil {
		return err
	}
	actionrollback.Register(ctx, func(ctx context.Context) error {
		return dev.Secondary()
	})
	return nil
}

func (t *T) Stop(ctx context.Context) error {
	if !t.isConfigured() {
		t.Log().Infof("skip: resource not configured")
		return nil
	}
	dev := t.drbd()
	if ok, err := dev.IsDefined(); err != nil {
		return err
	} else if !ok {
		t.Log().Infof("skip: resource not defined (for this host)")
		return nil
	}
	return t.Down(ctx)
}

func (t *T) Shutdown(ctx context.Context) error {
	if !t.isConfigured() {
		t.Log().Infof("skip: resource not configured")
		return nil
	}
	dev := t.drbd()
	if ok, err := dev.IsDefined(); err != nil {
		return err
	} else if !ok {
		t.Log().Infof("skip: resource not defined (for this host)")
		return nil
	}
	return t.DownForce(ctx)
}

// StartConnection ensures cstate is Connecting or Connected.
//
// on cstate StandAlone: try Down then Up
//
// transient ctates: https://github.com/LINBIT/drbd-headers/blob/master/linux/drbd.h
//
//	example: Unconnected, Timeout, ... wait for Connecting or Connected reached
//	example: Disconnecting: wait for StandAlone reached before try Down, Up
func (t *T) StartConnection(ctx context.Context) error {
	dev := t.drbd()
	state, err := dev.ConnState()
	if err != nil {
		return err
	}

	doWait := func(candidates ...string) (bool, error) {
		t.Log().Infof("wait %s for cstate in (%s)", t.Res, strings.Join(candidates, ","))
		var state string
		ok, err := waitfor.TrueNoErrorCtx(ctx, 5*time.Second, time.Second, func() (bool, error) {
			var err error
			if state, err = dev.ConnState(); err != nil {
				return false, err
			} else if slices.Contains(candidates, state) {
				return true, nil
			} else {
				return false, nil
			}
		})
		if err != nil {
			t.Log().Warnf("wait for %s cstate in (%s): %s",
				t.Res, strings.Join(candidates, ","), err)
		} else if !ok {
			t.Log().Warnf("wait for %s cstate in (%s): timeout, last state was: %s",
				t.Res, strings.Join(candidates, ","), state)
		}
		return ok, err
	}

	doWaitConnected := func() error {
		if ok, err := doWait("Connecting", "Connected"); err != nil {
			return fmt.Errorf("wait for cstate in (Connecting, Connected): %s", err)
		} else if !ok {
			return fmt.Errorf("wait for cstate in (Connecting, Connected): timeout")
		} else {
			return nil
		}
	}

	restartConnection := func(doDown bool) error {
		t.Log().Infof("drbd %s restart connection", t.Res)
		if doDown {
			_ = t.Down(ctx)
		}
		if err := t.Up(ctx); err != nil {
			return fmt.Errorf("drbd %s restart connection: Up: %s", t.Res, err)
		}
		return doWaitConnected()
	}

	t.Log().Infof("drbd resource %s cstate %s", t.Res, state)
	switch state {
	case "Connecting", "Connected":
		// expected state is reached
		return nil
	case "Unconnected", "Timeout", "BrokenPipe", "NetworkFailure", "ProtocolError", "TearDown":
		return doWaitConnected()
	case "Disconnecting":
		t.Log().Infof("drbd resource %s: wants cstate StandAlone before restart connection", t.Res)
		if ok, err := doWait("StandAlone"); err != nil {
			return fmt.Errorf("drbd resource %s: waiting for cstate StandAlone: %s", t.Res, err)
		} else if !ok {
			return fmt.Errorf("drbd resource %s: waiting for cstate StandAlone: timeout", t.Res)
		} else {
			t.Log().Infof("drbd resource %s: cstate StandAlone: restart connection", t.Res)
			return restartConnection(true)
		}
	case "StandAlone":
		return restartConnection(true)
	case "WFConnection":
		t.Log().Warnf("drbd resource %s peer node is not listening", t.Res)
		return nil
	default:
		// TODO: prefer instead restartConnection(true) ?
		return restartConnection(false)
	}
}

func (t *T) removeHolders() error {
	for _, dev := range t.ExposedDevices() {
		if err := dev.RemoveHolders(); err != nil {
			return nil
		}
	}
	return nil
}

func (t *T) Status(ctx context.Context) status.T {
	dev := t.drbd()
	isDefined, err := dev.IsDefined()
	if err != nil {
		t.StatusLog().Error("defined: %s", err)
		return status.Undef
	}
	if !isDefined {
		return status.Down
	}
	role, err := dev.Role()
	if err != nil {
		t.StatusLog().Error("role: %s", err)
		return status.Undef
	}
	t.StatusLog().Info(role)

	states, err := dev.DiskStates()
	if err != nil {
		t.StatusLog().Error("dstates: %s", err)
		return status.Undef
	}
	resourceStatus := status.Undef
	for i, state := range states {
		if i == 0 {
			switch state {
			case "Diskless", "DUnknown", "Unconfigured":
				resourceStatus = status.Down
			}
		} else {
			switch state {
			case "UpToDate":
			default:
				t.StatusLog().Warn("unexpected drbd resource %s/%d state: %s", t.Res, i, state)
			}
		}
	}
	if resourceStatus != status.Undef {
		return resourceStatus
	}

	switch role {
	case "Primary":
		return status.Up
	case "Secondary":
		return status.StandbyUp
	default:
		t.StatusLog().Warn("unexpected drbd resource %s role: %s", t.Res, role)
		return status.Warn
	}
}

// Label implements Label from resource.Driver interface,
// it returns a formatted short description of the Resource
func (t *T) Label(_ context.Context) string {
	return t.Res
}

// UnprovisionStop is a noop to avoid calling the normal Stop before unprovision
func (t *T) UnprovisionStop(ctx context.Context) error {
	return nil
}

// ProvisionStart is a noop to avoid calling the normal Start after provision
func (t *T) ProvisionStart(ctx context.Context) error {
	return nil
}

func (t *T) getDRBDAllocations() (map[string]api.DRBDAllocation, error) {
	allocations := make(map[string]api.DRBDAllocation)
	c, err := client.New()
	if err != nil {
		return nil, err
	}
	for _, nodename := range t.Nodes {
		resp, err := c.GetNodeDRBDAllocationWithResponse(context.Background(), nodename)
		switch {
		case err != nil:
			return nil, err
		case resp.StatusCode() == 500:
			return nil, fmt.Errorf("get node %s drbd allocations: %s", nodename, resp.JSON500)
		case resp.StatusCode() == 200:
			allocations[nodename] = *resp.JSON200
		default:
			return nil, fmt.Errorf("get node %s drbd allocations: unexpected status code %d", nodename, resp.StatusCode())
		}
	}
	return allocations, nil
}

func (t *T) formatConfig(wr io.Writer, res ConfRes) error {
	var text string
	if capabilities.Has("disk.drbd.mesh") {
		text = resTemplateTextV9
	} else {
		text = resTemplateTextV8
	}
	templ, err := template.New("res").Parse(text)
	if err != nil {
		return err
	}
	return templ.Execute(wr, res)
}

func (t *T) makeConfRes(allocations map[string]api.DRBDAllocation) (ConfRes, error) {
	res := ConfRes{
		Name:  t.Res,
		Hosts: make([]ConfResOn, 0),
	}
	obj := t.GetObject().(object.Configurer)
	for nodeID, nodename := range t.Nodes {
		var (
			disk, addr, ipVer string
			port              int
		)
		allocation, ok := allocations[nodename]
		if !ok {
			return ConfRes{}, fmt.Errorf("drbd allocation for node %s not found", nodename)
		}
		if time.Now().After(allocation.ExpiredAt) {
			return ConfRes{}, fmt.Errorf("drbd allocation for node %s has expired", nodename)
		}
		device := fmt.Sprintf("/dev/drbd%d", allocation.Minor)
		if s, err := obj.Config().EvalAs(key.T{Section: t.RID(), Option: "disk"}, nodename); err != nil {
			return res, err
		} else {
			disk = s.(string)
		}

		if s, err := obj.Config().EvalAs(key.T{Section: t.RID(), Option: "addr"}, nodename); (err != nil) || (s == "") {
			if ip, err := t.getNodeIP(nodename); err != nil {
				return res, err
			} else {
				addr = ip.String()
			}
		} else {
			addr = s.(string)
		}

		if i, err := obj.Config().EvalAs(key.T{Section: t.RID(), Option: "port"}, nodename); err != nil {
			// EvalAs will error because the port kw has no default
			port = allocation.Port
		} else {
			// TODO: remove to not let the user bug ?
			port = i.(int)
		}

		// ip stringer should set the brackets around ipv6
		ip := net.ParseIP(addr)
		if ip.To4() == nil {
			ipVer = "ipv6"
		} else {
			ipVer = "ipv4"
		}

		host := ConfResOn{
			Name:   nodename,
			Addr:   fmt.Sprintf("%s %s:%d", ipVer, ip, port),
			Disk:   disk,
			Device: device,
			NodeId: nodeID,
		}
		res.Hosts = append(res.Hosts, host)
	}
	return res, nil
}

func (t *T) getNodeIP(nodename string) (net.IP, error) {
	if t.Network != "" {
		return t.getNodeIPWithNetwork(nodename)
	} else {
		return t.getNodeIPWithGetAddrInfo(nodename)
	}
}

func (t *T) getNodeIPWithNetwork(nodename string) (net.IP, error) {
	node, err := object.NewNode(object.WithVolatile(true))
	if err != nil {
		return nil, err
	}
	nws := network.Networks(node)
	for _, nw := range nws {
		if nw.Name() != t.Network {
			continue
		}
		if ip, err := nw.NodeSubnetIP(nodename); err != nil {
			return nil, err
		} else {
			return ip, nil
		}
	}
	return nil, fmt.Errorf("node %s ip not found on network %s", nodename, t.Network)
}

func (t *T) getNodeIPWithGetAddrInfo(nodename string) (net.IP, error) {
	ips, err := net.LookupIP(nodename)
	if err != nil {
		return nil, err
	}
	n := len(ips)
	switch n {
	case 0:
		return nil, fmt.Errorf("name %s is unresolvable", nodename)
	case 1:
		// ok
	default:
		t.Log().Debugf("name %s is resolvables to %d address. Using the first.", nodename, n)
	}
	return ips[0], nil

}

// TODO: Acquire/Release cluster lock
func (t *T) lock(ctx context.Context) error {
	//lockName := "drivers.resources.disk.drbd.allocate"
	return nil
}

// TODO: Acquire/Release cluster lock
func (t *T) unlock(ctx context.Context) error {
	return nil
}

func (t *T) fetchConfigFromNode(nodename string) ([]byte, error) {
	c, err := client.New()
	if err != nil {
		return nil, err
	}
	params := api.GetNodeDRBDConfigParams{
		Name: t.Res,
	}
	resp, err := c.GetNodeDRBDConfigWithResponse(context.Background(), nodename, &params)
	if err != nil {
		return nil, err
	} else if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("unexpected get node drbd config status code %s", resp.Status())
	}
	return resp.JSON200.Data, nil
}

func (t *T) fetchConfig() error {
	cf := drbd.ResConfigFile(t.Res)
	if file.Exists(cf) {
		t.Log().Infof("%s already exists", cf)
		return nil
	}
	for _, nodename := range t.Nodes {
		b, err := t.fetchConfigFromNode(nodename)
		if err != nil {
			continue
		}
		err = os.WriteFile(cf, b, os.ModePerm)
		if err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("failed to fetch %s, tried node %s", cf, t.Nodes)
}

func (t *T) writeConfig(ctx context.Context) error {
	cf := drbd.ResConfigFile(t.Res)
	if file.Exists(cf) {
		t.Log().Infof("%s already exists", cf)
		return nil
	}
	if err := t.lock(ctx); err != nil {
		return err
	}
	defer func() {
		_ = t.unlock(ctx)
	}()
	allocations, err := t.getDRBDAllocations()
	if err != nil {
		return err
	}
	res, err := t.makeConfRes(allocations)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(cf, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.ModePerm)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := t.formatConfig(file, res); err != nil {
		return err
	}
	b, err := os.ReadFile(cf)
	if err != nil {
		return err
	}
	if err := t.sendConfig(b, allocations); err != nil {
		return err
	}
	return nil
}

func (t *T) sendConfig(b []byte, allocations map[string]api.DRBDAllocation) error {
	for _, nodename := range t.Nodes {
		var allocationID uuid.UUID
		if nodename == hostname.Hostname() {
			continue
		}
		if a, ok := allocations[nodename]; ok {
			allocationID = a.ID
		} else {
			return fmt.Errorf("allocation id for node %s not found", nodename)
		}
		if err := t.sendConfigToNode(nodename, allocationID, b); err != nil {
			return err
		}
	}
	return nil
}

func (t *T) sendConfigToNode(nodename string, allocationID uuid.UUID, b []byte) error {
	c, err := client.New()
	if err != nil {
		return err
	}
	params := api.PostNodeDRBDConfigParams{
		Name: t.Res,
	}
	body := api.PostNodeDRBDConfigRequest{
		AllocationID: allocationID,
		Data:         b,
	}
	resp, err := c.PostNodeDRBDConfigWithResponse(context.Background(), nodename, &params, body)
	if err != nil {
		return err
	}
	switch resp.StatusCode() {
	case 200:
		return nil
	case 400:
		return fmt.Errorf("%s", resp.JSON400)
	case 401:
		return fmt.Errorf("%s", resp.JSON401)
	case 403:
		return fmt.Errorf("%s", resp.JSON403)
	case 500:
		return fmt.Errorf("%s", resp.JSON500)
	default:
		return fmt.Errorf("unexpected status code: %s", resp.Status())
	}
}

func (t *T) ProvisionAsFollower(ctx context.Context) error {
	actionrollback.Register(ctx, func(ctx context.Context) error {
		return t.UnprovisionAsFollower(ctx)
	})
	if err := t.fetchConfig(); err != nil {
		return err
	}
	if err := t.provisionCommon(ctx); err != nil {
		return err
	}
	if err := t.drbd().Disconnect(); err != nil {
		return err
	}
	if err := t.drbd().Connect(); err != nil {
		return err
	}
	return nil
}

func (t *T) ProvisionAsLeader(ctx context.Context) error {
	actionrollback.Register(ctx, func(ctx context.Context) error {
		return t.UnprovisionAsLeader(ctx)
	})
	if err := t.writeConfig(ctx); err != nil {
		return err
	}
	if err := t.provisionCommon(ctx); err != nil {
		return err
	}
	if err := t.drbd().PrimaryForce(); err != nil {
		return err
	}
	return nil
}

func (t *T) provisionCommon(ctx context.Context) error {
	if err := t.CreateMD(); err != nil {
		return err
	}
	if err := t.Down(ctx); err != nil {
		return err
	}
	if err := t.Up(ctx); err != nil {
		return err
	}
	return nil
}

func (t *T) WipeMD() error {
	if v, err := t.drbd().HasMD(); err != nil {
		return err
	} else if !v {
		t.Log().Infof("resource %s already has no metadata", t.Res)
		return nil
	}
	return t.drbd().WipeMD()
}

func (t *T) maxPeers() int {
	v := t.MaxPeers
	nNodes := len(t.Nodes)

	// minValue could be nNodes-1 but we want to add a slot to allow a server
	// swap.
	minValue := nNodes
	if minValue < 1 {
		minValue = 1
	}
	maxValue := MaxNodes - 1
	if v == 0 {
		v = (nNodes * 2) - 1
	}
	if v < minValue {
		v = minValue
	}
	if v > maxValue {
		v = maxValue
	}
	return v
}

func (t *T) CreateMD() error {
	if v, err := t.drbd().HasMD(); err != nil {
		return err
	} else if v {
		t.Log().Infof("resource %s already has metadata", t.Res)
		return nil
	}
	return t.drbd().CreateMD(t.maxPeers())
}

func (t *T) deleteConfig() error {
	cf := drbd.ResConfigFile(t.Res)
	err := os.Remove(cf)
	if os.IsNotExist(err) {
		t.Log().Infof("%s already deleted", cf)
		return nil
	} else if err != nil {
		return err
	} else {
		t.Log().Infof("deleted %s", cf)
		return nil
	}
}

func (t *T) UnprovisionAsLeader(ctx context.Context) error {
	return t.unprovisionCommon(ctx)
}

func (t *T) UnprovisionAsFollower(ctx context.Context) error {
	return t.unprovisionCommon(ctx)
}

func (t *T) unprovisionCommon(ctx context.Context) error {
	isDefined, err := t.drbd().IsDefined()
	if err != nil {
		return err
	}
	if isDefined {
		if err := t.DownForce(ctx); err != nil {
			return err
		}
		if err := t.WipeMD(); err != nil {
			return err
		}
	} else {
		t.Log().Infof("resource already not defined")
	}
	if err := t.deleteConfig(); err != nil {
		return err
	}
	return nil
}

func (t *T) Provisioned() (provisioned.T, error) {
	if !t.isConfigured() {
		return provisioned.False, nil
	}
	hasMD, err := t.drbd().HasMD()
	if err != nil {
		t.Log().Debugf("drbd res is not configured")
		return provisioned.Undef, err
	}
	if !hasMD {
		t.Log().Debugf("drbd disk has no metadata")
		return provisioned.False, nil
	}
	return provisioned.True, nil
}

func (t *T) ExposedDevices() device.L {
	l := make(device.L, 0)
	dump, err := drbd.GetConfig()
	if err != nil {
		return l
	}
	resource, ok := dump.GetResource(t.Res)
	if !ok {
		return l
	}
	host, ok := resource.GetHost(hostname.Hostname())
	if !ok {
		return l
	}
	for _, volume := range host.Volumes {
		l = append(l, device.New(volume.Device.Path))
	}
	return l
}

func (t *T) SubDevices() device.L {
	l := make(device.L, 0)
	dump, err := drbd.GetConfig()
	if err != nil {
		return l
	}
	resource, ok := dump.GetResource(t.Res)
	if !ok {
		return l
	}
	host, ok := resource.GetHost(hostname.Hostname())
	if !ok {
		return l
	}
	for _, volume := range host.Volumes {
		l = append(l, device.New(volume.Disk))
	}
	return l
}

func (t *T) ReservableDevices() device.L {
	return t.SubDevices()
}

func (t *T) ClaimedDevices() device.L {
	return t.SubDevices()
}

/*
func (t Path) Boot(ctx context.Context) error {
	return t.Stop(ctx)
}

func (t Path) PostSync() error {
	return nil
}

func (t Path) PreSync() error {
	return t.dumpCacheFile()
}

func (t Path) ToSync() []string {
	return []string{}
}

func (t Path) Resync(ctx context.Context) error {
	return t.drbd().Resync()
}
*/
