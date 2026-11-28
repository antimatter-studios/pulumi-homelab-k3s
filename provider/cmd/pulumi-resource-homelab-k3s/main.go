// Command pulumi-resource-homelab-k3s is the homelab k3s provider plugin. Pulumi starts it by name;
// it serves the provider over gRPC on the port it prints, and exits when the engine is done with it.
package main

import (
	"context"
	"fmt"
	"os"

	homelabk3s "github.com/antimatter-studios/pulumi-homelab-k3s/provider"
)

func main() {
	if err := homelabk3s.Provider().Run(context.Background(), homelabk3s.Name, homelabk3s.Version); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}
