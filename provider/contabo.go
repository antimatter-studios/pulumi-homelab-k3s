package homelabk3s

import (
	"context"
	"fmt"
	"strings"
	"time"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

type ContaboCredentialsArgs struct {
	ClientID     *string `pulumi:"clientId" provider:"secret"`
	ClientSecret *string `pulumi:"clientSecret" provider:"secret"`
	User         *string `pulumi:"user" provider:"secret"`
	Pass         *string `pulumi:"pass" provider:"secret"`
}

type ContaboCredentialsState struct {
	ClientID     *string `pulumi:"clientId,optional" provider:"secret"`
	ClientSecret *string `pulumi:"clientSecret,optional" provider:"secret"`
	User         *string `pulumi:"user,optional" provider:"secret"`
	Pass         *string `pulumi:"pass,optional" provider:"secret"`
}

func (args ContaboCredentialsArgs) state() ContaboCredentialsState {
	return ContaboCredentialsState{args.ClientID, args.ClientSecret, args.User, args.Pass}
}

func (state ContaboCredentialsState) absent() bool {
	return state.ClientID == nil && state.ClientSecret == nil && state.User == nil && state.Pass == nil
}

func (state ContaboCredentialsState) forRead(ctx context.Context, inputs ContaboCredentialsArgs) (ContaboCredentialsArgs, error) {
	for _, credential := range []struct {
		state *string
		input **string
	}{
		{state.ClientID, &inputs.ClientID},
		{state.ClientSecret, &inputs.ClientSecret},
		{state.User, &inputs.User},
		{state.Pass, &inputs.Pass},
	} {
		if credential.state != nil {
			*credential.input = credential.state
		}
	}
	if inputs.ClientID != nil && inputs.ClientSecret != nil && inputs.User != nil && inputs.Pass != nil {
		return inputs, nil
	}

	// An ID-only read uses one explicitly configured account.
	return infer.GetConfig[*Config](ctx).ContaboConfig.credentials()
}

type ContaboInstanceValidation struct {
	api contaboAPI
}

type ContaboInstanceValidationArgs struct {
	ContaboCredentialsArgs
	InstanceID string  `pulumi:"instanceId"`
	OSImage    *string `pulumi:"osImage,optional"`
}

type ContaboInstanceValidationState struct {
	ContaboCredentialsState
	InstanceID    string         `pulumi:"instanceId"`
	OSImage       *string        `pulumi:"osImage,optional"`
	Fingerprint   map[string]any `pulumi:"fingerprint,optional"`
	ResolvedImage any            `pulumi:"resolvedImage,optional"`
	Raw           any            `pulumi:"raw,optional"`
}

func (r *ContaboInstanceValidation) Annotate(a infer.Annotator) {
	a.SetToken("index", "ContaboInstanceValidation")
	a.Describe(r, "Read a Contabo instance and resolve an optional image name.")
}

func (r ContaboInstanceValidation) collect(ctx context.Context, state ContaboInstanceValidationState, creds ContaboCredentialsArgs) (ContaboInstanceValidationState, error) {
	token, err := r.api.accessToken(ctx, creds)
	if err != nil {
		return state, err
	}
	raw, err := r.api.instance(ctx, token, state.InstanceID)
	if err != nil {
		return state, err
	}
	switch raw {
	case nil, false, "", float64(0):
		return state, fmt.Errorf("Contabo instance lookup returned empty data for instanceId=%s", state.InstanceID)
	}
	state.Raw = raw
	state.Fingerprint = contaboFingerprint(state.InstanceID, raw)
	return r.resolveImage(ctx, state, token)
}

func (r ContaboInstanceValidation) resolveImage(ctx context.Context, state ContaboInstanceValidationState, token string) (ContaboInstanceValidationState, error) {
	if image := valueOr(state.OSImage, ""); image != "" {
		resolved, err := r.api.image(ctx, token, image)
		if err != nil {
			return state, err
		}
		if resolved == nil {
			return state, fmt.Errorf("Failed to resolve osImage='%s' for instanceId=%s", image, state.InstanceID)
		}
		state.ResolvedImage = resolved
	} else {
		state.ResolvedImage = nil
	}
	return state, nil
}

func validationState(args ContaboInstanceValidationArgs) ContaboInstanceValidationState {
	return ContaboInstanceValidationState{
		ContaboCredentialsState: args.ContaboCredentialsArgs.state(), InstanceID: args.InstanceID, OSImage: args.OSImage,
	}
}

func (r ContaboInstanceValidation) Create(ctx context.Context, req infer.CreateRequest[ContaboInstanceValidationArgs]) (infer.CreateResponse[ContaboInstanceValidationState], error) {
	state := validationState(req.Inputs)
	if req.DryRun {
		state.Fingerprint, state.ResolvedImage, state.Raw = map[string]any{}, map[string]any{}, map[string]any{}
		return infer.CreateResponse[ContaboInstanceValidationState]{Output: state}, nil
	}
	state, err := r.collect(ctx, state, req.Inputs.ContaboCredentialsArgs)
	return infer.CreateResponse[ContaboInstanceValidationState]{ID: req.Inputs.InstanceID, Output: state}, err
}

func (r ContaboInstanceValidation) Read(ctx context.Context, req infer.ReadRequest[ContaboInstanceValidationArgs, ContaboInstanceValidationState]) (infer.ReadResponse[ContaboInstanceValidationArgs, ContaboInstanceValidationState], error) {
	creds, err := req.State.ContaboCredentialsState.forRead(ctx, req.Inputs.ContaboCredentialsArgs)
	if err != nil {
		return infer.ReadResponse[ContaboInstanceValidationArgs, ContaboInstanceValidationState]{}, err
	}
	req.State.InstanceID = req.ID
	if req.State.OSImage == nil && req.State.ContaboCredentialsState.absent() {
		// The initial import cannot see the requested image name. A later refresh
		// can resolve it once the declaration has been recorded in the inputs.
		req.State.OSImage = req.Inputs.OSImage
	}
	if req.Inputs.InstanceID == "" {
		req.Inputs.InstanceID = req.ID
	}
	state, err := r.collect(ctx, req.State, creds)
	return infer.ReadResponse[ContaboInstanceValidationArgs, ContaboInstanceValidationState]{ID: req.ID, Inputs: req.Inputs, State: state}, err
}

func (ContaboInstanceValidation) Diff(ctx context.Context, req infer.DiffRequest[ContaboInstanceValidationArgs, ContaboInstanceValidationState]) (p.DiffResponse, error) {
	old, want := req.State, req.Inputs
	imported := old.ContaboCredentialsState.absent()
	oldImage := recordedVPSInput(ctx, "osImage", old.OSImage)
	imageChanged := differs(oldImage, want.OSImage) || (!imported && oldImage == nil && want.OSImage != nil)
	needsHeal := old.Fingerprint == nil || (want.OSImage != nil && old.ResolvedImage == nil)
	changed := old.InstanceID != want.InstanceID || imageChanged || needsHeal ||
		differs(recordedVPSInput(ctx, "clientId", old.ClientID), want.ClientID) ||
		differs(recordedVPSInput(ctx, "clientSecret", old.ClientSecret), want.ClientSecret) ||
		differs(recordedVPSInput(ctx, "user", old.User), want.User) ||
		differs(recordedVPSInput(ctx, "pass", old.Pass), want.Pass)
	return p.DiffResponse{HasChanges: changed}, nil
}

func (r ContaboInstanceValidation) Update(ctx context.Context, req infer.UpdateRequest[ContaboInstanceValidationArgs, ContaboInstanceValidationState]) (infer.UpdateResponse[ContaboInstanceValidationState], error) {
	old, args := req.State, req.Inputs
	if req.DryRun {
		return infer.UpdateResponse[ContaboInstanceValidationState]{Output: old}, nil
	}

	state := validationState(args)
	creds := args.ContaboCredentialsArgs
	// The ID-only read already observed the instance. Complete the import with
	// an image lookup and keep that observation.
	if old.ContaboCredentialsState.absent() && old.Fingerprint != nil && old.ResolvedImage == nil && old.InstanceID == args.InstanceID {
		state.Raw, state.Fingerprint = old.Raw, old.Fingerprint
		if valueOr(args.OSImage, "") == "" {
			return infer.UpdateResponse[ContaboInstanceValidationState]{Output: state}, nil
		}
		token, err := r.api.accessToken(ctx, creds)
		if err != nil {
			return infer.UpdateResponse[ContaboInstanceValidationState]{}, err
		}
		state, err = r.resolveImage(ctx, state, token)
		return infer.UpdateResponse[ContaboInstanceValidationState]{Output: state}, err
	}

	state, err := r.collect(ctx, state, creds)
	return infer.UpdateResponse[ContaboInstanceValidationState]{Output: state}, err
}

type ContaboInstanceReinstall struct {
	oneShot[ContaboInstanceReinstallArgs, ContaboInstanceReinstallState]
	api contaboAPI
}

type ContaboReinstallOptions struct {
	DefaultUser          *string  `pulumi:"defaultUser,optional"`
	UserData             *string  `pulumi:"userData,optional" provider:"secret"`
	DrainBeforeReinstall *bool    `pulumi:"drainBeforeReinstall,optional"`
	Kubeconfig           *string  `pulumi:"kubeconfig,optional"`
	NodeName             *string  `pulumi:"nodeName,optional"`
	DrainTimeoutSeconds  *float64 `pulumi:"drainTimeoutSeconds,optional"`
}

type ContaboInstanceReinstallArgs struct {
	ContaboCredentialsArgs
	ContaboReinstallOptions
	InstanceID     string  `pulumi:"instanceId"`
	OSImage        *string `pulumi:"osImage"`
	SSHPublicKey   *string `pulumi:"sshPublicKey"`
	ReinstallToken string  `pulumi:"reinstallToken"`
}

type ContaboInstanceReinstallState struct {
	ContaboCredentialsState
	ContaboReinstallOptions
	InstanceID     string   `pulumi:"instanceId"`
	OSImage        *string  `pulumi:"osImage,optional"`
	SSHPublicKey   *string  `pulumi:"sshPublicKey,optional"`
	ReinstallToken string   `pulumi:"reinstallToken"`
	ReinstalledAt  *string  `pulumi:"reinstalledAt,optional"`
	SSHKeySecretID *float64 `pulumi:"sshKeySecretId,optional"`
	ImageID        *string  `pulumi:"imageId,optional"`
}

func (r *ContaboInstanceReinstall) Annotate(a infer.Annotator) {
	a.SetToken("index", "ContaboInstanceReinstall")
	a.Describe(r, "Reinstall a Contabo instance when the resource is created or its reinstall token changes.")
}

func (r ContaboInstanceReinstall) Create(ctx context.Context, req infer.CreateRequest[ContaboInstanceReinstallArgs]) (infer.CreateResponse[ContaboInstanceReinstallState], error) {
	args := req.Inputs
	state := ContaboInstanceReinstallState{
		ContaboCredentialsState: args.ContaboCredentialsArgs.state(), ContaboReinstallOptions: args.ContaboReinstallOptions,
		InstanceID: args.InstanceID, OSImage: args.OSImage, SSHPublicKey: args.SSHPublicKey, ReinstallToken: args.ReinstallToken,
	}
	if req.DryRun {
		state.ReinstalledAt, state.SSHKeySecretID, state.ImageID = ptr(""), ptr(0.0), ptr("")
		return infer.CreateResponse[ContaboInstanceReinstallState]{Output: state}, nil
	}

	args.drain(ctx)
	token, err := r.api.accessToken(ctx, args.ContaboCredentialsArgs)
	if err != nil {
		return infer.CreateResponse[ContaboInstanceReinstallState]{}, err
	}
	image, err := r.api.image(ctx, token, *args.OSImage)
	if err != nil {
		return infer.CreateResponse[ContaboInstanceReinstallState]{}, err
	}
	if image == nil {
		return infer.CreateResponse[ContaboInstanceReinstallState]{}, fmt.Errorf("Failed to resolve osImage='%s' for reinstall", *args.OSImage)
	}

	state.SSHKeySecretID, err = r.api.upsertSSHKey(ctx, token, "pulumi-node-"+args.InstanceID, *args.SSHPublicKey)
	if err != nil {
		return infer.CreateResponse[ContaboInstanceReinstallState]{}, err
	}
	if err := r.api.reinstall(ctx, token, args.InstanceID, image, state.SSHKeySecretID, args.DefaultUser, args.UserData); err != nil {
		return infer.CreateResponse[ContaboInstanceReinstallState]{}, err
	}

	if id, ok := jsonField(image, "imageId").(string); ok {
		state.ImageID = &id
	}
	state.ReinstalledAt = ptr(vpsTimestamp())
	return infer.CreateResponse[ContaboInstanceReinstallState]{ID: args.InstanceID + "-" + args.ReinstallToken, Output: state}, nil
}

func (args ContaboInstanceReinstallArgs) drain(ctx context.Context) {
	kubeconfig, node := valueOr(args.Kubeconfig, ""), valueOr(args.NodeName, "")
	if !valueOr(args.DrainBeforeReinstall, false) || kubeconfig == "" || node == "" || !localPathExists(kubeconfig) {
		return
	}

	logger := p.GetLogger(ctx)
	if err := runKubectl(ctx, 10*time.Second, false, "--kubeconfig", kubeconfig, "get", "node", node, "--request-timeout=5s"); err != nil {
		logger.Infof("Node %s not found in cluster, skipping drain", node)
		return
	}
	logger.Infof("Draining node %s before reinstall...", node)
	_ = cordonAndDrain(ctx, kubeconfig, node, valueOr(args.DrainTimeoutSeconds, defaultDrainTimeoutSeconds), func(err error) {
		logger.Warning(err.Error())
	})
}

func (ContaboInstanceReinstall) Read(_ context.Context, req infer.ReadRequest[ContaboInstanceReinstallArgs, ContaboInstanceReinstallState]) (infer.ReadResponse[ContaboInstanceReinstallArgs, ContaboInstanceReinstallState], error) {
	if req.State.InstanceID == "" {
		// Contabo instance IDs are numeric. Reinstall tokens can contain hyphens.
		instance, token, ok := strings.Cut(req.ID, "-")
		if !ok || instance == "" {
			return infer.ReadResponse[ContaboInstanceReinstallArgs, ContaboInstanceReinstallState]{}, fmt.Errorf("invalid ID %q, expected <instanceId>-<reinstallToken>", req.ID)
		}
		req.State.InstanceID, req.State.ReinstallToken = instance, token
		req.Inputs.InstanceID, req.Inputs.ReinstallToken = instance, token
	}
	return infer.ReadResponse[ContaboInstanceReinstallArgs, ContaboInstanceReinstallState]{ID: req.ID, Inputs: req.Inputs, State: req.State}, nil
}

func (ContaboInstanceReinstall) Diff(_ context.Context, req infer.DiffRequest[ContaboInstanceReinstallArgs, ContaboInstanceReinstallState]) (p.DiffResponse, error) {
	return reinstallTokenDiff(&req.State.ReinstallToken, &req.Inputs.ReinstallToken), nil
}
