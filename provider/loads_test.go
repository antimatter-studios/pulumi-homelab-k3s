package homelabk3s

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Load the package through the real Node ESM loader from outside it.
func TestSDKLoadsInNode(t *testing.T) {
	sdk, err := filepath.Abs("../sdk/nodejs")
	if err != nil {
		t.Fatal(err)
	}
	node, nodeErr := exec.LookPath("node")
	_, depsErr := os.Stat(filepath.Join(sdk, "node_modules/@pulumi/pulumi"))
	if nodeErr != nil || depsErr != nil {
		if os.Getenv("HOMELAB_K3S_REQUIRE_SDK") != "" {
			t.Fatalf("SDK loading needs Node and installed SDK dependencies: %v, %v", nodeErr, depsErr)
		}
		t.Skip("SDK loading needs Node and installed SDK dependencies")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	consumer := t.TempDir()
	scope := filepath.Join(consumer, "node_modules/@antimatter-studios")
	if err := os.MkdirAll(scope, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sdk, filepath.Join(scope, "homelab-k3s")); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, node, "--input-type=module", "-e",
		`import("@antimatter-studios/homelab-k3s").then(m => console.log(Object.keys(m).sort().join(",")))`)
	cmd.Dir = consumer
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("load SDK: %v\n%s", err, out)
	}
	exports := strings.Split(strings.TrimSpace(string(out)), ",")
	for _, name := range []string{
		"K3sBinary", "K3sServer", "K3sAgent", "NodeToken", "Kubeconfig",
		"renderCloudInit", "renderCloudInitOutput",
		"ContaboInstanceValidation", "ContaboInstanceReinstall", "CordonAndDrainBarrier", "LocalFileDelete", "LocalFileRotate",
	} {
		if !slices.Contains(exports, name) {
			t.Errorf("missing export %s in %v", name, exports)
		}
	}
}
