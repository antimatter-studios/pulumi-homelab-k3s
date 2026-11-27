package homelabk3s

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/antimatter-studios/pulumi-homelab/provider/jscompat"
	"github.com/antimatter-studios/pulumi-homelab/provider/shell"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

type K3sBinary struct{}

type K3sBinaryArgs struct {
	Version   *string           `pulumi:"version"`
	Checksums map[string]string `pulumi:"checksums"`
	Path      *string           `pulumi:"path,optional"`
}

type K3sBinaryState struct {
	Version   *string           `pulumi:"version"`
	Checksums map[string]string `pulumi:"checksums,optional"`
	Path      *string           `pulumi:"path"`
	Artifact  *string           `pulumi:"artifact,optional"`
	Written   *bool             `pulumi:"written,optional"`
}

func (r *K3sBinary) Annotate(a infer.Annotator) {
	a.SetToken("index", "K3sBinary")
	a.Describe(r, "A pinned k3s release installed and verified on the machine.")
}

func artifactFor(machine string) string {
	switch jscompat.Trim(machine) {
	case "aarch64", "arm64":
		return "k3s-arm64"
	case "armv7l", "armv6l", "armv8l":
		return "k3s-armhf"
	case "x86_64":
		return "k3s"
	default:
		return ""
	}
}

func artifactForDebianArch(arch string) string {
	switch jscompat.Trim(arch) {
	case "arm64":
		return "k3s-arm64"
	case "armhf":
		return "k3s-armhf"
	case "amd64":
		return "k3s"
	default:
		return ""
	}
}

func artifactFrom(machine, debianArch string) string {
	if artifact := artifactForDebianArch(debianArch); artifact != "" {
		return artifact
	}
	return artifactFor(machine)
}

var versionLine = regexp.MustCompile(`(?:^|[\n\r\x{2028}\x{2029}])k3s version `)

func parseVersion(output string) string {
	for _, match := range versionLine.FindAllStringIndex(output, -1) {
		version, _, _ := strings.Cut(output[match[1]:], "\n")
		version = strings.TrimRightFunc(version, jscompat.IsSpace)
		if version != "" && !jscompat.IsSpace([]rune(version)[0]) {
			return jscompat.Fields(version)[0]
		}
	}
	return ""
}

func releaseURL(version, artifact string) string {
	tag := url.QueryEscape(version)
	tag = strings.NewReplacer("+", "%20", "%21", "!", "%27", "'", "%28", "(", "%29", ")", "%2A", "*").Replace(tag)
	return "https://github.com/k3s-io/k3s/releases/download/" + tag + "/" + artifact
}

func readVersion(ctx context.Context, t *target, path string) (*string, error) {
	q := shell.Quote(path)
	asked, err := t.ask(ctx, false, "test -x "+q+" || exit 9; "+q+" --version")
	if err != nil || asked.code == 9 {
		return nil, err
	}
	if asked.code != 0 {
		return nil, fmt.Errorf("could not run %s --version: %s", path, jscompat.Trim(asked.stderr))
	}
	version := parseVersion(asked.out)
	if version == "" {
		return nil, fmt.Errorf("%s --version said something unexpected: %s", path, jscompat.Trim(asked.out))
	}
	return &version, nil
}

func serviceAction(action string) string {
	return "for unit in k3s k3s-agent; do if systemctl is-active --quiet \"$unit\"; then systemctl " +
		action + " \"$unit\"; fi; done"
}

func installBinary(ctx context.Context, path string, args K3sBinaryArgs) (K3sBinaryState, error) {
	version := valueOr(args.Version, "")
	t, err := targetFrom(ctx)
	if err != nil {
		return K3sBinaryState{}, err
	}
	out, err := t.must(ctx, false, "uname -m; dpkg --print-architecture 2>/dev/null || true")
	if err != nil {
		return K3sBinaryState{}, err
	}
	machine, rest, _ := strings.Cut(jscompat.Trim(out), "\n")
	debianArch, _, _ := strings.Cut(rest, "\n")
	machine, debianArch = jscompat.Trim(machine), jscompat.Trim(debianArch)
	artifact := artifactFrom(machine, debianArch)
	if artifact == "" {
		detail := ""
		if debianArch != "" {
			detail = fmt.Sprintf(" with a '%s' userland", debianArch)
		}
		return K3sBinaryState{}, fmt.Errorf("no k3s release artifact known for a machine reporting '%s'%s", machine, detail)
	}
	sum := args.Checksums[artifact]
	if sum == "" {
		return K3sBinaryState{}, fmt.Errorf("this machine needs the '%s' artifact and no checksum was given for it; take it from sha256sum-*.txt in the %s release", artifact, version)
	}
	q := shell.Quote
	_, err = t.must(ctx, true, `set -e; tmp=$(mktemp); trap 'rm -f "$tmp"' EXIT; `+
		`curl -fsSL -o "$tmp" `+q(releaseURL(version, artifact))+"; "+
		`printf '%s  %s\n' `+q(sum)+` "$tmp" | sha256sum -c - >/dev/null; `+
		`install -m 0755 "$tmp" `+q(path)+"; "+serviceAction("restart"))
	if err != nil {
		return K3sBinaryState{}, err
	}
	installed, err := readVersion(ctx, t, path)
	if err != nil {
		return K3sBinaryState{}, err
	}
	if installed == nil || *installed != version {
		return K3sBinaryState{}, fmt.Errorf("installed %s but %s reports %s", version, path, valueOr(installed, "nothing"))
	}
	return K3sBinaryState{Version: installed, Checksums: args.Checksums, Path: &path, Artifact: &artifact, Written: ptr(true)}, nil
}

func (K3sBinary) Create(ctx context.Context, req infer.CreateRequest[K3sBinaryArgs]) (infer.CreateResponse[K3sBinaryState], error) {
	path := valueOr(req.Inputs.Path, defaultBinary)
	if req.DryRun {
		return infer.CreateResponse[K3sBinaryState]{ID: path, Output: K3sBinaryState{
			Version: req.Inputs.Version, Checksums: req.Inputs.Checksums, Path: &path, Artifact: ptr(""), Written: ptr(true),
		}}, nil
	}
	state, err := installBinary(ctx, path, req.Inputs)
	return infer.CreateResponse[K3sBinaryState]{ID: path, Output: state}, err
}

func (K3sBinary) Read(ctx context.Context, req infer.ReadRequest[K3sBinaryArgs, K3sBinaryState]) (infer.ReadResponse[K3sBinaryArgs, K3sBinaryState], error) {
	t, err := targetFrom(ctx)
	if err != nil {
		return infer.ReadResponse[K3sBinaryArgs, K3sBinaryState]{}, err
	}
	version, err := readVersion(ctx, t, req.ID)
	if err != nil || version == nil {
		return infer.ReadResponse[K3sBinaryArgs, K3sBinaryState]{}, err
	}
	state := req.State
	state.Version, state.Path = version, &req.ID
	inputs := req.Inputs
	if inputs.Version == nil {
		inputs.Version = version
		inputs.Path = &req.ID
	}
	return infer.ReadResponse[K3sBinaryArgs, K3sBinaryState]{ID: req.ID, Inputs: inputs, State: state}, nil
}

func (K3sBinary) Update(ctx context.Context, req infer.UpdateRequest[K3sBinaryArgs, K3sBinaryState]) (infer.UpdateResponse[K3sBinaryState], error) {
	if req.DryRun {
		return infer.UpdateResponse[K3sBinaryState]{Output: req.State}, nil
	}
	state, err := installBinary(ctx, req.ID, req.Inputs)
	return infer.UpdateResponse[K3sBinaryState]{Output: state}, err
}

// infer strips "version" before resource diffs. This resource uses that key for
// the k3s release, so compare the engine properties before infer strips it.
func diffBinary(req p.DiffRequest) p.DiffResponse {
	diff := make(map[string]p.PropertyDiff)
	oldVersion, version := req.State.Get("version"), req.Inputs.Get("version")
	if version.HasComputed() || (!oldVersion.IsNull() && !oldVersion.Equals(version)) {
		diff["version"] = p.PropertyDiff{Kind: p.Update}
	}
	oldPath, path := req.State.Get("path"), req.Inputs.Get("path")
	if path.IsNull() {
		path = property.New(defaultBinary)
	}
	if path.HasComputed() {
		diff["path"] = p.PropertyDiff{Kind: p.Update}
	} else if !oldPath.IsNull() && !oldPath.Equals(path) {
		diff["path"] = p.PropertyDiff{Kind: p.UpdateReplace}
	}
	return p.DiffResponse{HasChanges: len(diff) != 0, DetailedDiff: diff, DeleteBeforeReplace: true}
}

func (K3sBinary) Delete(ctx context.Context, req infer.DeleteRequest[K3sBinaryState]) (infer.DeleteResponse, error) {
	if !valueOr(req.State.Written, false) {
		p.GetLogger(ctx).Warningf("Keeping imported k3s binary %s because this provider has not installed it.", req.ID)
		return infer.DeleteResponse{}, nil
	}
	t, err := targetFrom(ctx)
	if err != nil {
		return infer.DeleteResponse{}, err
	}
	_, err = t.must(ctx, true, serviceAction("stop")+"; rm -f "+shell.Quote(req.ID))
	return infer.DeleteResponse{}, err
}
