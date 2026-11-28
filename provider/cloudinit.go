package homelabk3s

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/antimatter-studios/pulumi-homelab/provider/checks"
	"github.com/antimatter-studios/pulumi-homelab/provider/shell"
	"github.com/pulumi/pulumi-go-provider/infer"
	"gopkg.in/yaml.v3"
)

type RenderCloudInitArgs struct {
	K3sServerArgs
	Role      string            `pulumi:"role"`
	Version   string            `pulumi:"version"`
	Checksums map[string]string `pulumi:"checksums"`
}

func (a *RenderCloudInitArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.Role, "The k3s role: server or agent. Use the corresponding resource inputs for that role.")
	ann.Describe(&a.Version, "The pinned k3s release tag, as in K3sBinary.")
	ann.Describe(&a.Checksums, "SHA-256 checksums keyed by release artifact, as in K3sBinary.")
}

type RenderCloudInitResult struct {
	UserData string `pulumi:"userData" provider:"secret"`
}

type renderCloudInit struct{}

func (r *renderCloudInit) Annotate(a infer.Annotator) {
	a.SetToken("index", "renderCloudInit")
	a.Describe(r, "Render cloud-config user data for a k3s server or agent on a systemd image. Use the Output form to retain the secrecy of the user data.")
}

func (renderCloudInit) Invoke(_ context.Context, req infer.FunctionRequest[RenderCloudInitArgs]) (infer.FunctionResponse[RenderCloudInitResult], error) {
	userData, err := cloudInit(req.Input)
	return infer.FunctionResponse[RenderCloudInitResult]{Output: RenderCloudInitResult{UserData: userData}}, err
}

type cloudInitFile struct {
	Path        string `yaml:"path"`
	Owner       string `yaml:"owner"`
	Permissions string `yaml:"permissions"`
	Content     string `yaml:"content"`
}

type cloudConfig struct {
	Packages   []string        `yaml:"packages"`
	WriteFiles []cloudInitFile `yaml:"write_files"`
	RunCmd     [][]string      `yaml:"runcmd"`
}

func cloudInit(args RenderCloudInitArgs) (string, error) {
	var id string
	switch args.Role {
	case "server":
		id = serverService
	case "agent":
		id = agentService
		if valueOr(args.ClusterInit, false) || len(args.TLSSan) > 0 || len(args.Disable) > 0 {
			return "", errors.New("clusterInit, tlsSan and disable are server options")
		}
	default:
		return "", errors.New("role must be server or agent")
	}

	state, err := wantedNode(args.Role, args.K3sServerArgs)
	if err != nil {
		return "", err
	}
	install, err := cloudInitBinaryCommand(valueOr(args.Binary, defaultBinary), args.Version, args.Checksums)
	if err != nil {
		return "", err
	}
	command := install + "\n" + configureNodeService(id, state)
	if mount := valueOr(args.RequiresMount, ""); mount != "" {
		command = "if ! (" + checks.MountedAt(mount) + "); then\n" +
			"  echo " + shell.Quote("k3s requires "+mount+" to be mounted") + " >&2\n  exit 1\nfi\n" + command
	}
	data, err := yaml.Marshal(cloudConfig{
		Packages: []string{"curl"},
		WriteFiles: []cloudInitFile{
			{Path: configPath, Owner: "root:root", Permissions: *state.ConfigMode, Content: *state.Config},
			{Path: unitPath(id), Owner: "root:root", Permissions: *state.UnitMode, Content: *state.Unit},
		},
		RunCmd: [][]string{{"sh", "-ec", command}},
	})
	if err != nil {
		return "", err
	}
	return "#cloud-config\n" + string(data), nil
}

func cloudInitBinaryCommand(path, version string, checksums map[string]string) (string, error) {
	if strings.TrimSpace(version) == "" {
		return "", errors.New("version must be a k3s release tag")
	}

	var userland, machine, installs strings.Builder
	for _, platform := range binaryPlatforms {
		artifact := platform.artifact
		fmt.Fprintf(&userland, "  %s) artifact=%s ;;\n", platform.debianArch, artifact)
		fmt.Fprintf(&machine, "      %s) artifact=%s ;;\n", strings.Join(platform.machines, "|"), artifact)
		if sum := checksums[artifact]; sum != "" {
			if _, err := hex.DecodeString(sum); err != nil || len(sum) != 64 {
				return "", fmt.Errorf("checksums[%q] must be a 64-digit hexadecimal SHA-256", artifact)
			}
			fmt.Fprintf(&installs, "  %s) %s ;;\n", artifact, binaryInstallCommand(path, version, artifact, sum))
		}
	}
	if installs.Len() == 0 {
		return "", errors.New("a SHA-256 checksum is required for at least one k3s release artifact (k3s, k3s-arm64 or k3s-armhf)")
	}
	return `case "$(dpkg --print-architecture 2>/dev/null || true)" in` + "\n" + userland.String() +
		`  *) case "$(uname -m)" in` + "\n" + machine.String() +
		"      *) echo 'No k3s release artifact for this architecture' >&2; exit 1 ;;\n    esac ;;\nesac\n" +
		`case "$artifact" in` + "\n" + installs.String() +
		`  *) echo "No checksum for k3s artifact $artifact" >&2; exit 1 ;;` + "\nesac", nil
}
