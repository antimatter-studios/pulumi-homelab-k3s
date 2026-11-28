package homelabk3s

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blang/semver"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

func vpsServer(t *testing.T, client *http.Client, config property.Map) integration.Server {
	t.Helper()
	server, err := integration.NewServer(t.Context(), Name, semver.MustParse("0.0.0-dev"),
		integration.WithProvider(providerWith(nil, client)))
	if err != nil {
		t.Fatal(err)
	}
	checked, err := server.CheckConfig(p.CheckRequest{Inputs: config})
	if err != nil || len(checked.Failures) > 0 {
		t.Fatalf("CheckConfig: %v, %v", checked.Failures, err)
	}
	for _, key := range contaboConfigKeys {
		if config.Get(key).IsString() && !checked.Inputs.Get(key).IsSecret() {
			t.Errorf("CheckConfig left %s unmarked as a secret", key)
		}
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
	server := vpsServer(t, nil, property.Map{})
	creds := contaboValidationInputs()
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
			diff, err := server.Diff(p.DiffRequest{ID: tc.id, Urn: urn, State: read.Properties, OldInputs: read.Inputs, Inputs: tc.inputs})
			if err != nil || diff.HasChanges || len(diff.DetailedDiff) != 0 {
				t.Fatalf("Diff after ID-only Read: %+v, %v", diff, err)
			}
			// After applying the matching declaration, the engine has recorded
			// inputs that could not be recovered by the import Read.
			if tc.typ == "CordonAndDrainBarrier" {
				changed, err := server.Diff(p.DiffRequest{ID: tc.id, Urn: urn, State: read.Properties,
					OldInputs: tc.inputs, Inputs: tc.inputs.Set("reinstallToken", property.New("changed"))})
				if err != nil || !changed.HasChanges || changed.DetailedDiff["reinstallToken"].Kind != p.UpdateReplace {
					t.Fatalf("changing a recorded barrier token must replace the resource: %+v, %v", changed, err)
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

func contaboValidationInputs() property.Map {
	return property.NewMap(map[string]property.Value{
		"clientId": property.New("client"), "clientSecret": property.New("secret"),
		"user": property.New("operator"), "pass": property.New("password"),
		"instanceId": property.New("42"), "osImage": property.New("Ubuntu"),
	})
}

func TestContaboValidationImportResolvesImageOnUpdate(t *testing.T) {
	cfgAuth := testContaboAuth
	cfgAuth.requestBody = "client_id=import-client&client_secret=import-secret&grant_type=password&password=import-password&username=import-user"
	client := contaboResponses(t, cfgAuth,
		contaboResponse{method: "GET", url: contaboInstanceURL("42"), body: `{"data":[{"instanceId":42,"imageId":"running-image"}]}`},
		testContaboAuth, contaboResponse{method: "GET", url: contaboAPIURL + "/compute/images",
			body: `{"data":[{"name":"Ubuntu","imageId":"requested-image"}]}`})
	config := property.NewMap(map[string]property.Value{
		"contaboClientId": property.New("import-client"), "contaboClientSecret": property.New("import-secret"),
		"contaboUser": property.New("import-user"), "contaboPass": property.New("import-password"),
	})
	server := vpsServer(t, client, config)
	urn := resourceURN("ContaboInstanceValidation")
	read, err := server.Read(p.ReadRequest{ID: "42", Urn: urn})
	if err != nil || read.ID != "42" || read.Inputs.Get("instanceId").AsString() != "42" {
		t.Fatalf("read imported instance: ID %q, %v", read.ID, err)
	}
	for _, properties := range []property.Map{read.Properties, read.Inputs} {
		for _, key := range []string{"clientId", "clientSecret", "user", "pass", "osImage", "resolvedImage"} {
			if !properties.Get(key).IsNull() {
				t.Errorf("import populated %s", key)
			}
		}
	}
	fp := read.Properties.Get("fingerprint")
	if !fp.IsMap() || !fp.AsMap().Get("imageId").Equals(property.New("running-image")) {
		t.Fatal("import did not record the instance fingerprint")
	}

	checked, err := server.Check(p.CheckRequest{Urn: urn, Inputs: contaboValidationInputs()})
	if err != nil || len(checked.Failures) != 0 {
		t.Fatalf("Check: %v, %v", checked.Failures, err)
	}
	diff, err := server.Diff(p.DiffRequest{ID: read.ID, Urn: urn, State: read.Properties, OldInputs: read.Inputs, Inputs: checked.Inputs})
	if err != nil || !diff.HasChanges || diff.DeleteBeforeReplace || len(diff.DetailedDiff) != 0 {
		t.Fatalf("missing image should produce an in-place update: %+v, %v", diff, err)
	}

	req := p.UpdateRequest{ID: read.ID, Urn: urn, State: read.Properties, OldInputs: read.Inputs, Inputs: checked.Inputs, DryRun: true}
	preview, err := server.Update(req)
	if err != nil || !preview.Properties.Get("resolvedImage").IsNull() {
		t.Fatalf("preview populated the unresolved image: %v", err)
	}
	req.DryRun = false
	// The mocked transport expects OAuth and an image GET, with no second
	// instance lookup, reinstall, or write to the Contabo API.
	updated, err := server.Update(req)
	if err != nil {
		t.Fatal(err)
	}
	image := updated.Properties.Get("resolvedImage")
	if !image.IsMap() || !image.AsMap().Get("imageId").Equals(property.New("requested-image")) {
		t.Fatalf("update did not resolve the declaration's image: %v", image)
	}
	if !updated.Properties.Get("fingerprint").Equals(fp) || !updated.Properties.Get("raw").Equals(read.Properties.Get("raw")) {
		t.Fatal("the follow-up update changed the observations from the import")
	}
	diff, err = server.Diff(p.DiffRequest{ID: read.ID, Urn: urn, State: updated.Properties, OldInputs: checked.Inputs, Inputs: checked.Inputs})
	if err != nil || diff.HasChanges {
		t.Fatalf("diff after resolving the image: %+v, %v", diff, err)
	}
}

func TestContaboImportRequiresConfiguredCredentials(t *testing.T) {
	t.Setenv("CONTABO_OAUTH2_CLIENT_ID", "client")
	t.Setenv("CONTABO_OAUTH2_CLIENT_SECRET", "secret")
	t.Setenv("CONTABO_OAUTH2_USER", "operator")
	t.Setenv("CONTABO_OAUTH2_PASS", "password")

	server := vpsServer(t, contaboResponses(t), property.Map{})
	_, err := server.Read(p.ReadRequest{ID: "42", Urn: resourceURN("ContaboInstanceValidation")})
	if err == nil || !strings.Contains(err.Error(), "missing required configuration homelab-k3s:contaboClientId") {
		t.Fatalf("import should name the missing provider setting even with credentials in the environment: %v", err)
	}
	for _, missing := range contaboConfigKeys {
		t.Run(missing, func(t *testing.T) {
			config := property.NewMap(nil)
			for _, key := range contaboConfigKeys {
				if key != missing {
					config = config.Set(key, property.New("configured"))
				}
			}
			want := "missing required configuration homelab-k3s:" + missing
			if _, err := server.CheckConfig(p.CheckRequest{Inputs: config}); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("CheckConfig should name the missing field: %v", err)
			}
			unconfigured := vpsServer(t, nil, property.Map{})
			if err := unconfigured.Configure(p.ConfigureRequest{Args: config}); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("Configure should name the missing field: %v", err)
			}
		})
	}
}

func TestContaboReinstallImportKeepsUserDataSecret(t *testing.T) {
	server := vpsServer(t, contaboResponses(t), property.Map{})
	const id = "42-cycle-2"
	urn := resourceURN("ContaboInstanceReinstall")
	data := property.New("#cloud-config\nnode-token: fixture")
	read, err := server.Read(p.ReadRequest{ID: id, Urn: urn})
	if err != nil || !read.Properties.Get("userData").IsNull() || !read.Inputs.Get("userData").IsNull() {
		t.Fatalf("ID-only import must leave userData absent: %v", err)
	}
	inputs := contaboValidationInputs().Set("reinstallToken", property.New("cycle-2")).
		Set("sshPublicKey", property.New("ssh-ed25519 key")).Set("userData", data)
	checked, err := server.Check(p.CheckRequest{Urn: urn, Inputs: inputs, State: read.Inputs})
	if err != nil || len(checked.Failures) != 0 || !checked.Inputs.Get("userData").Equals(data.WithSecret(true)) {
		t.Fatalf("the follow-up apply must checkpoint secret userData: %v, %v", checked.Failures, err)
	}
	diff, err := server.Diff(p.DiffRequest{ID: id, Urn: urn, State: read.Properties, OldInputs: read.Inputs, Inputs: checked.Inputs})
	if err != nil || diff.HasChanges {
		t.Fatalf("the follow-up apply must not reinstall: %+v, %v", diff, err)
	}

	// A read may be supplied plain values from an older checkpoint.
	read, err = server.Read(p.ReadRequest{ID: id, Urn: urn, Inputs: inputs, Properties: read.Properties.Set("userData", data)})
	if err != nil || !read.Inputs.Get("userData").Equals(data.WithSecret(true)) ||
		!read.Properties.Get("userData").Equals(data.WithSecret(true)) {
		t.Fatalf("Read must mark userData secret in inputs and outputs: %v", err)
	}
	updated, err := server.Update(p.UpdateRequest{ID: id, Urn: urn, State: read.Properties, OldInputs: read.Inputs, Inputs: checked.Inputs})
	if err != nil || !updated.Properties.Get("userData").Equals(data.WithSecret(true)) {
		t.Fatalf("Update must keep userData secret in outputs: %v", err)
	}
}
