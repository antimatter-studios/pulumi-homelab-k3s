package homelabk3s

import (
	"strings"
	"testing"

	"github.com/antimatter-studios/pulumi-homelab/provider/remote/remotetest"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

const customDataDir = "/mnt/storage/k3s-data"

func anyCommand() *remotetest.Runner {
	return remotetest.New().When(func(string) bool { return true }, func(string) remotetest.Reply { return remotetest.Reply{} }, 0)
}

func nodeDeleteState(dataDir string) property.Map {
	return property.NewMap(map[string]property.Value{
		"config":     property.New("data-dir: " + dataDir + "\nnode-name: homelab\n").WithSecret(true),
		"unit":       property.New("[Service]\nExecStart=/usr/local/bin/k3s server --data-dir " + dataDir + "\n"),
		"configMode": property.New("0600"), "unitMode": property.New("0644"),
		"enabled": property.New(true), "started": property.New(true), "written": property.New(true),
	})
}

func runDelete(t *testing.T, typ, id string, props property.Map) []string {
	t.Helper()
	runner := anyCommand()
	server := testServer(t, runner)
	if err := server.Delete(p.DeleteRequest{ID: id, Urn: resourceURN(typ), Properties: props}); err != nil {
		t.Fatal(err)
	}
	return runner.Commands()
}

func TestNodeDeleteCommands(t *testing.T) {
	for _, tc := range []struct{ typ, id, want string }{
		{"K3sServer", serverService, "systemctl disable --now 'k3s' || true; rm -f '/etc/systemd/system/k3s.service' '/etc/rancher/k3s/config.yaml'; systemctl daemon-reload"},
		{"K3sAgent", agentService, "systemctl disable --now 'k3s-agent' || true; rm -f '/etc/systemd/system/k3s-agent.service' '/etc/rancher/k3s/config.yaml'; systemctl daemon-reload"},
	} {
		for _, dir := range []string{defaultDataDir, customDataDir} {
			t.Run(tc.typ+dir, func(t *testing.T) {
				got := runDelete(t, tc.typ, tc.id, nodeDeleteState(dir))
				if len(got) != 1 || got[0] != tc.want {
					t.Fatalf("delete ran %q, want exactly %q", got, tc.want)
				}
			})
		}
	}
}

// The data directory holds the live cluster: no Delete command may name it, whatever it does.
func TestNodeDeleteNeverTouchesDataDir(t *testing.T) {
	for _, typ := range []string{"K3sServer", "K3sAgent"} {
		id := serverService
		if typ == "K3sAgent" {
			id = agentService
		}
		for _, dir := range []string{defaultDataDir, customDataDir} {
			t.Run(typ+dir, func(t *testing.T) {
				for _, cmd := range runDelete(t, typ, id, nodeDeleteState(dir)) {
					for _, bad := range []string{dir, "/var/lib/rancher", "data-dir", "$DATA", "rm -r", "rm -rf"} {
						if strings.Contains(cmd, bad) {
							t.Fatalf("BLOCKER: delete command %q references %q", cmd, bad)
						}
					}
				}
			})
		}
	}
}

func TestBinaryDeleteStopsServicesAndRemovesBinary(t *testing.T) {
	for _, path := range []string{defaultBinary, "/opt/k3s/bin/k3s"} {
		t.Run(path, func(t *testing.T) {
			props := property.NewMap(map[string]property.Value{
				"path": property.New(path), "version": property.New("v1.36.4+k3s1"),
			})
			want := `for unit in k3s k3s-agent; do if systemctl is-active --quiet "$unit"; then systemctl stop "$unit"; fi; done; rm -f '` + path + `'`
			got := runDelete(t, "K3sBinary", path, props)
			if len(got) != 1 || got[0] != want {
				t.Fatalf("delete ran %q, want exactly %q", got, want)
			}
		})
	}
}

func TestTokenAndKubeconfigDeleteRunNothing(t *testing.T) {
	for _, tc := range []struct{ typ, field string }{{"NodeToken", "token"}, {"Kubeconfig", "config"}} {
		t.Run(tc.typ, func(t *testing.T) {
			props := property.NewMap(map[string]property.Value{
				tc.field: property.New("secret").WithSecret(true), "dataDir": property.New(customDataDir),
			})
			if tc.typ == "Kubeconfig" {
				props = property.NewMap(map[string]property.Value{tc.field: property.New("secret").WithSecret(true)})
			}
			typ := tc.typ
			if got := runDelete(t, typ, typ, props); len(got) != 0 {
				t.Fatalf("%s delete ran %q, want nothing", typ, got)
			}
		})
	}
}
