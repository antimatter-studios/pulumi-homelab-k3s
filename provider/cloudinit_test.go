package homelabk3s

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antimatter-studios/pulumi-homelab/provider/remote/remotetest"
	"github.com/antimatter-studios/pulumi-homelab/provider/shell"
	"github.com/blang/semver"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"gopkg.in/yaml.v3"
)

func parseCloudConfig(t *testing.T, userData string) cloudConfig {
	t.Helper()
	if !strings.HasPrefix(userData, "#cloud-config\n") {
		t.Fatal("missing cloud-config header")
	}
	var config cloudConfig
	decoder := yaml.NewDecoder(strings.NewReader(userData))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		t.Fatal(err)
	}
	if len(config.WriteFiles) != 2 || len(config.RunCmd) != 1 ||
		len(config.RunCmd[0]) != 3 || config.RunCmd[0][0] != "sh" || config.RunCmd[0][1] != "-ec" {
		t.Fatal("expected config and unit in write_files and one shell command in runcmd")
	}
	return config
}

func TestRenderCloudInitInvokeMatchesNodeAndImports(t *testing.T) {
	// The default provider can render user data without SSH or Contabo configuration.
	provider, err := integration.NewServer(t.Context(), Name, semver.MustParse("0.0.0-dev"), integration.WithProvider(Provider()))
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.Configure(p.ConfigureRequest{}); err != nil {
		t.Fatal(err)
	}

	const version = "v1.36.4+k3s1"
	sum := strings.Repeat("a", 64)
	token := "test \"token\"\nsecond line"
	for _, tc := range []struct {
		role, typ, id, status string
		node                  K3sServerArgs
		inputs                map[string]property.Value
	}{
		{
			role: "server", typ: "K3sServer", id: "k3s", status: "UnitFileState=enabled ActiveState=active",
			node: K3sServerArgs{
				Token: &token, ClusterInit: ptr(true), TLSSan: []string{"api.example.com"},
				NodeArgs: NodeArgs{Extra: map[string]any{"flannel-backend": "none", "disable-network-policy": true}},
			},
			inputs: map[string]property.Value{
				"token": property.New(token), "clusterInit": property.New(true),
				"tlsSan": property.New([]property.Value{property.New("api.example.com")}),
				"extra":  property.New(map[string]property.Value{"flannel-backend": property.New("none"), "disable-network-policy": property.New(true)}),
			},
		},
		{
			role: "agent", typ: "K3sAgent", id: "k3s-agent", status: "UnitFileState=disabled ActiveState=inactive",
			node: K3sServerArgs{
				Token: &token, Server: ptr("https://api.example.com:6443"),
				NodeArgs: NodeArgs{NodeName: ptr("worker"), Binary: ptr("/opt/bin/k3s"), Enabled: ptr(false), Started: ptr(false)},
			},
			inputs: map[string]property.Value{
				"token": property.New(token).WithSecret(true), "server": property.New("https://api.example.com:6443"),
				"nodeName": property.New("worker"), "binary": property.New("/opt/bin/k3s"),
				"enabled": property.New(false), "started": property.New(false),
			},
		},
	} {
		t.Run(tc.role, func(t *testing.T) {
			inputs := property.NewMap(tc.inputs)
			invoke, err := provider.Invoke(p.InvokeRequest{
				Token: "homelab-k3s:index:renderCloudInit",
				Args: inputs.Set("role", property.New(tc.role)).Set("version", property.New(version)).
					Set("checksums", property.New(map[string]property.Value{"k3s": property.New(sum)})),
			})
			if err != nil || len(invoke.Failures) > 0 {
				t.Fatalf("renderCloudInit invoke: %v, %v", err, invoke.Failures)
			}
			userData := invoke.Return.Get("userData")
			if !userData.IsSecret() || !userData.IsString() {
				t.Fatal("expected secret string userData")
			}

			config := parseCloudConfig(t, userData.AsString())
			want, err := wantedNode(tc.role, tc.node)
			if err != nil {
				t.Fatal(err)
			}
			configFile, unitFile := config.WriteFiles[0], config.WriteFiles[1]
			if configFile != (cloudInitFile{configPath, "root:root", "0600", *want.Config}) {
				t.Fatal("cloud-init config differs from wantedNode")
			}
			if unitFile != (cloudInitFile{unitPath(tc.id), "root:root", "0644", *want.Unit}) {
				t.Fatal("cloud-init unit differs from wantedNode")
			}
			command := config.RunCmd[0][2]
			if !strings.Contains(command, binaryInstallCommand(valueOr(tc.node.Binary, defaultBinary), version, "k3s", sum)) ||
				!strings.HasSuffix(command, configureNodeService(tc.id, want)) {
				t.Fatal("cloud-init command differs from the binary or node resource")
			}

			file := shell.Quote(unitFile.Path)
			read := "test -f " + file + " || exit 9; systemctl show " + shell.Quote(tc.id) +
				" --property=UnitFileState,ActiveState | tr '\\n' ' ' && stat -c '%a' " + file + " && cat " + file
			fake := remotetest.New().
				Once(read, remotetest.Reply{Stdout: tc.status + " " + unitFile.Permissions[1:] + "\n" + unitFile.Content}).
				Once(fileReadCommand(configFile.Path), remotetest.Reply{Stdout: configFile.Permissions[1:] + " root root\n" + configFile.Content})
			assertImport(t, tc.typ, tc.id, inputs, fake, 2)
		})
	}
}

func TestRenderCloudInitRunsPinnedInstall(t *testing.T) {
	const version = "v1.36.4+k3s1"
	const payload = "#!/bin/sh\necho 'k3s version " + version + "'\n"
	for _, tc := range []struct {
		name, userland, kernel, sum, mount, wantURL, wantError string
	}{
		{"Debian userland", "armhf", "aarch64", fmt.Sprintf("%x", sha256.Sum256([]byte(payload))), "",
			"https://github.com/k3s-io/k3s/releases/download/v1.36.4%2Bk3s1/k3s-armhf", ""},
		{"kernel fallback with bad checksum", "", "aarch64", strings.Repeat("0", 64), "",
			"https://github.com/k3s-io/k3s/releases/download/v1.36.4%2Bk3s1/k3s-arm64", "checksum"},
		{"missing checksum", "amd64", "x86_64", strings.Repeat("0", 64), "", "", "No checksum for k3s artifact k3s"},
		{"missing mount", "armhf", "aarch64", strings.Repeat("0", 64), "/mnt/storage", "", "k3s requires /mnt/storage to be mounted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "pinned k3s")
			scripts := map[string]string{
				"dpkg":       "echo " + shell.Quote(tc.userland),
				"uname":      "echo " + shell.Quote(tc.kernel),
				"curl":       "echo \"$4\" >> \"$K3S_TEST_LOG\"; printf '%s' " + shell.Quote(payload) + " > \"$3\"",
				"systemctl":  "echo \"$*\" >> \"$K3S_TEST_LOG\"",
				"mountpoint": "exit 1",
				"findmnt":    "exit 1",
			}
			for name, script := range scripts {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script+"\n"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			userData, err := cloudInit(RenderCloudInitArgs{
				Role: "server", Version: version,
				Checksums:     map[string]string{"k3s-armhf": tc.sum, "k3s-arm64": tc.sum},
				K3sServerArgs: K3sServerArgs{NodeArgs: NodeArgs{Binary: &path, RequiresMount: &tc.mount}},
			})
			if err != nil {
				t.Fatal(err)
			}
			args := parseCloudConfig(t, userData).RunCmd[0]
			cmd := exec.CommandContext(t.Context(), args[0], args[1:]...)
			log := filepath.Join(dir, "commands")
			cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "K3S_TEST_LOG="+log, "LC_ALL=C")
			out, err := cmd.CombinedOutput()
			trace, traceErr := os.ReadFile(log)
			if tc.wantError == "" {
				if err != nil || traceErr != nil || string(trace) != tc.wantURL+"\ndaemon-reload\nenable k3s\nrestart k3s\n" {
					t.Fatalf("boot command: %v, %s; trace %v, %s", err, out, traceErr, trace)
				}
				binary, err := os.ReadFile(path)
				if err != nil || string(binary) != payload {
					t.Fatalf("pinned binary was not installed: %v", err)
				}
			} else {
				if err == nil || !strings.Contains(string(out), tc.wantError) {
					t.Fatalf("boot command: error = %v, output = %s, want %q", err, out, tc.wantError)
				}
				if tc.wantURL != "" && (traceErr != nil || string(trace) != tc.wantURL+"\n") {
					t.Fatalf("expected only the download: %v, %s", traceErr, trace)
				}
				if tc.wantURL == "" && !os.IsNotExist(traceErr) {
					t.Fatalf("download or systemctl ran without a checksum: %v, %s", traceErr, trace)
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("binary appeared on failure: %v", err)
				}
			}
		})
	}
}
