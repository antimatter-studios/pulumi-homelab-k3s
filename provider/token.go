package homelabk3s

import (
	"context"
	"fmt"

	"github.com/antimatter-studios/pulumi-homelab/provider/jscompat"
	"github.com/antimatter-studios/pulumi-homelab/provider/shell"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"gopkg.in/yaml.v3"
)

const defaultDataDir = "/var/lib/rancher/k3s"
const defaultReadySeconds = 300

type NodeToken struct{}

type NodeTokenArgs struct {
	ReadySeconds *float64 `pulumi:"readySeconds,optional"`
	DataDir      *string  `pulumi:"dataDir,optional"`
}

type NodeTokenState struct {
	NodeTokenArgs
	Token *string `pulumi:"token" provider:"secret"`
}

func (r *NodeToken) Annotate(a infer.Annotator) {
	a.SetToken("index", "NodeToken")
	a.Describe(r, "The join token read from a k3s server.")
}

func nodeTokenPath(dataDir *string) string {
	return valueOr(dataDir, defaultDataDir) + "/server/node-token"
}

func waitForFile(path string, seconds float64) string {
	q := shell.Quote(path)
	return "for _ in $(seq 1 " + jscompat.FormatNumber(seconds) + "); do test -f " + q +
		" && break; sleep 1; done; test -f " + q
}

func readNodeToken(ctx context.Context, t *target, path string) (*string, error) {
	q := shell.Quote(path)
	result, err := t.ask(ctx, true, "test -f "+q+" || exit 9; cat "+q)
	if err != nil || result.code == 9 {
		return nil, err
	}
	if result.code != 0 {
		return nil, fmt.Errorf("could not read %s: %s", path, jscompat.Trim(result.stderr))
	}
	return ptr(jscompat.Trim(result.out)), nil
}

func collectToken(ctx context.Context, t *target, args NodeTokenArgs) (NodeTokenState, error) {
	seconds := valueOr(args.ReadySeconds, defaultReadySeconds)
	dataDir := valueOr(args.DataDir, defaultDataDir)
	path := nodeTokenPath(&dataDir)
	result, err := t.ask(ctx, true, waitForFile(path, seconds))
	if err != nil {
		return NodeTokenState{}, err
	}
	if result.code != 0 {
		return NodeTokenState{}, fmt.Errorf("%s has not written %s within %ss; it is written by a server, so check that this node is one, that it started, and that dataDir matches the data-dir it is actually running with", t.cfg.Address, path, jscompat.FormatNumber(seconds))
	}
	token, err := readNodeToken(ctx, t, path)
	if err != nil {
		return NodeTokenState{}, err
	}
	if token == nil {
		return NodeTokenState{}, fmt.Errorf("%s vanished between waiting for it and reading it", path)
	}
	return NodeTokenState{NodeTokenArgs{ReadySeconds: &seconds, DataDir: &dataDir}, token}, nil
}

func (NodeToken) WireDependencies(f infer.FieldSelector, _ *NodeTokenArgs, state *NodeTokenState) {
	f.OutputField(&state.Token).AlwaysSecret()
}

func (NodeToken) Create(ctx context.Context, req infer.CreateRequest[NodeTokenArgs]) (infer.CreateResponse[NodeTokenState], error) {
	if req.DryRun {
		return infer.CreateResponse[NodeTokenState]{Output: NodeTokenState{NodeTokenArgs: req.Inputs, Token: ptr("")}}, nil
	}
	t, err := targetFrom(ctx)
	if err != nil {
		return infer.CreateResponse[NodeTokenState]{}, err
	}
	state, err := collectToken(ctx, t, req.Inputs)
	return infer.CreateResponse[NodeTokenState]{ID: t.cfg.Address + ":node-token", Output: state}, err
}

func (NodeToken) Read(ctx context.Context, req infer.ReadRequest[NodeTokenArgs, NodeTokenState]) (infer.ReadResponse[NodeTokenArgs, NodeTokenState], error) {
	t, err := targetFrom(ctx)
	if err != nil {
		return infer.ReadResponse[NodeTokenArgs, NodeTokenState]{}, err
	}
	state := req.State
	if state.DataDir == nil && state.Token == nil {
		// An ID-only import can discover a custom directory from the k3s config.
		config, err := t.readFile(ctx, configPath)
		if err != nil {
			return infer.ReadResponse[NodeTokenArgs, NodeTokenState]{}, err
		}
		if config != nil {
			var observed struct {
				DataDir *string `yaml:"data-dir"`
			}
			if err := yaml.Unmarshal([]byte(config.content), &observed); err == nil {
				state.DataDir = observed.DataDir
			}
		}
	}
	token, err := readNodeToken(ctx, t, nodeTokenPath(state.DataDir))
	if err != nil || token == nil {
		return infer.ReadResponse[NodeTokenArgs, NodeTokenState]{}, err
	}
	state.Token = token
	return infer.ReadResponse[NodeTokenArgs, NodeTokenState]{ID: req.ID, Inputs: req.Inputs, State: state}, nil
}

func (NodeToken) Update(ctx context.Context, req infer.UpdateRequest[NodeTokenArgs, NodeTokenState]) (infer.UpdateResponse[NodeTokenState], error) {
	if req.DryRun {
		return infer.UpdateResponse[NodeTokenState]{Output: req.State}, nil
	}
	t, err := targetFrom(ctx)
	if err != nil {
		return infer.UpdateResponse[NodeTokenState]{}, err
	}
	state, err := collectToken(ctx, t, req.Inputs)
	return infer.UpdateResponse[NodeTokenState]{Output: state}, err
}

func (NodeToken) Diff(_ context.Context, req infer.DiffRequest[NodeTokenArgs, NodeTokenState]) (p.DiffResponse, error) {
	changed := differs(req.State.ReadySeconds, ptr(valueOr(req.Inputs.ReadySeconds, defaultReadySeconds))) ||
		differs(req.State.DataDir, ptr(valueOr(req.Inputs.DataDir, defaultDataDir)))
	return p.DiffResponse{HasChanges: changed}, nil
}

func (NodeToken) Delete(context.Context, infer.DeleteRequest[NodeTokenState]) (infer.DeleteResponse, error) {
	return infer.DeleteResponse{}, nil
}
