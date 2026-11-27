// Package homelabk3s is the homelab k3s Pulumi provider: k3s on a machine you own, installed and
// read back over the same explicitly configured connection the homelab provider uses.
//
// Every resource token is homelab-k3s:index:<Type>, aliased to the homelabk3s:index:<Type> it had
// before the package took the repository's name. The TypeScript dynamic providers under src/
// remain available for stacks that have yet to switch by importing their live resources.
package homelabk3s

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/antimatter-studios/pulumi-homelab/provider/remote"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/pkg/v3/codegen/schema"
)

// Name is the package name: the plugin binary is pulumi-resource-homelab-k3s and every token
// starts with homelab-k3s:.
const Name = "homelab-k3s"

// FormerName is the package name before it matched the repository. Every resource keeps a type
// alias from its homelabk3s:index:<Type> token, so stacks created under the old name adopt their
// resources unchanged instead of replacing them.
const FormerName = "homelabk3s"

// Version is the provider's version. Release builds set it with
// -ldflags "-X github.com/antimatter-studios/pulumi-homelab-k3s/provider.Version=<version>".
var Version = "0.0.0-dev"

// PluginDownloadURL points to the GitHub repository that publishes the plugin assets.
const PluginDownloadURL = "github://api.github.com/antimatter-studios/pulumi-homelab-k3s"

// Provider builds the provider the plugin serves.
func Provider() p.Provider {
	return providerWith(nil, nil)
}

func providerWith(factory remote.Factory, contaboClient *http.Client) p.Provider {
	provider := infer.Provider(infer.NewProviderBuilder().
		WithConfig(infer.Config(&Config{factory: factory})).
		WithResources(infer.Resource(K3sServer{}), infer.Resource(K3sAgent{}),
			infer.Resource(K3sBinary{}), infer.Resource(NodeToken{}), infer.Resource(Kubeconfig{}),
			infer.Resource(LocalFileDelete{}), infer.Resource(LocalFileRotate{}), infer.Resource(CordonAndDrainBarrier{}),
			infer.Resource(ContaboInstanceValidation{api: contaboAPI{client: contaboClient}}),
			infer.Resource(ContaboInstanceReinstall{api: contaboAPI{client: contaboClient}})).
		WithDisplayName("homelab-k3s").
		WithDescription("k3s on a machine you own: install it, read back what is actually running.").
		WithNamespace("antimatter-studios").
		WithRepository("https://github.com/antimatter-studios/pulumi-homelab-k3s").
		WithLicense("MIT").
		WithPluginDownloadURL(PluginDownloadURL).
		WithLanguageMap(map[string]any{
			"nodejs": map[string]any{"packageName": "@antimatter-studios/homelab-k3s"},
			"go": map[string]any{
				"importBasePath":       "github.com/antimatter-studios/pulumi-homelab-k3s/sdk/go",
				"modulePath":           "github.com/antimatter-studios/pulumi-homelab-k3s/sdk/go",
				"rootPackageName":      "homelabk3s",
				"respectSchemaVersion": true,
			},
		}).
		WithKeywords("homelab", "k3s", "kubernetes", "category/infrastructure").
		BuildOptions())
	check := provider.CheckConfig
	provider.CheckConfig = func(ctx context.Context, req p.CheckRequest) (p.CheckResponse, error) {
		resp, err := check(ctx, req)
		if version, ok := req.Inputs.GetOk("version"); ok && err == nil {
			resp.Inputs = resp.Inputs.Set("version", version)
		}
		return resp, err
	}
	diff := provider.Diff
	provider.Diff = func(ctx context.Context, req p.DiffRequest) (p.DiffResponse, error) {
		if req.Urn.Type() == Name+":index:K3sBinary" {
			return diffBinary(req), nil
		}
		return diff(ctx, req)
	}
	return withFormerNameAliases(markSecretsOnRead(provider))
}

// withFormerNameAliases adds a type alias from FormerName to every resource in the schema. The
// infer annotator can only alias within the provider's own package, so the aliases are added to
// the finished schema. The generated SDKs pass them to the engine with every registration.
func withFormerNameAliases(provider p.Provider) p.Provider {
	getSchema := provider.GetSchema
	provider.GetSchema = func(ctx context.Context, req p.GetSchemaRequest) (p.GetSchemaResponse, error) {
		resp, err := getSchema(ctx, req)
		if err != nil {
			return resp, err
		}
		var spec schema.PackageSpec
		if err := json.Unmarshal([]byte(resp.Schema), &spec); err != nil {
			return resp, err
		}
		for token, resource := range spec.Resources {
			former := FormerName + strings.TrimPrefix(token, Name)
			if !slices.ContainsFunc(resource.Aliases, func(a schema.AliasSpec) bool { return a.Type == former }) {
				resource.Aliases = append(resource.Aliases, schema.AliasSpec{Type: former})
			}
			spec.Resources[token] = resource
		}
		out, err := json.Marshal(spec)
		if err != nil {
			return resp, err
		}
		resp.Schema = string(out)
		return resp, nil
	}
	return provider
}

// Read can return values that infer did not get from checked inputs.
func markSecretsOnRead(provider p.Provider) p.Provider {
	read := provider.Read
	provider.Read = func(ctx context.Context, req p.ReadRequest) (p.ReadResponse, error) {
		resp, err := read(ctx, req)
		if err != nil || req.Urn.Type() != Name+":index:ContaboInstanceReinstall" {
			return resp, err
		}
		if value, ok := resp.Properties.GetOk("userData"); ok {
			resp.Properties = resp.Properties.Set("userData", value.WithSecret(true))
		}
		if value, ok := resp.Inputs.GetOk("userData"); ok {
			resp.Inputs = resp.Inputs.Set("userData", value.WithSecret(true))
		}
		return resp, nil
	}
	return provider
}
