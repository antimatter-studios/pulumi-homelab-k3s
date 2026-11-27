package homelabk3s

import (
	"context"
	"fmt"
	"testing"

	"github.com/antimatter-studios/pulumi-homelab/provider/remote"
	"github.com/antimatter-studios/pulumi-homelab/provider/remote/remotetest"
	"github.com/antimatter-studios/pulumi-homelab/provider/shell"
	"github.com/blang/semver"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

const importAddress = "192.168.0.47"

func resourceURN(typ string) resource.URN {
	return resource.URN("urn:pulumi:test::test::homelabk3s:index:" + typ + "::imported")
}

func testServer(t *testing.T, runner remote.Runner) integration.Server {
	t.Helper()
	server, err := integration.NewServer(t.Context(), Name, semver.MustParse("0.0.0-dev"),
		integration.WithProvider(providerWith(func(_ context.Context, cfg remote.Config) (remote.Runner, error) {
			if cfg.Address != importAddress || cfg.Escalation != "none" || cfg.Port != 22 {
				return nil, fmt.Errorf("configuration did not reach the runner: %+v", cfg)
			}
			return runner, nil
		})))
	if err != nil {
		t.Fatal(err)
	}
	config := property.NewMap(map[string]property.Value{
		"address": property.New(importAddress), "port": property.New(22.0), "user": property.New("test"),
		"agentSocket": property.New("/tmp/test.sock"), "hostKeys": property.New([]property.Value{property.New("test key")}),
		"readSshConfig": property.New(false), "readDefaultKeys": property.New(false),
		"readKnownHosts": property.New(false), "strictHostKeyChecking": property.New(true),
		"escalation": property.New("none"), "version": property.New("0.0.0-dev"),
	})
	checked, err := server.CheckConfig(p.CheckRequest{Inputs: config})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("CheckConfig: %v, %v", checked.Failures, err)
	}
	if !checked.Inputs.Get("version").Equals(config.Get("version")) {
		t.Fatal("CheckConfig dropped the engine version")
	}
	if err := server.Configure(p.ConfigureRequest{Args: checked.Inputs}); err != nil {
		t.Fatal(err)
	}
	return server
}

func fileReadCommand(path string) string {
	q := shell.Quote(path)
	return "test -f " + q + " || exit 9; stat -c '%a %U %G' " + q + " && cat " + q
}

func assertImport(t *testing.T, typ, id string, inputs property.Map, fake *remotetest.Runner, calls int) p.ReadResponse {
	t.Helper()
	server := testServer(t, fake)
	urn := resourceURN(typ)
	read, err := server.Read(p.ReadRequest{ID: id, Urn: urn})
	if err != nil || read.ID != id {
		t.Fatalf("Read(%q): ID = %q, error = %v", id, read.ID, err)
	}
	diff, err := server.Diff(p.DiffRequest{ID: id, Urn: urn, State: read.Properties, OldInputs: read.Inputs, Inputs: inputs})
	if err != nil || diff.HasChanges || len(diff.DetailedDiff) != 0 {
		t.Fatalf("Diff after ID-only Read: %+v, %v", diff, err)
	}
	if err := server.Delete(p.DeleteRequest{ID: id, Urn: urn, Properties: read.Properties}); err != nil {
		t.Fatalf("Delete of imported resource: %v", err)
	}
	if len(fake.Commands()) != calls {
		t.Fatalf("import, diff and delete ran %v, want only %d read commands", fake.Commands(), calls)
	}
	return read
}

func TestNodeImports(t *testing.T) {
	const header = "# Managed by pulumi-homelab-k3s. Edits here are drift and will be overwritten.\n"
	for _, tc := range []struct {
		typ, role, id, config string
		args                  map[string]property.Value
	}{
		{"K3sServer", "server", "k3s", header + "cluster-init: true\n",
			map[string]property.Value{"clusterInit": property.New(true)}},
		{"K3sAgent", "agent", "k3s-agent", header + "token: \"test-token\"\nserver: \"https://pi:6443\"\n",
			map[string]property.Value{"server": property.New("https://pi:6443"), "token": property.New("test-token").WithSecret(true)}},
	} {
		t.Run(tc.typ, func(t *testing.T) {
			file := shell.Quote(unitPath(tc.id))
			command := "test -f " + file + " || exit 9; systemctl show '" + tc.id +
				"' --property=UnitFileState,ActiveState | tr '\\n' ' ' && stat -c '%a' " + file + " && cat " + file
			fake := remotetest.New().
				Once(command, remotetest.Reply{Stdout: "ActiveState=activating UnitFileState=enabled 644\n" + renderUnit(tc.role, defaultBinary, "")}).
				Once(fileReadCommand(configPath), remotetest.Reply{Stdout: "600 root root\n" + tc.config})
			read := assertImport(t, tc.typ, tc.id, property.NewMap(tc.args), fake, 2)
			config := read.Properties.Get("config")
			if config.AsString() != tc.config || !config.IsSecret() {
				t.Fatal("imported node config was not preserved as a secret")
			}
			if !read.Inputs.Get("token").IsNull() || !read.Properties.Get("written").IsNull() {
				t.Fatal("import filled declaration-only fields")
			}
		})
	}
}

func TestBinaryImport(t *testing.T) {
	const version = "v1.36.4+k3s1"
	fake := remotetest.New().Once("test -x '/usr/local/bin/k3s' || exit 9; '/usr/local/bin/k3s' --version",
		remotetest.Reply{Stdout: "k3s version " + version + " (0a1b2c3d)\n"})
	read := assertImport(t, "K3sBinary", defaultBinary, property.NewMap(map[string]property.Value{
		"version": property.New(version), "checksums": property.New(map[string]property.Value{"k3s": property.New("sha256")}),
	}), fake, 1)
	if read.Properties.Get("version").AsString() != version ||
		!read.Properties.Get("artifact").IsNull() || !read.Properties.Get("checksums").IsNull() {
		t.Fatal("import failed to preserve the observed version or invented a checksum or artifact")
	}
}

func TestNodeTokenImport(t *testing.T) {
	const path = "/mnt/storage/k3s/server/node-token"
	fake := remotetest.New().
		Once(fileReadCommand(configPath), remotetest.Reply{Stdout: "600 root root\ndata-dir: \"/mnt/storage/k3s\"\n"}).
		Once("test -f '"+path+"' || exit 9; cat '"+path+"'", remotetest.Reply{Stdout: "test-token\n"})
	read := assertImport(t, "NodeToken", importAddress+":node-token", property.NewMap(map[string]property.Value{
		"readySeconds": property.New(120.0), "dataDir": property.New("/mnt/storage/k3s"),
	}), fake, 2)
	if token := read.Properties.Get("token"); !token.IsSecret() || token.AsString() != "test-token" {
		t.Fatal("imported token was not preserved as a secret")
	}
	if !read.Properties.Get("readySeconds").IsNull() {
		t.Fatal("import filled the declaration-only timeout")
	}
}

func TestKubeconfigImport(t *testing.T) {
	fake := remotetest.New().Once(fileReadCommand(kubeconfigPath),
		remotetest.Reply{Stdout: "600 root root\n    server: https://127.0.0.1:6443\n"})
	read := assertImport(t, "Kubeconfig", importAddress+":kubeconfig", property.NewMap(map[string]property.Value{
		"readySeconds": property.New(120.0), "server": property.New(importAddress),
	}), fake, 1)
	if config := read.Properties.Get("config"); !config.IsSecret() || config.AsString() != "    server: https://"+importAddress+":6443\n" {
		t.Fatal("imported kubeconfig was not repointed and preserved as a secret")
	}
	if !read.Properties.Get("server").IsNull() || !read.Properties.Get("readySeconds").IsNull() {
		t.Fatal("import filled declaration-only fields")
	}
}

func TestBinaryDiffChangesVersionAndReplacesPath(t *testing.T) {
	server := testServer(t, remotetest.New())
	old := property.NewMap(map[string]property.Value{
		"version": property.New("v1.36.4+k3s1"), "path": property.New(defaultBinary),
	})
	urn := resourceURN("K3sBinary")
	args := property.NewMap(map[string]property.Value{
		"version":   property.New("v1.36.5+k3s1"),
		"checksums": property.New(map[string]property.Value{"k3s": property.New("sha256")}),
	})
	diff, err := server.Diff(p.DiffRequest{ID: defaultBinary, Urn: urn, State: old, Inputs: args})
	if err != nil || !diff.HasChanges || len(diff.DetailedDiff) != 1 || diff.DetailedDiff["version"].Kind != p.Update {
		t.Fatalf("version diff = %+v, %v", diff, err)
	}
	args = args.Set("path", property.New("/opt/k3s"))
	diff, err = server.Diff(p.DiffRequest{ID: defaultBinary, Urn: urn, State: old, Inputs: args})
	if err != nil || !diff.HasChanges || !diff.DeleteBeforeReplace || diff.DetailedDiff["path"].Kind != p.UpdateReplace {
		t.Fatalf("path diff = %+v, %v", diff, err)
	}
}

func TestAgentCreatePreviewLeavesConfigUnknownAndSecret(t *testing.T) {
	server := testServer(t, remotetest.New())
	created, err := server.Create(p.CreateRequest{
		Urn: resourceURN("K3sAgent"), DryRun: true,
		Properties: property.NewMap(map[string]property.Value{
			"server": property.New("https://pi:6443"), "token": property.New(property.Computed).WithSecret(true),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if config := created.Properties.Get("config"); !config.IsSecret() || !config.IsComputed() {
		t.Fatal("preview config must be unknown and secret")
	}
}
