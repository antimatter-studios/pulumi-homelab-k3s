package homelabk3s

import (
	"context"
	"fmt"
	"strings"

	"github.com/antimatter-studios/pulumi-homelab/provider/jscompat"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

const kubeconfigPath = "/etc/rancher/k3s/k3s.yaml"

type Kubeconfig struct{}

type KubeconfigArgs struct {
	Server       *string  `pulumi:"server"`
	ReadySeconds *float64 `pulumi:"readySeconds,optional"`
}

type KubeconfigState struct {
	Server       *string  `pulumi:"server,optional"`
	ReadySeconds *float64 `pulumi:"readySeconds,optional"`
	Config       *string  `pulumi:"config" provider:"secret"`
}

func (r *Kubeconfig) Annotate(a infer.Annotator) {
	a.SetToken("index", "Kubeconfig")
	a.Describe(r, "The cluster administrator kubeconfig, with the API endpoint repointed.")
}

func repoint(config, server string) string {
	const loopback = "https://127.0.0.1:6443"
	if !strings.Contains(server, "$") {
		return strings.ReplaceAll(config, loopback, "https://"+server+":6443")
	}

	// JavaScript String.replace expands these sequences in a string replacement.
	parts := strings.Split(config, loopback)
	var out strings.Builder
	for i, part := range parts {
		out.WriteString(part)
		if i == len(parts)-1 {
			break
		}
		out.WriteString("https://")
		for j := 0; j < len(server); j++ {
			if server[j] == '$' && j+1 < len(server) {
				switch server[j+1] {
				case '$':
					out.WriteByte('$')
				case '&':
					out.WriteString(loopback)
				case '\x60':
					out.WriteString(strings.Join(parts[:i+1], loopback))
				case '\'':
					out.WriteString(strings.Join(parts[i+1:], loopback))
				default:
					out.WriteByte(server[j])
					continue
				}
				j++
			} else {
				out.WriteByte(server[j])
			}
		}
		out.WriteString(":6443")
	}
	return out.String()
}

func readKubeconfig(ctx context.Context, t *target, server string) (*string, error) {
	file, err := t.readFile(ctx, kubeconfigPath)
	if err != nil || file == nil {
		return nil, err
	}
	return ptr(repoint(file.content, server)), nil
}

func collectKubeconfig(ctx context.Context, t *target, args KubeconfigArgs) (KubeconfigState, error) {
	seconds := valueOr(args.ReadySeconds, defaultReadySeconds)
	timeout := jscompat.FormatNumber(seconds) + "s"
	waited, err := t.ask(ctx, true, waitForFile(kubeconfigPath, seconds)+
		" || exit 9; k3s kubectl wait --for=condition=Ready node --all --timeout="+timeout)
	if err != nil {
		return KubeconfigState{}, err
	}
	if waited.code == 9 {
		return KubeconfigState{}, fmt.Errorf("k3s never wrote %s within %s; check `systemctl status k3s` on %s", kubeconfigPath, timeout, t.cfg.Address)
	}
	if waited.code != 0 {
		detail := waited.stderr
		if detail == "" {
			detail = waited.out
		}
		return KubeconfigState{}, fmt.Errorf("the k3s node did not become ready within %s: %s", timeout, jscompat.Trim(detail))
	}
	config, err := readKubeconfig(ctx, t, valueOr(args.Server, ""))
	if err != nil {
		return KubeconfigState{}, err
	}
	if config == nil {
		return KubeconfigState{}, fmt.Errorf("%s vanished between waiting for it and reading it", kubeconfigPath)
	}
	return KubeconfigState{Server: args.Server, ReadySeconds: &seconds, Config: config}, nil
}

func (Kubeconfig) WireDependencies(f infer.FieldSelector, _ *KubeconfigArgs, state *KubeconfigState) {
	f.OutputField(&state.Config).AlwaysSecret()
}

func (Kubeconfig) Create(ctx context.Context, req infer.CreateRequest[KubeconfigArgs]) (infer.CreateResponse[KubeconfigState], error) {
	if req.DryRun {
		return infer.CreateResponse[KubeconfigState]{Output: KubeconfigState{Server: req.Inputs.Server, ReadySeconds: req.Inputs.ReadySeconds, Config: ptr("")}}, nil
	}
	t, err := targetFrom(ctx)
	if err != nil {
		return infer.CreateResponse[KubeconfigState]{}, err
	}
	state, err := collectKubeconfig(ctx, t, req.Inputs)
	return infer.CreateResponse[KubeconfigState]{ID: t.cfg.Address + ":kubeconfig", Output: state}, err
}

func (Kubeconfig) Read(ctx context.Context, req infer.ReadRequest[KubeconfigArgs, KubeconfigState]) (infer.ReadResponse[KubeconfigArgs, KubeconfigState], error) {
	t, err := targetFrom(ctx)
	if err != nil {
		return infer.ReadResponse[KubeconfigArgs, KubeconfigState]{}, err
	}
	// The import ID can supply the declared API address independently of the SSH route.
	// Return the canonical ID so later refreshes use the endpoint saved in state.
	id := req.ID
	state, inputs := req.State, req.Inputs
	if host, api, ok := strings.Cut(id, ":kubeconfig:"); ok {
		if api == "" {
			return infer.ReadResponse[KubeconfigArgs, KubeconfigState]{}, fmt.Errorf("kubeconfig import ID %q is missing the API address", req.ID)
		}
		id, state.Server = host+":kubeconfig", &api
	}
	if state.Server == nil {
		state.Server = ptr(valueOr(inputs.Server, t.cfg.Address))
	}
	config, err := readKubeconfig(ctx, t, *state.Server)
	if err != nil || config == nil {
		return infer.ReadResponse[KubeconfigArgs, KubeconfigState]{}, err
	}
	if inputs.Server == nil || id != req.ID {
		inputs.Server = state.Server
	}
	state.Config = config
	return infer.ReadResponse[KubeconfigArgs, KubeconfigState]{ID: id, Inputs: inputs, State: state}, nil
}

func (Kubeconfig) Update(ctx context.Context, req infer.UpdateRequest[KubeconfigArgs, KubeconfigState]) (infer.UpdateResponse[KubeconfigState], error) {
	if req.DryRun {
		return infer.UpdateResponse[KubeconfigState]{Output: req.State}, nil
	}
	t, err := targetFrom(ctx)
	if err != nil {
		return infer.UpdateResponse[KubeconfigState]{}, err
	}
	state, err := collectKubeconfig(ctx, t, req.Inputs)
	return infer.UpdateResponse[KubeconfigState]{Output: state}, err
}

func (Kubeconfig) Diff(_ context.Context, req infer.DiffRequest[KubeconfigArgs, KubeconfigState]) (p.DiffResponse, error) {
	changed := differs(req.State.Server, req.Inputs.Server) ||
		differs(req.State.ReadySeconds, ptr(valueOr(req.Inputs.ReadySeconds, defaultReadySeconds)))
	return p.DiffResponse{HasChanges: changed}, nil
}

func (Kubeconfig) Delete(context.Context, infer.DeleteRequest[KubeconfigState]) (infer.DeleteResponse, error) {
	return infer.DeleteResponse{}, nil
}
