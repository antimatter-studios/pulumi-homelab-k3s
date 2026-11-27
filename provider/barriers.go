package homelabk3s

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/antimatter-studios/pulumi-homelab/provider/jscompat"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

const defaultDrainTimeoutSeconds = 300.0

type CordonAndDrainBarrier struct {
	oneShot[CordonAndDrainBarrierArgs, CordonAndDrainBarrierState]
}

type CordonAndDrainBarrierArgs struct {
	Kubeconfig     *string  `pulumi:"kubeconfig"`
	NodeName       string   `pulumi:"nodeName"`
	TimeoutSeconds *float64 `pulumi:"timeoutSeconds,optional"`
	ReinstallToken *string  `pulumi:"reinstallToken"`
}

type CordonAndDrainBarrierState struct {
	Kubeconfig     *string  `pulumi:"kubeconfig,optional"`
	NodeName       string   `pulumi:"nodeName"`
	TimeoutSeconds *float64 `pulumi:"timeoutSeconds,optional"`
	ReinstallToken *string  `pulumi:"reinstallToken,optional"`
	CompletedAt    *string  `pulumi:"completedAt,optional"`
}

func (r *CordonAndDrainBarrier) Annotate(a infer.Annotator) {
	a.SetToken("index", "CordonAndDrainBarrier")
	a.Describe(r, "Cordon and drain a Kubernetes node when the resource is created or its reinstall token changes.")
}

func (CordonAndDrainBarrier) Create(ctx context.Context, req infer.CreateRequest[CordonAndDrainBarrierArgs]) (infer.CreateResponse[CordonAndDrainBarrierState], error) {
	args := req.Inputs
	state := CordonAndDrainBarrierState{
		Kubeconfig: args.Kubeconfig, NodeName: args.NodeName, TimeoutSeconds: args.TimeoutSeconds,
		ReinstallToken: args.ReinstallToken, CompletedAt: ptr(""),
	}
	if req.DryRun {
		return infer.CreateResponse[CordonAndDrainBarrierState]{Output: state}, nil
	}
	if err := cordonAndDrain(ctx, *args.Kubeconfig, args.NodeName, valueOr(args.TimeoutSeconds, defaultDrainTimeoutSeconds), nil); err != nil {
		return infer.CreateResponse[CordonAndDrainBarrierState]{}, err
	}
	state.CompletedAt = ptr(vpsTimestamp())
	return infer.CreateResponse[CordonAndDrainBarrierState]{ID: vpsTimedID("drain", args.NodeName), Output: state}, nil
}

func (CordonAndDrainBarrier) Read(_ context.Context, req infer.ReadRequest[CordonAndDrainBarrierArgs, CordonAndDrainBarrierState]) (infer.ReadResponse[CordonAndDrainBarrierArgs, CordonAndDrainBarrierState], error) {
	if req.State.ReinstallToken == nil && req.State.CompletedAt == nil {
		name, err := vpsTimedIDValue("drain", req.ID)
		if err != nil {
			return infer.ReadResponse[CordonAndDrainBarrierArgs, CordonAndDrainBarrierState]{}, err
		}
		req.State.NodeName, req.Inputs.NodeName = name, name
	}
	return infer.ReadResponse[CordonAndDrainBarrierArgs, CordonAndDrainBarrierState]{ID: req.ID, Inputs: req.Inputs, State: req.State}, nil
}

func (CordonAndDrainBarrier) Diff(ctx context.Context, req infer.DiffRequest[CordonAndDrainBarrierArgs, CordonAndDrainBarrierState]) (p.DiffResponse, error) {
	old := recordedVPSInput(ctx, "reinstallToken", req.State.ReinstallToken)
	return reinstallTokenDiff(old, req.Inputs.ReinstallToken), nil
}

// An import can leave a value out of the outputs even when the engine has
// recorded it as an input from a declaration.
func recordedVPSInput(ctx context.Context, key string, state *string) *string {
	if state != nil {
		return state
	}
	if req, ok := infer.GetRawRequest[p.DiffRequest](ctx); ok {
		if old := req.OldInputs.Get(key); old.IsString() {
			return ptr(old.AsString())
		}
	}
	return nil
}

func reinstallTokenDiff(old, want *string) p.DiffResponse {
	if !differs(old, want) {
		return p.DiffResponse{}
	}
	return p.DiffResponse{HasChanges: true, DetailedDiff: map[string]p.PropertyDiff{
		"reinstallToken": {Kind: p.UpdateReplace, InputDiff: true},
	}}
}

func runKubectl(ctx context.Context, timeout time.Duration, inherit bool, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	if inherit {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	}
	return cmd.Run()
}

func cordonAndDrain(ctx context.Context, kubeconfig, node string, seconds float64, warn func(error)) error {
	timeout := max(10*time.Second, time.Duration(seconds*float64(time.Second)))
	commands := [][]string{
		{"--kubeconfig", kubeconfig, "cordon", node},
		{"--kubeconfig", kubeconfig, "drain", node, "--ignore-daemonsets", "--delete-emptydir-data", "--force",
			"--disable-eviction", "--timeout=" + jscompat.FormatNumber(seconds) + "s"},
	}
	for _, cmd := range commands {
		if err := runKubectl(ctx, timeout, true, cmd...); err != nil {
			failure := fmt.Errorf("Failed to %s node %s: %w", cmd[2], node, err)
			if warn == nil {
				return failure
			}
			warn(failure)
		}
	}
	return nil
}
