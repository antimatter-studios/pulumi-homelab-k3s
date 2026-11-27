#!/usr/bin/env bash
# Regenerates the Go SDK from the plugin schema using Pulumi 3.150.0.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT
bin="$out/pulumi-resource-homelabk3s"
(cd "$root/provider" && go build -o "$bin" ./cmd/pulumi-resource-homelabk3s)
pulumi package gen-sdk "$bin" --language go --out "$out"
(
  cd "$out/go"
  go mod init github.com/antimatter-studios/pulumi-homelab-k3s/sdk/go
  go mod edit -go=1.26.6 -require=github.com/pulumi/pulumi/sdk/v3@v3.278.0
  go mod tidy
)
rm -rf "$root/sdk/go"
mkdir -p "$root/sdk"
mv "$out/go" "$root/sdk/go"
