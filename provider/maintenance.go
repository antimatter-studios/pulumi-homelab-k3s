package homelabk3s

import (
	"context"
	"fmt"
	"time"

	"github.com/pulumi/pulumi-go-provider/infer"
)

const healthPollInterval = 2 * time.Second

type K3sDrain struct{}

type K3sDrainState struct {
	K3sMaintenanceArgs
	ApprovedRelease string `pulumi:"approvedRelease"`
	CompletedAt     string `pulumi:"completedAt"`
}

func (r *K3sDrain) Annotate(a infer.Annotator) {
	a.SetToken("index", "K3sDrain")
	a.Describe(r, "Wait for cluster health, then cordon and drain one node using the eviction API. The provider connects to an existing embedded-etcd server.")
}

func (K3sDrain) Create(ctx context.Context, req infer.CreateRequest[K3sMaintenanceArgs]) (infer.CreateResponse[K3sDrainState], error) {
	state := K3sDrainState{K3sMaintenanceArgs: req.Inputs}
	if !req.DryRun {
		if err := drainNode(ctx, req.Inputs); err != nil {
			return infer.CreateResponse[K3sDrainState]{}, err
		}
		state.ApprovedRelease, state.CompletedAt = req.Inputs.Release, vpsTimestamp()
	}
	return infer.CreateResponse[K3sDrainState]{ID: req.Inputs.NodeName, Output: state}, nil
}

func (K3sDrain) Read(_ context.Context, req infer.ReadRequest[K3sMaintenanceArgs, K3sDrainState]) (infer.ReadResponse[K3sMaintenanceArgs, K3sDrainState], error) {
	if req.State.CompletedAt == "" {
		return infer.ReadResponse[K3sMaintenanceArgs, K3sDrainState]{}, fmt.Errorf("a drain checkpoint cannot be imported; create K3sDrain to check health and drain")
	}
	return infer.ReadResponse[K3sMaintenanceArgs, K3sDrainState]{ID: req.ID, Inputs: req.Inputs, State: req.State}, nil
}

func (K3sDrain) Update(ctx context.Context, req infer.UpdateRequest[K3sMaintenanceArgs, K3sDrainState]) (infer.UpdateResponse[K3sDrainState], error) {
	if req.State.Release == req.Inputs.Release {
		if !req.DryRun {
			if err := req.Inputs.validate(); err != nil {
				return infer.UpdateResponse[K3sDrainState]{}, err
			}
		}
		state := req.State
		state.K3sMaintenanceArgs = req.Inputs
		return infer.UpdateResponse[K3sDrainState]{Output: state}, nil
	}
	created, err := (K3sDrain{}).Create(ctx, infer.CreateRequest[K3sMaintenanceArgs]{Inputs: req.Inputs, DryRun: req.DryRun})
	return infer.UpdateResponse[K3sDrainState]{Output: created.Output}, err
}

func drainNode(ctx context.Context, args K3sMaintenanceArgs) error {
	if err := args.validate(); err != nil {
		return err
	}
	t, err := targetFrom(ctx)
	if err != nil {
		return err
	}
	return args.drain(ctx, t)
}

func (a K3sMaintenanceArgs) drain(ctx context.Context, t *target) error {
	if err := a.waitHealthy(ctx, t, "", healthPollInterval); err != nil {
		return err
	}
	// kubectl drain cordons first and honors PodDisruptionBudgets. Standalone pods and
	// pods with emptyDir block maintenance. A retry can continue from an already cordoned node.
	ctx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	_, err := t.must(ctx, true, a.kubectl("drain", a.NodeName, "--ignore-daemonsets", "--timeout="+a.timeout().String()))
	return err
}

type K3sHealthGate struct{}

type K3sHealthGateArgs struct {
	K3sMaintenanceArgs
	Uncordon *bool `pulumi:"uncordon,optional"`
}

type K3sHealthGateState struct {
	K3sHealthGateArgs
	CompletedAt string `pulumi:"completedAt"`
}

func (r *K3sHealthGate) Annotate(a infer.Annotator) {
	a.SetToken("index", "K3sHealthGate")
	a.Describe(r, "Wait for all nodes and embedded etcd members to be healthy and for one node to report the pinned release. Optionally uncordon that node after the checks pass.")
}

func (K3sHealthGate) Create(ctx context.Context, req infer.CreateRequest[K3sHealthGateArgs]) (infer.CreateResponse[K3sHealthGateState], error) {
	state := K3sHealthGateState{K3sHealthGateArgs: req.Inputs}
	if !req.DryRun {
		if err := passHealthGate(ctx, req.Inputs); err != nil {
			return infer.CreateResponse[K3sHealthGateState]{}, err
		}
		state.CompletedAt = vpsTimestamp()
	}
	return infer.CreateResponse[K3sHealthGateState]{ID: req.Inputs.NodeName, Output: state}, nil
}

func (K3sHealthGate) Read(_ context.Context, req infer.ReadRequest[K3sHealthGateArgs, K3sHealthGateState]) (infer.ReadResponse[K3sHealthGateArgs, K3sHealthGateState], error) {
	if req.State.CompletedAt == "" {
		return infer.ReadResponse[K3sHealthGateArgs, K3sHealthGateState]{}, fmt.Errorf("a health checkpoint cannot be imported; create K3sHealthGate to run the checks")
	}
	return infer.ReadResponse[K3sHealthGateArgs, K3sHealthGateState]{ID: req.ID, Inputs: req.Inputs, State: req.State}, nil
}

func (K3sHealthGate) Update(ctx context.Context, req infer.UpdateRequest[K3sHealthGateArgs, K3sHealthGateState]) (infer.UpdateResponse[K3sHealthGateState], error) {
	if req.State.Release == req.Inputs.Release && valueOr(req.State.Uncordon, false) == valueOr(req.Inputs.Uncordon, false) {
		if !req.DryRun {
			if err := req.Inputs.validate(); err != nil {
				return infer.UpdateResponse[K3sHealthGateState]{}, err
			}
		}
		state := req.State
		state.K3sHealthGateArgs = req.Inputs
		return infer.UpdateResponse[K3sHealthGateState]{Output: state}, nil
	}
	created, err := (K3sHealthGate{}).Create(ctx, infer.CreateRequest[K3sHealthGateArgs]{Inputs: req.Inputs, DryRun: req.DryRun})
	return infer.UpdateResponse[K3sHealthGateState]{Output: created.Output}, err
}

func passHealthGate(ctx context.Context, args K3sHealthGateArgs) error {
	if err := args.validate(); err != nil {
		return err
	}
	t, err := targetFrom(ctx)
	if err != nil {
		return err
	}
	return args.pass(ctx, t)
}

func (a K3sHealthGateArgs) pass(ctx context.Context, t *target) error {
	if err := a.waitHealthy(ctx, t, a.Release, healthPollInterval); err != nil {
		return err
	}
	if valueOr(a.Uncordon, false) {
		ctx, cancel := context.WithTimeout(ctx, a.timeout())
		defer cancel()
		if _, err := t.must(ctx, true, a.kubectl("uncordon", a.NodeName)); err != nil {
			return fmt.Errorf("uncordon %s after health checks: %w", a.NodeName, err)
		}
	}
	return nil
}
