// Package homelabk3s is the homelab k3s Pulumi provider: k3s on a machine you own, installed and
// read back over the same explicitly configured connection the homelab provider uses.
//
// Every resource token is homelabk3s:index:<Type>. The TypeScript dynamic providers under src/
// stay in service while the resources move here (pulumi-homelab-k3s#10, tracked on
// antimatter-studios/pulumi-homelab#31).
package homelabk3s

import (
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

// Name is the package name: the plugin binary is pulumi-resource-homelabk3s and every token
// starts with homelabk3s:.
const Name = "homelabk3s"

// Version is the provider's version. Release builds set it with
// -ldflags "-X github.com/antimatter-studios/pulumi-homelab-k3s/provider.Version=<version>".
var Version = "0.0.0-dev"

// Provider builds the provider the plugin serves.
func Provider() p.Provider {
	return infer.Provider(options())
}

func options() infer.Options {
	return infer.NewProviderBuilder().
		WithDisplayName("homelabk3s").
		WithDescription("k3s on a machine you own: install it, read back what is actually running.").
		WithNamespace("antimatter-studios").
		WithRepository("https://github.com/antimatter-studios/pulumi-homelab-k3s").
		WithLicense("MIT").
		WithKeywords("homelab", "k3s", "kubernetes", "category/infrastructure").
		BuildOptions()
}
