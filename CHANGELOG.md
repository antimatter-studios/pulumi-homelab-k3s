# Changelog

All notable changes to `pulumi-homelab-k3s`. Versions follow [semver](https://semver.org); the
format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## v0.1.0 — 2026-09-06

First release. Five resources, no `Cluster` type.

### Added

- **`K3sBinary`** — installs a pinned k3s release, verified against a SHA-256 written in your
  source, instead of piping a script from the internet into a shell. Reads back from
  `k3s --version`. Selects the release artifact from `dpkg --print-architecture` where it can, and
  `uname -m` otherwise, because on a Raspberry Pi the kernel and the userland disagree as standard.
  `delete` removes the binary and never `/var/lib/rancher/k3s`.
- **`K3sServer`** and **`K3sAgent`** — own `/etc/rancher/k3s/config.yaml` and the systemd unit as
  one resource, reading both back from `systemctl show`, the unit file and the config file. The
  unit keeps `Delegate=yes` and `KillMode=process`. `dataDir` moves containerd, the datastore and
  local-path volumes off an SD card together; `requiresMount` refuses to deploy onto a mount point
  whose disk is absent, and adds `RequiresMountsFor` plus `ConditionPathIsMountPoint` to the unit.
- **`NodeToken`** — reads back the join token a first server generated, following `dataDir`.
  Declared secret.
- **`Kubeconfig`** — waits for the kubeconfig file *and* for `kubectl wait --for=condition=Ready
  node --all`, then hands back the credentials repointed away from `127.0.0.1`. Declared secret.
- High availability as arguments rather than a type: `clusterInit` on the first server, `server`
  plus a shared token on the rest.
- `pnpm check` — serialises every provider, loads it back and runs it, because Pulumi evaluates a
  dynamic provider's closure from the state file and nothing else in the toolchain exercises that.

### Fixed before anyone could depend on it

- Relative imports carried no `.ts` extension, so Pulumi could not load the package at all while
  `tsc` and vitest both resolved it happily.
- Provider-local helpers named `fetch` silently resolved to the global on revival, breaking
  `NodeToken` and `Kubeconfig` against a real machine with `Failed to parse URL from [object
  Object]`.
- `armv8l` — what a 64-bit kernel calls a 32-bit userland — was refused as an unknown architecture.

### Licence

MIT.

### Known limitations

- `pulumi import` cannot adopt dynamic-provider resources; adoption is by convergence.
- Pointing joining nodes at a single server's address means losing that machine costs the ability to
  add nodes until it returns. A load balancer in front of the control plane is yours to provide.
