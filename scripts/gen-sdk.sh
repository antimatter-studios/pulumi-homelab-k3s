#!/usr/bin/env bash
# Regenerates the TypeScript SDK in sdk/nodejs and the Go SDK in sdk/go from the plugin's own schema. CI runs this and fails
# when the committed SDK differs, so run it after any change to the provider's resources or config.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
bin="$root/provider/bin/pulumi-resource-homelabk3s"
(cd "$root/provider" && go build -o "$bin" ./cmd/pulumi-resource-homelabk3s)
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT
pulumi package gen-sdk "$bin" --language nodejs --out "$out"
rm -rf "$root/sdk/nodejs"
mkdir -p "$root/sdk"
mv "$out/nodejs" "$root/sdk/nodejs"
# Drop the generator’s trailing blank line from the provider config source.
sed -i.bak '${/^$/d;}' "$root/sdk/nodejs/config/vars.ts"
# Consumers type-check these sources with their own compiler settings, and the generated
# `isInstance` overrides a base-class member without saying so, which `noImplicitOverride` rejects.
# Saying so changes nothing at runtime and is valid under every setting.
find "$root/sdk/nodejs" -name '*.ts' -exec sed -i.bak 's/public static isInstance(/public static override isInstance(/' {} +
find "$root/sdk/nodejs" -name '*.bak' -delete
# Consumers link this directory (`link:../pulumi-homelab-k3s/sdk/nodejs`) and load it from their
# programs, which Node runs with type stripping: that takes only erasable TypeScript, and the
# generated SDK is CommonJS-style TypeScript written to be compiled (require, exports,
# extensionless imports, <any> casts). So it is compiled here, with its own build script, and main
# points at the JavaScript; bin/ is committed so a link needs no build step.
node -e '
const f = process.argv[1], p = JSON.parse(require("fs").readFileSync(f, "utf8"));
// The generator leaves the version as the literal "${VERSION}", which the SDK then registers its
// provider with and Pulumi cannot resolve. The committed SDK is the development build, matched by
// the plugin consumers build from this checkout and install as 0.0.0-dev; a consumer that wants a
// release pins it with the version option of the provider resource.
p.version = "0.0.0-dev";
if (p.pulumi) p.pulumi.version = "0.0.0-dev";
// exact build tools, so a new TypeScript patch cannot change the committed output under CI check
p.devDependencies = { "@types/node": "14.18.63", "typescript": "4.9.5" };
p.main = "bin/index.js";
p.types = "bin/index.d.ts";
require("fs").writeFileSync(f, JSON.stringify(p, null, 4) + "\n");' "$root/sdk/nodejs/package.json"
(
  cd "$root/sdk/nodejs"
  npm install --no-audit --no-fund --no-package-lock --silent >/dev/null
  npx --no-install tsc
  # the compiled utilities read their version from ./package.json beside them, as in Pulumi's own
  # SDK builds, which publish bin/ as the package
  node -e '
const p = JSON.parse(require("fs").readFileSync("package.json", "utf8"));
p.main = "index.js"; p.types = "index.d.ts";
require("fs").writeFileSync("bin/package.json", JSON.stringify(p, null, 4) + "\n");'

)

# Test the compiled package the same way consumers load it while dependencies are installed.
(cd "$root/provider" && HOMELABK3S_REQUIRE_SDK=1 go test -count=1 -run '^TestSDKLoadsInNode$')
rm -rf "$root/sdk/nodejs/node_modules"

# The Go SDK, for the programs once they move to Go. Its module has its own go.mod, pinned to the
# Pulumi SDK the provider builds with.
pulumi package gen-sdk "$bin" --language go --out "$out"
(
  cd "$out/go"
  go mod init github.com/antimatter-studios/pulumi-homelab-k3s/sdk/go
  go mod edit -go=1.26.6 -require=github.com/pulumi/pulumi/sdk/v3@v3.278.0
  go mod tidy
)
rm -rf "$root/sdk/go"
mv "$out/go" "$root/sdk/go"
