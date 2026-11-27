package homelabk3s

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/blang/semver"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

func vpsServer(t *testing.T, client *http.Client) integration.Server {
	t.Helper()
	server, err := integration.NewServer(t.Context(), Name, semver.MustParse("0.0.0-dev"),
		integration.WithProvider(providerWith(nil, client)))
	if err != nil {
		t.Fatal(err)
	}
	checked, err := server.CheckConfig(p.CheckRequest{})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("CheckConfig: %v, %v", checked.Failures, err)
	}
	if err := server.Configure(p.ConfigureRequest{Args: checked.Inputs}); err != nil {
		t.Fatal(err)
	}
	return server
}

func TestVPSIDOnlyImports(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cluster-config")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	instance := contaboResponse{method: "GET", url: contaboInstanceURL("42"), body: `{"data":[{"instanceId":42,"imageId":"running-image"}]}`}
	authRead := contaboResponses(t, testContaboAuth, instance, testContaboAuth, instance,
		contaboResponse{method: "GET", url: contaboAPIURL + "/compute/images", body: `{"data":[{"name":"Ubuntu","imageId":"requested-image"}]}`})
	t.Setenv("CONTABO_OAUTH2_CLIENT_ID", "client")
	t.Setenv("CONTABO_OAUTH2_CLIENT_SECRET", "secret")
	t.Setenv("CONTABO_OAUTH2_USER", "operator")
	t.Setenv("CONTABO_OAUTH2_PASS", "password")
	server := vpsServer(t, authRead)
	creds := property.NewMap(map[string]property.Value{
		"clientId": property.New("client"), "clientSecret": property.New("secret"),
		"user": property.New("operator"), "pass": property.New("password"),
		"instanceId": property.New("42"), "osImage": property.New("Ubuntu"),
	})
	for _, tc := range []struct {
		typ, id, observedKey, observedValue string
		inputs                              property.Map
		unknown                             []string
	}{
		{"LocalFileDelete", "del-" + path + "-1700000000000", "path", path,
			property.NewMap(map[string]property.Value{"path": property.New("cluster-config")}), []string{"deletedAt"}},
		{"LocalFileRotate", "rot-" + path + "-1700000000000", "path", path,
			property.NewMap(map[string]property.Value{"path": property.New("cluster-config")}), []string{"rotatedTo", "rotatedAt"}},
		{"CordonAndDrainBarrier", "drain-worker-east-1700000000000", "nodeName", "worker-east",
			property.NewMap(map[string]property.Value{"nodeName": property.New("worker-east"), "reinstallToken": property.New("cycle-2"),
				"kubeconfig": property.New(path), "timeoutSeconds": property.New(90.0)}),
			[]string{"reinstallToken", "kubeconfig", "timeoutSeconds", "completedAt"}},
		{"ContaboInstanceReinstall", "42-cycle-2", "reinstallToken", "cycle-2",
			creds.Set("reinstallToken", property.New("cycle-2")).Set("sshPublicKey", property.New("ssh-ed25519 key")),
			[]string{"clientId", "clientSecret", "user", "pass", "osImage", "sshPublicKey", "imageId", "sshKeySecretId", "reinstalledAt"}},
		{"ContaboInstanceValidation", "42", "instanceId", "42", creds,
			[]string{"clientId", "clientSecret", "user", "pass", "osImage", "resolvedImage"}},
	} {
		t.Run(tc.typ, func(t *testing.T) {
			urn := resourceURN(tc.typ)
			read, err := server.Read(p.ReadRequest{ID: tc.id, Urn: urn})
			if err != nil || read.ID != tc.id {
				t.Fatalf("Read %q: got %q, %v", tc.id, read.ID, err)
			}
			for _, properties := range []property.Map{read.Properties, read.Inputs} {
				if !properties.Get(tc.observedKey).Equals(property.New(tc.observedValue)) {
					t.Fatalf("import did not recover %s in %v", tc.observedKey, properties)
				}
				for _, key := range tc.unknown {
					if !properties.Get(key).IsNull() {
						t.Errorf("import filled unobservable %s", key)
					}
				}
			}
			if tc.typ == "ContaboInstanceValidation" {
				fp := read.Properties.Get("fingerprint")
				if !fp.IsMap() || !fp.AsMap().Get("imageId").Equals(property.New("running-image")) {
					t.Fatal("import did not read the instance fingerprint")
				}
			}
			diff, err := server.Diff(p.DiffRequest{ID: tc.id, Urn: urn, State: read.Properties, OldInputs: read.Inputs, Inputs: tc.inputs})
			if err != nil || diff.HasChanges || len(diff.DetailedDiff) != 0 {
				t.Fatalf("Diff after ID-only Read: %+v, %v", diff, err)
			}
			// After applying the matching declaration, the engine has recorded
			// inputs that could not be recovered by the import Read.
			if tc.typ == "CordonAndDrainBarrier" || tc.typ == "ContaboInstanceValidation" {
				key := "reinstallToken"
				if tc.typ == "ContaboInstanceValidation" {
					key = "osImage"
				}
				changed, err := server.Diff(p.DiffRequest{ID: tc.id, Urn: urn, State: read.Properties,
					OldInputs: tc.inputs, Inputs: tc.inputs.Set(key, property.New("changed"))})
				if err != nil || !changed.HasChanges {
					t.Fatalf("Diff after changing a recorded input: %+v, %v", changed, err)
				}
				if tc.typ == "CordonAndDrainBarrier" && changed.DetailedDiff[key].Kind != p.UpdateReplace {
					t.Fatalf("barrier token change must replace the resource: %+v", changed)
				}
			}
			if tc.typ == "ContaboInstanceValidation" {
				refreshed, err := server.Read(p.ReadRequest{ID: tc.id, Urn: urn, Inputs: tc.inputs, Properties: read.Properties})
				if err != nil {
					t.Fatal(err)
				}
				image := refreshed.Properties.Get("resolvedImage")
				if !image.IsMap() || !image.AsMap().Get("imageId").Equals(property.New("requested-image")) {
					t.Fatalf("refresh did not resolve the recorded image: %v", image)
				}
			}
			if err := server.Delete(p.DeleteRequest{ID: tc.id, Urn: urn, Properties: read.Properties}); err != nil {
				t.Fatalf("Delete after import: %v", err)
			}
		})
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "keep" {
		t.Fatalf("import changed local file: %q, %v", content, err)
	}
}
