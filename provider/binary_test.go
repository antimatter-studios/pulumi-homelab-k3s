package homelabk3s

import (
	"strings"
	"testing"

	"github.com/antimatter-studios/pulumi-homelab/provider/remote/remotetest"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/urn"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

func TestArtifactFor(t *testing.T) {
	for machine, want := range map[string]string{
		"aarch64": "k3s-arm64", "arm64": "k3s-arm64", "armv7l": "k3s-armhf",
		"armv6l": "k3s-armhf", "x86_64": "k3s", "armv8l": "k3s-armhf",
		"aarch64\n": "k3s-arm64", "riscv64": "", "": "",
	} {
		if got := artifactFor(machine); got != want {
			t.Errorf("artifactFor(%q) = %q, want %q", machine, got, want)
		}
	}
}

func TestParseVersion(t *testing.T) {
	const output = "k3s version v1.36.4+k3s1 (0a1b2c3d)\ngo version go1.24.3\n"
	if got := parseVersion(output); got != "v1.36.4+k3s1" || !strings.Contains(got, "+k3s1") {
		t.Errorf("version = %q", got)
	}
	for _, input := range []string{"command not found", ""} {
		if got := parseVersion(input); got != "" {
			t.Errorf("parseVersion(%q) = %q", input, got)
		}
	}
}

func TestReleaseURL(t *testing.T) {
	want := "https://github.com/k3s-io/k3s/releases/download/v1.36.4%2Bk3s1/k3s-arm64"
	if got := releaseURL("v1.36.4+k3s1", "k3s-arm64"); got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

func TestArtifactFrom(t *testing.T) {
	for _, tc := range []struct{ machine, debianArch, want string }{
		{"aarch64", "armhf", "k3s-armhf"}, {"armv8l", "armhf", "k3s-armhf"},
		{"aarch64", "arm64", "k3s-arm64"}, {"x86_64", "amd64", "k3s"},
		{"aarch64", "", "k3s-arm64"}, {"x86_64", "", "k3s"},
		{"riscv64", "", ""}, {"riscv64", "riscv64", ""},
	} {
		if got := artifactFrom(tc.machine, tc.debianArch); got != tc.want {
			t.Errorf("artifactFrom(%q, %q) = %q, want %q", tc.machine, tc.debianArch, got, tc.want)
		}
	}
	if got := artifactFor("aarch64"); got != "k3s-arm64" {
		t.Errorf("the kernel-only answer = %q", got)
	}
}

func TestRollingBinaryRetriesRestartAfterRefresh(t *testing.T) {
	oldVersion := property.New("v1.36.3+k3s1")
	version := property.New("v1.36.4+k3s1")
	// A previous attempt has moved the binary into place and failed to restart the unit.
	state := property.NewMap(map[string]property.Value{"path": property.New(defaultBinary), "version": version})
	oldInputs := property.NewMap(map[string]property.Value{"version": oldVersion})
	inputs := property.NewMap(map[string]property.Value{
		"version": version, "restartUnit": property.New("k3s"),
		"checksums": property.New(map[string]property.Value{"k3s": property.New("release-sha256")}),
	})
	matchesInstall := func(cmd string) bool {
		check := strings.Index(cmd, "sha256sum -c -")
		move := strings.Index(cmd, "mv -fT --")
		restart := strings.Index(cmd, "systemctl restart -- ")
		return strings.Contains(cmd, "/usr/local/bin/k3s.XXXXXX") &&
			strings.Contains(cmd, "k3s.service") && check >= 0 && move > check && restart > move &&
			!strings.Contains(cmd, "is-active")
	}
	fake := remotetest.New().
		On("uname -m; dpkg --print-architecture 2>/dev/null || true", remotetest.Reply{Stdout: "x86_64\namd64"}).
		When(matchesInstall, func(string) remotetest.Reply { return remotetest.Reply{Code: 1, Stderr: "restart failed"} }, 1).
		When(matchesInstall, func(string) remotetest.Reply { return remotetest.Reply{} }, 1).
		When(func(cmd string) bool { return strings.HasSuffix(cmd, " --version") },
			func(string) remotetest.Reply { return remotetest.Reply{Stdout: "k3s version " + version.AsString()} }, 1)
	server := testServer(t, fake)
	binaryURN := resourceURN("K3sBinary")
	diffReq := p.DiffRequest{Urn: binaryURN, ID: defaultBinary, State: state, OldInputs: oldInputs, Inputs: inputs}
	diff, err := server.Diff(diffReq)
	if err != nil || !diff.HasChanges || diff.DetailedDiff["version"].Kind != p.Update {
		t.Fatalf("refreshed rolling diff = %+v, %v", diff, err)
	}
	diffReq.Inputs = inputs.Delete("restartUnit")
	diff, err = server.Diff(diffReq)
	if err != nil || diff.HasChanges {
		t.Fatalf("existing declarations must keep their previous diff = %+v, %v", diff, err)
	}
	diffReq.OldInputs = inputs
	diffReq.Inputs = inputs.Set("version", version.WithDependencies([]urn.URN{resourceURN("K3sDrain")}))
	diff, err = server.Diff(diffReq)
	if err != nil || diff.HasChanges {
		t.Fatalf("the same release with a dependency must produce no diff: %+v, %v", diff, err)
	}

	req := p.UpdateRequest{Urn: binaryURN, ID: defaultBinary, State: state, Inputs: inputs}
	if _, err := server.Update(req); err == nil || !strings.Contains(err.Error(), "restart failed") {
		t.Fatalf("failed restart reported %v", err)
	}
	updated, err := server.Update(req)
	if err != nil || !updated.Properties.Get("version").Equals(version) {
		t.Fatalf("retry update = %v, %v", updated, err)
	}
}
