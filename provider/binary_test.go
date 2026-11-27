package homelabk3s

import (
	"strings"
	"testing"
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
