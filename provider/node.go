package homelabk3s

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/antimatter-studios/pulumi-homelab/provider/checks"
	"github.com/antimatter-studios/pulumi-homelab/provider/jscompat"
	"github.com/antimatter-studios/pulumi-homelab/provider/mode"
	"github.com/antimatter-studios/pulumi-homelab/provider/shell"
	"github.com/antimatter-studios/pulumi-homelab/provider/systemd"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

const (
	configDir     = "/etc/rancher/k3s"
	configPath    = configDir + "/config.yaml"
	configMode    = "0600"
	unitMode      = "0644"
	defaultBinary = "/usr/local/bin/k3s"
)

type NodeArgs struct {
	DataDir       *string        `pulumi:"dataDir,optional"`
	RequiresMount *string        `pulumi:"requiresMount,optional"`
	KubeletArg    []string       `pulumi:"kubeletArg,optional"`
	Snapshotter   *string        `pulumi:"snapshotter,optional"`
	NodeName      *string        `pulumi:"nodeName,optional"`
	NodeLabel     []string       `pulumi:"nodeLabel,optional"`
	NodeTaint     []string       `pulumi:"nodeTaint,optional"`
	Extra         map[string]any `pulumi:"extra,optional"`
	Binary        *string        `pulumi:"binary,optional"`
	Enabled       *bool          `pulumi:"enabled,optional"`
	Started       *bool          `pulumi:"started,optional"`
}

type K3sServerArgs struct {
	NodeArgs
	Server      *string  `pulumi:"server,optional"`
	Token       *string  `pulumi:"token,optional" provider:"secret"`
	ClusterInit *bool    `pulumi:"clusterInit,optional"`
	TLSSan      []string `pulumi:"tlsSan,optional"`
	Disable     []string `pulumi:"disable,optional"`
}

type K3sAgentArgs struct {
	NodeArgs
	Server *string `pulumi:"server"`
	Token  *string `pulumi:"token" provider:"secret"`
}

type NodeState struct {
	Config     *string `pulumi:"config" provider:"secret"`
	Unit       *string `pulumi:"unit"`
	ConfigMode *string `pulumi:"configMode"`
	UnitMode   *string `pulumi:"unitMode"`
	Enabled    *bool   `pulumi:"enabled"`
	Started    *bool   `pulumi:"started"`
	Written    *bool   `pulumi:"written,optional"`
}

type configPair struct {
	key   string
	value any
}

func quoteJSON(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < ' ' {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func scalar(value any) (string, error) {
	switch v := value.(type) {
	case string:
		return quoteJSON(v), nil
	case *string:
		return quoteJSON(*v), nil
	case bool:
		return fmt.Sprint(v), nil
	case float64:
		return jscompat.FormatNumber(v), nil
	case int:
		return jscompat.FormatNumber(float64(v)), nil
	default:
		return "", fmt.Errorf("expected a string, number, boolean or list of strings, got %T", value)
	}
}

func renderConfig(pairs []configPair) (string, error) {
	var out strings.Builder
	out.WriteString("# Managed by pulumi-homelab-k3s. Edits here are drift and will be overwritten.\n")
	for _, pair := range pairs {
		switch v := pair.value.(type) {
		case nil:
			continue
		case []string:
			writeConfigList(&out, pair.key, v)
			continue
		case []any:
			list := make([]string, 0, len(v))
			for _, item := range v {
				s, ok := item.(string)
				if !ok {
					return "", fmt.Errorf("%s: expected a list of strings, got %T", pair.key, item)
				}
				list = append(list, s)
			}
			writeConfigList(&out, pair.key, list)
			continue
		case *string:
			if v == nil {
				continue
			}
		}
		text, err := scalar(pair.value)
		if err != nil {
			return "", fmt.Errorf("%s: %w", pair.key, err)
		}
		fmt.Fprintf(&out, "%s: %s\n", pair.key, text)
	}
	return out.String(), nil
}

func writeConfigList(out *strings.Builder, key string, list []string) {
	if len(list) > 0 {
		fmt.Fprintf(out, "%s:\n", key)
		for _, item := range list {
			fmt.Fprintf(out, "  - %s\n", quoteJSON(item))
		}
	}
}

func renderUnit(role, binary, mount string) string {
	mountLines := ""
	if mount != "" {
		mountLines = "RequiresMountsFor=" + mount + "\nConditionPathIsMountPoint=" + mount + "\n"
	}
	return fmt.Sprintf(`[Unit]
Description=Lightweight Kubernetes (k3s %s)
Documentation=https://k3s.io
Wants=network-online.target
After=network-online.target
%s
[Service]
Type=notify
EnvironmentFile=-/etc/default/%%N
ExecStartPre=-/sbin/modprobe br_netfilter
ExecStartPre=-/sbin/modprobe overlay
ExecStart=%s %s
KillMode=process
Delegate=yes
LimitNOFILE=1048576
LimitNPROC=infinity
LimitCORE=infinity
TasksMax=infinity
TimeoutStartSec=0
Restart=always
RestartSec=5s

[Install]
WantedBy=multi-user.target
`, role, mountLines, binary, role)
}

func configFor(role string, args K3sServerArgs) (string, error) {
	server := args.Server
	var clusterInit any
	if valueOr(args.ClusterInit, false) {
		if role == "server" && valueOr(server, "") != "" {
			return "", errors.New("a server cannot both start a cluster and join one: set clusterInit on the first server, and server on the others")
		}
		server = nil
		if role == "server" {
			clusterInit = true
		}
	}
	if valueOr(server, "") != "" && valueOr(args.Token, "") == "" {
		return "", fmt.Errorf("this node joins %s and no token was given; set the same token on every node, or read the first server's with NodeToken", *server)
	}
	if role == "agent" && valueOr(server, "") == "" {
		return "", errors.New("an agent has to be told which server to join")
	}
	var tlsSan, disable []string
	if role == "server" {
		tlsSan, disable = args.TLSSan, args.Disable
	}
	pairs := []configPair{
		{"token", args.Token},
		{"cluster-init", clusterInit},
		{"server", server},
		{"tls-san", tlsSan},
		{"disable", disable},
		{"data-dir", args.DataDir},
		{"snapshotter", args.Snapshotter},
		{"node-name", args.NodeName},
		{"node-label", args.NodeLabel},
		{"node-taint", args.NodeTaint},
		{"kubelet-arg", args.KubeletArg},
	}
	keys := jscompat.SortedKeys(args.Extra)
	order := collate.New(language.English)
	slices.SortStableFunc(keys, order.CompareString)
	for _, key := range keys {
		pairs = append(pairs, configPair{key, args.Extra[key]})
	}
	return renderConfig(pairs)
}

func wantedNode(role string, args K3sServerArgs) (NodeState, error) {
	config, err := configFor(role, args)
	if err != nil {
		return NodeState{}, err
	}
	unit := renderUnit(role, valueOr(args.Binary, defaultBinary), valueOr(args.RequiresMount, valueOr(args.DataDir, "")))
	return NodeState{
		Config: &config, Unit: &unit, ConfigMode: ptr(configMode), UnitMode: ptr(unitMode),
		Enabled: ptr(valueOr(args.Enabled, true)), Started: ptr(valueOr(args.Started, true)),
		Written: ptr(true),
	}, nil
}

func unitPath(id string) string { return "/etc/systemd/system/" + id + ".service" }

func heredoc(path, content string) string {
	return "cat > " + shell.Quote(path) + " <<'PULUMI_EOF'\n" + content + "PULUMI_EOF\n"
}

func applyNode(ctx context.Context, t *target, id string, state NodeState, requiresMount *string) error {
	if mount := valueOr(requiresMount, ""); mount != "" {
		mounted, err := t.ask(ctx, true, checks.MountedAt(mount))
		if err != nil {
			return err
		}
		if mounted.code != 0 {
			return fmt.Errorf("%s on %s is not mounted, and k3s's data directory is on it. "+
				"Refusing to write anything: with the disk absent this would create the data directory on "+
				"the root filesystem, and k3s would then start, find it empty, and build a new empty "+
				"cluster on the mount point of the real one. Mount it and deploy again.", mount, t.cfg.Address)
		}
	}
	enable, start := "disable", "stop"
	if *state.Enabled {
		enable = "enable"
	}
	if *state.Started {
		start = "restart"
	}
	file := unitPath(id)
	q := shell.Quote
	_, err := t.must(ctx, true, "mkdir -p "+q(configDir)+" && "+heredoc(configPath, *state.Config)+"\n"+
		"chmod "+*state.ConfigMode+" "+q(configPath)+" && "+heredoc(file, *state.Unit)+"\n"+
		"chmod "+*state.UnitMode+" "+q(file)+" && systemctl daemon-reload && "+
		"systemctl "+enable+" "+q(id)+" && systemctl "+start+" "+q(id))
	return err
}

func readNode(ctx context.Context, t *target, id string) (*NodeState, error) {
	q := shell.Quote
	file := q(unitPath(id))
	result, err := t.ask(ctx, true, "test -f "+file+" || exit 9; "+
		"systemctl show "+q(id)+" --property=UnitFileState,ActiveState | tr '\\n' ' ' && "+
		"stat -c '%a' "+file+" && cat "+file)
	if err != nil || result.code == 9 {
		return nil, err
	}
	if result.code != 0 {
		return nil, fmt.Errorf("could not read unit %s: %s", id, jscompat.Trim(result.stderr))
	}
	first, unit, _ := strings.Cut(result.out, "\n")
	words := jscompat.Fields(first)
	rawMode := ""
	if len(words) != 0 {
		rawMode = words[len(words)-1]
	}
	shown := systemd.ParseShow(strings.Join(words, "\n"))
	state := &NodeState{
		Unit: &unit, UnitMode: ptr(mode.NormaliseMode(rawMode)),
		Enabled: ptr(strings.HasPrefix(shown["UnitFileState"], "enabled")),
		Started: ptr(shown["ActiveState"] == "active" || shown["ActiveState"] == "activating"),
	}
	config, err := t.readFile(ctx, configPath)
	if err != nil {
		return nil, err
	}
	if config == nil {
		config = &remoteFile{}
	}
	state.Config, state.ConfigMode = &config.content, &config.mode
	return state, nil
}

type nodeInputs interface{ K3sServerArgs | K3sAgentArgs }
type nodeResource[I nodeInputs] struct{}

type K3sServer struct{ nodeResource[K3sServerArgs] }
type K3sAgent struct{ nodeResource[K3sAgentArgs] }

func (r *K3sServer) Annotate(a infer.Annotator) {
	a.SetToken("index", "K3sServer")
	a.Describe(r, "A k3s control-plane node and its systemd unit.")
}

func (r *K3sAgent) Annotate(a infer.Annotator) {
	a.SetToken("index", "K3sAgent")
	a.Describe(r, "A k3s worker and its systemd unit.")
}

func nodeArgs[I nodeInputs](inputs I) (role, id string, args K3sServerArgs) {
	switch in := any(inputs).(type) {
	case K3sServerArgs:
		return "server", "k3s", in
	case K3sAgentArgs:
		return "agent", "k3s-agent", K3sServerArgs{NodeArgs: in.NodeArgs, Server: in.Server, Token: in.Token}
	default:
		panic("unexpected node input type")
	}
}

func (nodeResource[I]) WireDependencies(f infer.FieldSelector, _ *I, state *NodeState) {
	f.OutputField(&state.Config).AlwaysSecret()
}

func (nodeResource[I]) Create(ctx context.Context, req infer.CreateRequest[I]) (infer.CreateResponse[NodeState], error) {
	role, id, args := nodeArgs(req.Inputs)
	if req.DryRun {
		return infer.CreateResponse[NodeState]{ID: id, Output: NodeState{
			Config: ptr(""), Unit: ptr(""), ConfigMode: ptr(configMode), UnitMode: ptr(unitMode),
			Enabled: ptr(valueOr(args.Enabled, true)), Started: ptr(valueOr(args.Started, true)), Written: ptr(true),
		}}, nil
	}
	state, err := writeNode(ctx, id, role, args)
	return infer.CreateResponse[NodeState]{ID: id, Output: state}, err
}

func (nodeResource[I]) Read(ctx context.Context, req infer.ReadRequest[I, NodeState]) (infer.ReadResponse[I, NodeState], error) {
	t, err := targetFrom(ctx)
	if err != nil {
		return infer.ReadResponse[I, NodeState]{}, err
	}
	state, err := readNode(ctx, t, req.ID)
	if err != nil || state == nil {
		return infer.ReadResponse[I, NodeState]{}, err
	}
	state.Written = req.State.Written
	return infer.ReadResponse[I, NodeState]{ID: req.ID, Inputs: req.Inputs, State: *state}, nil
}

func writeNode(ctx context.Context, id, role string, args K3sServerArgs) (NodeState, error) {
	state, err := wantedNode(role, args)
	if err != nil {
		return NodeState{}, err
	}
	t, err := targetFrom(ctx)
	if err != nil {
		return NodeState{}, err
	}
	return state, applyNode(ctx, t, id, state, args.RequiresMount)
}

func (nodeResource[I]) Update(ctx context.Context, req infer.UpdateRequest[I, NodeState]) (infer.UpdateResponse[NodeState], error) {
	if req.DryRun {
		return infer.UpdateResponse[NodeState]{Output: req.State}, nil
	}
	role, _, args := nodeArgs(req.Inputs)
	state, err := writeNode(ctx, req.ID, role, args)
	return infer.UpdateResponse[NodeState]{Output: state}, err
}

func (nodeResource[I]) Diff(ctx context.Context, req infer.DiffRequest[I, NodeState]) (p.DiffResponse, error) {
	if raw, ok := infer.GetRawRequest[p.DiffRequest](ctx); ok && raw.Inputs.HasComputed() {
		return p.DiffResponse{HasChanges: true}, nil
	}
	role, _, args := nodeArgs(req.Inputs)
	want, err := wantedNode(role, args)
	if err != nil {
		return p.DiffResponse{}, err
	}
	old := req.State
	changes := differs(old.Config, want.Config) || differs(old.Unit, want.Unit) ||
		differs(old.ConfigMode, want.ConfigMode) || differs(old.UnitMode, want.UnitMode) ||
		differs(old.Enabled, want.Enabled) || differs(old.Started, want.Started)
	return p.DiffResponse{HasChanges: changes}, nil
}

func (nodeResource[I]) Delete(ctx context.Context, req infer.DeleteRequest[NodeState]) (infer.DeleteResponse, error) {
	t, err := targetFrom(ctx)
	if err != nil {
		return infer.DeleteResponse{}, err
	}
	q := shell.Quote
	_, err = t.must(ctx, true, "systemctl disable --now "+q(req.ID)+" || true; "+
		"rm -f "+q(unitPath(req.ID))+" "+q(configPath)+"; systemctl daemon-reload")
	return infer.DeleteResponse{}, err
}
