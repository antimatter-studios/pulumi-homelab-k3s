package homelabk3s

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/blang/semver"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
)

// The plugin starts and describes itself as the homelab-k3s package.
func TestSchemaNamesThePackage(t *testing.T) {
	server, err := integration.NewServer(t.Context(), Name, semver.MustParse("0.0.1"), integration.WithProvider(Provider()))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.GetSchema(p.GetSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(resp.Schema), &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Name != Name {
		t.Fatalf("schema name = %q, want %q", schema.Name, Name)
	}
}

// Every resource keeps a type alias from its token under the package's former name, so a stack
// whose state holds homelabk3s:index:<Type> resources adopts them instead of replacing them.
func TestSchemaAliasesTheFormerName(t *testing.T) {
	server, err := integration.NewServer(t.Context(), Name, semver.MustParse("0.0.1"), integration.WithProvider(Provider()))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.GetSchema(p.GetSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	type alias struct {
		Type string `json:"type"`
	}
	var schema struct {
		Resources map[string]struct {
			Aliases []alias `json:"aliases"`
		} `json:"resources"`
	}
	if err := json.Unmarshal([]byte(resp.Schema), &schema); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"ContaboInstanceReinstall", "ContaboInstanceValidation", "CordonAndDrainBarrier", "K3sAgent",
		"K3sBinary", "K3sServer", "Kubeconfig", "LocalFileDelete", "LocalFileRotate", "NodeToken",
	}
	var got []string
	for token, resource := range schema.Resources {
		name, ok := strings.CutPrefix(token, "homelab-k3s:index:")
		if !ok {
			t.Errorf("resource %s is outside homelab-k3s:index", token)
			continue
		}
		got = append(got, name)
		if !slices.Contains(resource.Aliases, alias{Type: "homelabk3s:index:" + name}) {
			t.Errorf("%s aliases = %v, want homelabk3s:index:%s", token, resource.Aliases, name)
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("resources = %v, want %v", got, want)
	}
}
