package homelabk3s

import (
	"strings"
	"testing"

	"github.com/antimatter-studios/pulumi-homelab/provider/checks"
)

func assertContains(t *testing.T, got string, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(got, part) {
			t.Errorf("%q does not contain %q", got, part)
		}
	}
}

func assertOmits(t *testing.T, got string, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if strings.Contains(got, part) {
			t.Errorf("%q contains %q", got, part)
		}
	}
}

func TestRenderConfig(t *testing.T) {
	for _, tc := range []struct {
		name            string
		pairs           []configPair
		contains, omits []string
	}{
		{"quotes strings", []configPair{{"node-name", "no"}, {"token", "1.36"}, {"server", "https://a:6443"}},
			[]string{`node-name: "no"`, `token: "1.36"`, `server: "https://a:6443"`}, nil},
		{"escapes quotes", []configPair{{"node-name", `it"s`}}, []string{`node-name: "it\"s"`}, nil},
		{"booleans and numbers", []configPair{{"cluster-init", true}, {"x", 3}},
			[]string{"cluster-init: true", "x: 3"}, nil},
		{"lists", []configPair{{"tls-san", []string{"a", "b"}}}, []string{"tls-san:\n  - \"a\"\n  - \"b\""}, nil},
		{"omits empty lists and unset inputs", []configPair{{"disable", []string{}}, {"tls-san", nil}, {"token", "x"}},
			[]string{`token: "x"`}, []string{"disable", "tls-san"}},
		{"names the writer", nil, []string{"pulumi-homelab-k3s"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := renderConfig(tc.pairs)
			if err != nil {
				t.Fatal(err)
			}
			assertContains(t, got, tc.contains...)
			assertOmits(t, got, tc.omits...)
		})
	}
}

func TestRenderUnit(t *testing.T) {
	out := renderUnit("server", "/usr/local/bin/k3s", "")
	assertContains(t, out, "Delegate=yes", "KillMode=process", "TimeoutStartSec=0")
	assertOmits(t, out, "RequiresMountsFor")
	assertContains(t, renderUnit("server", "/opt/k3s", ""), "ExecStart=/opt/k3s server")
	assertContains(t, renderUnit("agent", "/usr/local/bin/k3s", ""), "ExecStart=/usr/local/bin/k3s agent")
	assertContains(t, renderUnit("server", "/usr/local/bin/k3s", "/mnt/storage/k3s"), "RequiresMountsFor=/mnt/storage/k3s")
	assertContains(t, renderUnit("server", "/usr/local/bin/k3s", "/mnt/storage"),
		"RequiresMountsFor=/mnt/storage", "ConditionPathIsMountPoint=/mnt/storage")
}

func requireConfig(t *testing.T, role string, args K3sServerArgs) string {
	t.Helper()
	got, err := configFor(role, args)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestConfigFor(t *testing.T) {
	out := requireConfig(t, "server", K3sServerArgs{ClusterInit: ptr(true), TLSSan: []string{"192.168.0.47"}})
	assertContains(t, out, "cluster-init: true", "tls-san:\n  - \"192.168.0.47\"")
	assertOmits(t, out, "server:")

	for _, tc := range []struct {
		role, want string
		args       K3sServerArgs
	}{
		{"server", "cannot both start a cluster and join one", K3sServerArgs{ClusterInit: ptr(true), Server: ptr("https://a:6443"), Token: ptr("t")}},
		{"server", "no token was given", K3sServerArgs{Server: ptr("https://a:6443")}},
		{"agent", "no token was given", K3sServerArgs{Server: ptr("https://a:6443"), Token: ptr("")}},
		{"agent", "which server to join", K3sServerArgs{Server: ptr(""), Token: ptr("t")}},
	} {
		if _, err := configFor(tc.role, tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s config error = %v, want %q", tc.role, err, tc.want)
		}
	}

	out = requireConfig(t, "agent", K3sServerArgs{Server: ptr("https://a:6443"), Token: ptr("t"),
		NodeArgs: NodeArgs{Extra: map[string]any{"node-ip": "192.168.0.48"}}})
	assertOmits(t, out, "tls-san")
	assertContains(t, out, `node-ip: "192.168.0.48"`)

	one := requireConfig(t, "server", K3sServerArgs{ClusterInit: ptr(true), NodeArgs: NodeArgs{Extra: map[string]any{"b": "2", "a": "1"}}})
	other := requireConfig(t, "server", K3sServerArgs{ClusterInit: ptr(true), NodeArgs: NodeArgs{Extra: map[string]any{"a": "1", "b": "2"}}})
	if one != other {
		t.Errorf("extra settings order changed the file: %q != %q", one, other)
	}
	assertContains(t, one, "a: \"1\"\nb: \"2\"")

	out = requireConfig(t, "server", K3sServerArgs{ClusterInit: ptr(false), NodeArgs: NodeArgs{DataDir: ptr("/mnt/storage/k3s")}})
	assertContains(t, out, `data-dir: "/mnt/storage/k3s"`)
	out = requireConfig(t, "agent", K3sServerArgs{Server: ptr("https://a:6443"), Token: ptr("t"),
		NodeArgs: NodeArgs{DataDir: ptr("/mnt/storage/k3s"), KubeletArg: []string{"root-dir=/mnt/storage/k3s/kubelet"}}})
	assertContains(t, out, "kubelet-arg:\n  - \"root-dir=/mnt/storage/k3s/kubelet\"")
}

func TestMountCheck(t *testing.T) {
	got := checks.MountedAt("/mnt/storage")
	if !strings.Contains(got, "mountpoint") && !strings.Contains(got, "findmnt") {
		t.Errorf("missing mount check: %q", got)
	}
	assertOmits(t, got, "test -d")
	assertContains(t, got, "'/mnt/storage'")
	assertContains(t, checks.MountedAt("/mnt/it's"), `'\''`)
}
