// Package homelabk3s is the homelab k3s Pulumi provider: k3s on a machine you own, installed and
// read back over the same explicitly configured connection the homelab provider uses.
//
// Every resource token is homelabk3s:index:<Type>. The TypeScript dynamic providers under src/
// remain available for stacks that have yet to switch by importing their live resources.
package homelabk3s

import (
	"context"

	"github.com/antimatter-studios/pulumi-homelab/provider/remote"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

// Name is the package name: the plugin binary is pulumi-resource-homelabk3s and every token
// starts with homelabk3s:.
const Name = "homelabk3s"

// Version is the provider's version. Release builds set it with
// -ldflags "-X github.com/antimatter-studios/pulumi-homelab-k3s/provider.Version=<version>".
var Version = "0.0.0-dev"

// PluginDownloadURL points to the GitHub repository that publishes the plugin assets.
const PluginDownloadURL = "github://api.github.com/antimatter-studios/pulumi-homelab-k3s"

// Provider builds the provider the plugin serves.
func Provider() p.Provider {
	return providerWith(nil)
}

func providerWith(factory remote.Factory) p.Provider {
	provider := infer.Provider(infer.NewProviderBuilder().
		WithConfig(infer.Config(&Config{factory: factory})).
		WithResources(infer.Resource(K3sServer{}), infer.Resource(K3sAgent{}),
			infer.Resource(K3sBinary{}), infer.Resource(NodeToken{}), infer.Resource(Kubeconfig{})).
		WithLanguageMap(map[string]any{"nodejs": map[string]any{"packageName": "@antimatter-studios/homelab-k3s"}}).
		WithDisplayName("homelabk3s").
		WithDescription("k3s on a machine you own: install it, read back what is actually running.").
		WithNamespace("antimatter-studios").
		WithRepository("https://github.com/antimatter-studios/pulumi-homelab-k3s").
		WithLicense("MIT").
		WithPluginDownloadURL(PluginDownloadURL).
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
	return provider
}
