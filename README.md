# pulumi-homelab-k3s

Pulumi resources for running k3s on a machine you own. Install it, configure it, join nodes to it,
and read back what is actually running.

Built on `pulumi-homelab` — the generic Linux-host provider this is split from — and it inherits
that project's one rule:

> **Every resource implements a real `read`.**

Not "this command ran once", but "here is what the machine says right now". `k3s --version`, the
systemd unit and the config file on disk are the truth; the state file is only what we last
believed.

---

## Why this exists

There is no k3s provider. Not in the Pulumi registry, and nothing on npm — searching it turns up
Testcontainers modules, a CDK construct and cloud-specific packages, none of which install k3s on a
machine you own. The nearest official thing, `@pulumi/kubernetes`, talks to a cluster that already
exists and has no opinion about how it got there.

One did exist: [`brumhard/pulumi-k3s`](https://github.com/brumhard/pulumi-k3s), Go, archived by its
author with a note explaining why:

> "This is just a toy provider... It is not maintained since I personally don't use k3s anymore. I
> now think this provider is **way too complicated** and something that could be achieved using the
> command provider."

Both halves of that are worth taking seriously, and they pull in opposite directions.

**He is right that k3s does not justify a big abstraction.** Installing k3s is a binary, a config
file, a unit and a token. It is not a subsystem. There is no `Cluster` type here with node pools and
a model of the control plane, and there is not going to be one — that is what "way too complicated"
meant, and it is what killed the project.

**He is wrong that the command provider is the answer.** `command.remote.Command` records that it
ran. Nothing afterwards can say which version is installed, whether the service is up, or whether
somebody replaced the binary by hand in March. `pulumi up --refresh` then compares your code against
Pulumi's *memory* of the machine rather than the machine, which is the one way to make all of this
pointless. He traded one problem for another and then stopped caring, because he stopped using k3s.

So: the gap is why nobody else has done this. The read-back rule is why it is worth doing.

---

## Requirements

- **Node 22 or newer.** The package is TypeScript with no build step — Pulumi loads it through
  Node's own type stripping, so every relative import here carries its `.ts` extension. Consumers
  need `"allowImportingTsExtensions": true` in their `tsconfig.json` (safe wherever nothing emits,
  which is true of any Pulumi program).
- **[`pulumi-homelab`](https://github.com/antimatter-studios/pulumi-homelab)**, for the ssh
  transport and two read helpers, checked out as a sibling directory and installed. See
  [What it depends on](#what-it-depends-on-and-what-that-costs).
- **ssh that already works from your terminal** — agent, `known_hosts`, `~/.ssh/config` and all. The
  transport shells out to `ssh` rather than using a library, so that it cannot disagree with your
  shell about whether a host is trusted.
- **Root on the target.** Every resource here is root from end to end. `pulumi-homelab`'s `Host`
  takes a `become` field: `sudo` (the default, passwordless), `none` for a host where you ssh in as
  root, or `{ password }` for sudo with a password sent down ssh's stdin.

Neither package is published to npm yet. Both are consumed as local dependencies:

```jsonc
// package.json
"dependencies": {
  "@pulumi/pulumi": "^3.140.0",
  "pulumi-homelab": "link:../pulumi-homelab",
  "pulumi-homelab-k3s": "link:../pulumi-homelab-k3s"
}
```

---

### What it depends on, and what that costs

Nine names, out of the thirty-odd `pulumi-homelab` exports:

```ts
ask, must, escalate, shellQuote, heredoc, Host   // the ssh transport
readFile, readUnit                               // two reads
mountedAt                                        // one check
```

The transport is worth sharing on its own — quoting that cannot be got subtly wrong, one decision
about how a host escalates to root, and the distinction between a command that failed and a question
that answered "no". But the reads are the stronger reason, and it is not convenience. `readUnit`
decides that `enabled-runtime` counts as enabled and that `activating` counts as started; `readFile`
decides that `stat`'s `644` is written `0644`. Those are answers about what a machine *is*, and two
copies of them eventually disagree — at which point this package and the host provider report
different things about the same unit and both are certain. One implementation of a normalisation is
not a saving, it is the whole point.

**What it costs:** `package.json` points at `link:../pulumi-homelab`, so a clone needs
[`antimatter-studios/pulumi-homelab`](https://github.com/antimatter-studios/pulumi-homelab) checked
out as a sibling directory and installed on its own before this package will install. Neither is on
npm yet. CI does exactly that in two `actions/checkout` steps, which is the working example if you
need one.

## Quick start — a single node

```ts
import * as pulumi from '@pulumi/pulumi';
import { K3sBinary, K3sServer, Kubeconfig } from 'pulumi-homelab-k3s';
import type { Host } from 'pulumi-homelab';

const host: Host = { address: '192.168.1.10', user: 'you' };
const token = new pulumi.Config().requireSecret('k3s-token');

const binary = new K3sBinary('k3s', host, {
  version: 'v1.36.4+k3s1',
  // from sha256sum-arm64.txt in that release, keyed by the artifact's own name
  checksums: { 'k3s-arm64': '2b1c9…' },
});

const server = new K3sServer('server', host, {
  clusterInit: true,               // embedded etcd, so a second server can be added later
  token,
  tlsSan: ['192.168.1.10'],        // every address the API certificate must cover
}, { dependsOn: [binary] });

export const kubeconfig = new Kubeconfig('kubeconfig', host, {
  server: '192.168.1.10',
}, { dependsOn: [server] }).config;
```

Always run with `--refresh`. A bare `pulumi up` compares your code against Pulumi's memory of the
machine rather than the machine itself.

---

## The resources

| Resource | What it owns | What its `read` asks the machine |
|---|---|---|
| `K3sBinary` | `/usr/local/bin/k3s` | `k3s --version` |
| `K3sServer` | `config.yaml` + `k3s.service` | `systemctl show`, the unit file, the config file |
| `K3sAgent` | `config.yaml` + `k3s-agent.service` | the same, for the agent unit |
| `NodeToken` | nothing — it reads | `<dataDir>/server/node-token` |
| `Kubeconfig` | nothing — it reads | `/etc/rancher/k3s/k3s.yaml`, repointed |

### `K3sBinary`

A pinned release, fetched and verified, rather than `curl -sfL https://get.k3s.io | sh -`.

```ts
new K3sBinary('k3s', host, {
  version: 'v1.36.4+k3s1',
  checksums: { 'k3s-arm64': '…', 'k3s': '…' },
  path: '/usr/local/bin/k3s',      // optional
});
```

| Argument | Default | Notes |
|---|---|---|
| `version` | required | The release tag exactly as k3s publishes it, `+k3s1` and all |
| `checksums` | required | SHA-256 per release **artifact name** (`k3s-arm64`, `k3s-armhf`, `k3s`) |
| `path` | `/usr/local/bin/k3s` | `/usr/local` because it is not the distribution's to manage |

Outputs: `version`, `path`, `artifact`.

- **Checksums are keyed by artifact, not architecture**, because that is how the release publishes
  them in `sha256sum-<arch>.txt` — so you can check what is written in your source against the
  release without translating first. A missing checksum for the artifact this machine needs is a
  hard error, never an unverified download.
- **The architecture comes from the userland, not the kernel.** `dpkg --print-architecture` is
  asked first and `uname -m` is the fallback. See [The kernel is not the
  userland](#the-kernel-is-not-the-userland) — this one is subtle and it matters on a Pi.
- **Upgrading restarts the service.** A new binary on disk changes nothing about the process already
  running from the old one, and the unit has not changed so systemd has no reason to act.
- **`delete` removes the binary only.** `/var/lib/rancher/k3s` is every workload, secret and volume
  on the node. Tearing a cluster down is a deliberate act, not a side effect of `pulumi destroy`.

### `K3sServer` and `K3sAgent`

The config file and the unit as one resource, because they are one thought: a config without a unit
is a machine that is configured and does nothing, and a unit without the config is a node that joins
nothing — or starts a second cluster of its own.

```ts
new K3sServer('server', host, {
  clusterInit: true,
  token,
  tlsSan: ['192.168.1.10', 'pi.local'],
  disable: ['traefik'],                       // k3s ships traefik + servicelb by default
  dataDir: '/mnt/storage/k3s',
  requiresMount: '/mnt/storage',
  kubeletArg: ['root-dir=/mnt/storage/k3s/kubelet'],
});

new K3sAgent('worker', workerHost, {
  server: 'https://192.168.1.10:6443',
  token,
});
```

| Argument | Applies to | Default | Notes |
|---|---|---|---|
| `token` | both | k3s invents one | Set it: every node then knows how to join before the first exists |
| `clusterInit` | server | `false` | Embedded etcd. Required before a second server can ever be added |
| `server` | both | — | `https://host:6443` — join an existing cluster instead of starting one |
| `tlsSan` | server | — | Every name the API certificate must cover |
| `disable` | server | — | `traefik`, `servicelb`, `local-storage` |
| `dataDir` | both | `/var/lib/rancher/k3s` | Moves containerd, the datastore and local-path volumes together |
| `requiresMount` | both | — | A mount point the data directory needs. **Read the section below** |
| `kubeletArg` | both | — | The kubelet does not follow `dataDir`; `root-dir=…` moves it |
| `snapshotter` | both | k3s's default (overlayfs) | Only set `btrfs` deliberately; it needs a subvolume |
| `nodeName`, `nodeLabel`, `nodeTaint` | both | — | Passed through |
| `extra` | both | — | Anything else k3s accepts in `config.yaml`, keys sorted |
| `binary` | both | `/usr/local/bin/k3s` | Where `K3sBinary` put it |
| `enabled`, `started` | both | `true` | systemd |

Outputs: `config` (secret — it contains the token), `unit`, `enabled`, `started`.

Three arguments are rejected rather than rendered into a broken node: a server told to both
`clusterInit` and `server`, any node joining with no `token`, and an agent with no `server`.

The unit is k3s's own with two lines that matter kept:

- **`Delegate=yes`** hands the cgroup subtree to containerd. Without it, systemd and containerd both
  believe they own it and containers are killed for reasons neither logs anywhere useful.
- **`KillMode=process`** stops a k3s restart taking every running container with it — the difference
  between upgrading the binary and an outage.

### `NodeToken`

Reads back the token a first server generated, for the nodes that come after it.

```ts
const joining = new NodeToken('token', firstHost, { dataDir: '/mnt/storage/k3s' });
new K3sAgent('worker', workerHost, { server: 'https://…:6443', token: joining.token });
```

Only needed when the token was not set in the source. Setting it there is better: the cluster is
then reproducible, and rebuilding it from scratch gives the same token rather than a new one nothing
else has been told about. Output `token` is a secret. `dataDir` must match the server's, or this
waits for a file that will never exist on a machine that is running perfectly well.

### `Kubeconfig`

The admin credentials, fetched once the cluster is actually answering.

```ts
const kubeconfig = new Kubeconfig('kubeconfig', host, {
  server: '192.168.1.10',
  readySeconds: 300,
});
```

- It **waits for two conditions**: the file existing, and `kubectl wait --for=condition=Ready node
  --all`. The file appears before the node can schedule anything, and handing out a kubeconfig for a
  cluster that cannot schedule lets the next stack start and fail confusingly.
- The waiting happens **on the node in one ssh session**, not as a polling loop from your laptop —
  one handshake instead of dozens, and `kubectl wait` watches the API rather than guessing from a
  sleep.
- k3s writes the server as `https://127.0.0.1:6443`, which is correct on the node and useless
  anywhere else. It comes back repointed at whatever you pass as `server` — **which must be covered
  by the server's `tlsSan`**, or every connection fails with a certificate error rather than an
  address error.
- `config` is declared secret inside the resource, not at the call site. It is a cluster-admin
  certificate and key. Treat it as a root password, because it is.

---

## High availability

There is no type for this. HA is `clusterInit` on the first server, `server` plus a shared token on
the others, and three of them being a control plane that survives losing one.

```ts
new K3sServer('server-1', first,  { clusterInit: true, token, tlsSan });
new K3sServer('server-2', second, { server: 'https://192.168.1.10:6443', token, tlsSan });
new K3sServer('server-3', third,  { server: 'https://192.168.1.10:6443', token, tlsSan });
new K3sAgent('worker-1', fourth,  { server: 'https://192.168.1.10:6443', token });
```

Pointing the joining nodes at one server's address means losing that machine costs you the ability
to *add* nodes until it returns, though not the running cluster. A load balancer or a floating
address in front of the control plane is the fix, and it is yours to provide.

**`clusterInit` on an SD card is the wrong default.** Turning it on later means migrating the
datastore of a running cluster, which is a real cost and a good argument for setting it early. The
counter-argument wins on a Pi: embedded etcd fsyncs constantly, and on a Pi it is fsyncing onto the
slowest and least durable storage in the house. Buying that write amplification against a second
server nobody has ordered is the worse trade. Whichever way you go, write down why — a single-node
cluster recreated with `clusterInit` the week a second machine arrives is a decision; one where
nobody remembers the question is an afternoon migrating etcd.

---

## Moving the data off the SD card

Most of what kills a Pi's SD card is k3s: containerd unpacking images, the datastore's constant
small fsyncs, and every local-path volume. `dataDir` moves all three at once.

```ts
new K3sServer('server', host, {
  dataDir: '/mnt/storage/k3s',
  requiresMount: '/mnt/storage',
  kubeletArg: ['root-dir=/mnt/storage/k3s/kubelet'],
});
```

**`requiresMount` is the important one, and it does two jobs because there are two ways this goes
wrong.**

**At boot** it becomes `RequiresMountsFor` and `ConditionPathIsMountPoint` in the unit. The first
pulls in the mount unit and orders after it, so a disk that is late or fails to mount stops k3s. The
second covers what the dependency cannot see — a path that exists and simply is not a mount point,
because somebody unmounted the array by hand and there is no failing mount unit to wait on. A failed
condition *skips* the unit rather than failing it, which is what you want when the alternative to
not starting is starting empty.

**At deployment** it is checked before a single byte is written, which `RequiresMountsFor` cannot
cover because a deployment runs over ssh long after boot. With the disk absent, `/mnt/storage` is an
ordinary empty directory: `mkdir -p` creates the data directory on the root filesystem, k3s starts,
finds nothing, decides it is a new node and builds a brand new empty cluster. **Nothing in that
sequence returns an error.** Worse, mounting the real disk afterwards hides the files the deployment
just wrote, so the same path has two sets of contents depending on whether the array is there — and
the workloads look deleted while sitting intact underneath.

The check is `mountpoint -q`, not `test -d`. An unmounted mount point *is* an existing directory, so
every check based on the directory existing passes on precisely the broken machine.

---

## Adopting a cluster that is already running

**`pulumi import` does not work for any of this.** Dynamic-provider resources cannot be imported —
both the CLI and the resource-level `import` option fail inside Pulumi's own dynamic-provider
service. Adoption is therefore by convergence: declare exactly what is already on the machine and
let `create` write content identical to what is there.

That puts all the weight on the declaration being byte-exact, so read the machine first —

```bash
cat /etc/rancher/k3s/config.yaml
systemctl cat k3s
```

— and write the arguments to match, rather than writing what you would have chosen and finding out
the difference on a live cluster. Expect a real diff against a cluster installed by k3s's own script:
it passes flags as `ExecStart` arguments where this writes them into the config file. That diff means
a restart, which is survivable (`KillMode=process` keeps the containers up), but make sure the config
file says everything the old `ExecStart` said first.

---

## Raspberry Pi notes

**Cgroups.** k3s will not start without `cgroup_memory=1 cgroup_enable=memory` in
`/boot/firmware/cmdline.txt` (older images: `/boot/cmdline.txt`), and that needs a reboot. Without
the memory controller, k3s fails part way up as a container runtime error that reads like a k3s bug.
Nothing here reboots your machine — a reboot mid-deployment kills the ssh connection and leaves
Pulumi unable to say what it finished. `pulumi-homelab` has both halves:

```ts
import { KernelCmdline, Precondition, checkCommand } from 'pulumi-homelab';

const cmdline = new KernelCmdline('cgroups', host, {
  flags: ['cgroup_memory=1', 'cgroup_enable=memory'],
});

const booted = new Precondition('cgroups-active', host, {
  // Not a check of the boot line. A Pi 5's firmware puts its own parameters first, so a real
  // machine carries cgroup_disable=memory from the firmware AND cgroup_enable=memory from
  // cmdline.txt on the same line. The kernel takes the later one and is correct — but the same two
  // in the other order would satisfy a command-line check on a machine where the controller is off.
  check: checkCommand('grep -qw memory /sys/fs/cgroup/cgroup.controllers'),
  message: 'this Pi has not booted with the memory cgroup controller. `sudo reboot`, then deploy again.',
}, { dependsOn: [cmdline] });

new K3sBinary('k3s', host, { … }, { dependsOn: [booted] });
```

Declaring that pair is left to you rather than hidden inside `K3sServer`: a dynamic resource cannot
own another resource, and a gate you cannot see in the graph is one nobody knows they depend on.

### The kernel is not the userland

`uname -m` reports the kernel's architecture. The binary runs in the userland, and on a Pi those
disagree as standard rather than as an exotic case: Raspberry Pi OS 32-bit ships a **64-bit kernel**
by default on a Pi 4, and necessarily on a Pi 5, whose A76 has no aarch32 at EL1 at all.

Worse, `uname -m` has no single answer there. A compat process is told whatever the kernel's
`COMPAT_UTS_MACHINE` holds — `armv8l` on an arm64 kernel, where the same userland under an armhf
kernel is told `armv7l` — and a process that requests the `PER_LINUX` personality is told `aarch64`.
One machine, three answers, none of them the question you are asking. `dpkg --print-architecture`
answers the one that decides whether a binary will run, so it is asked first.

### Other things worth knowing on a Pi

- **Swap.** Pi OS enables `dphys-swapfile`. Turn it off rather than moving it — under memory pressure
  it is the fastest way to destroy the card.
- **Journald.** Leave it volatile. It starts long before a separate disk mounts, and with the mount
  guards above a missing array means k3s deliberately does not start — putting the explanation of
  that on the array makes the one failure this guards against also the one it cannot explain.
- **Memory.** The control plane alone is 500 MB–1 GB. A 1 GB Pi is not viable, 2 GB is tight.
- **Charts that ship CRDs and controllers together** need `skipAwait: true`. The controllers exit at
  startup because their own definitions are not registered yet, and waiting for readiness deadlocks
  against the creation that would satisfy it. The error names an image, not an ordering problem.

---

## Verifying a change

```bash
pnpm verify        # all three
pnpm typecheck     # tsc --noEmit
pnpm test          # vitest, including a real Node import of the package
pnpm check         # every provider serialised, reloaded, and run
```

`pnpm check` is the one that is not obvious, and it exists because two of these resources shipped
broken while everything else was green.

Pulumi does something to a dynamic provider that nothing else does: it **serialises the entire
provider closure into the state file and evaluates it again somewhere else**. A provider can
typecheck, pass every test and import cleanly, and still be impossible to serialise — or serialise
into text whose bindings resolve to something different when it runs. So the check writes each
provider out, loads it back, and invokes it against an address that cannot answer: reaching ssh at
all is the proof that everything on the way there survived the trip.

Two details that took a real outage to learn:

- **Serialise the object, not the factory.** Pulumi gets `super(providerFor(host), …)`, so the
  factory has already run and the *object* is what is captured. Serialising `() => providerFor(host)`
  puts the factory body in the state file, where its locals are redeclared on revival and everything
  works — which is the shape that cannot fail, and the shape this check was mistakenly using.
- **Never name a provider-local after a global.** A helper called `fetch`, declared inside the
  provider factory, does not survive revival — and losing the binding raises nothing, because the
  name resolves to the global instead. The symptom was `Failed to parse URL from [object Object]`,
  which names neither the helper nor the resource. There is a check that scans for this.

---

### Hooks

The guards that keep `main` linear and squash-only are not in the tree. They are installed per clone,
into `.git/hooks`, by [github-guard](https://github.com/antimatter-studios/agent-skills):

```sh
~/.claude/skills/github-guard/install.sh .
```

**Do not point `core.hooksPath` at a directory inside the working tree**, tempting as it is for
making hooks travel with a repository. Git resolves a hook's path at the moment it runs the hook, and
for a checkout that is *after* the working tree has been replaced — so an in-tree hooks directory
lets any branch you check out rewrite the hook that runs next, and it then runs as you, with your
credentials. On a public repository, reviewing somebody's pull request locally is enough.

The one tracked file is `.github-guard/required-checks`, which is data rather than code, and which
`github-protect-main` reads from the default branch **on the server** rather than from the checkout.
That asymmetry is the point: it is what stops an untrusted branch stripping the required checks the
moment you commit while it is checked out.

## A pattern worth keeping

Six times in building this, the same bug appeared: **the question that is cheap to ask is not the
question you care about.** It is usually right, which is exactly what makes the exception expensive.

| Cheap to ask | What you actually need |
|---|---|
| `cmdline.txt` | `/sys/fs/cgroup/cgroup.controllers` |
| the drop-in you wrote | `systemd-analyze cat-config` |
| the mount unit | `mountpoint -q` |
| `swapoff` ran | `/proc/swaps` |
| `uname -m` | `dpkg --print-architecture` |
| `storageClassName: ""` | absent — `""` means *explicitly no class*, not the default |

The corollary, which cost two bugs on its own: **do not assert state that nothing reads back.** A
mode written on every deployment and never checked cannot appear as drift, and it will quietly fight
the first resource that does read it.

---

## Changelog

Full history in [CHANGELOG.md](CHANGELOG.md).

### v0.1.0

First release: `K3sBinary`, `K3sServer`, `K3sAgent`, `NodeToken` and `Kubeconfig`, each reading its
state back from the machine. HA as arguments rather than a type. Data directory and mount guards for
running the cluster off an SD card.

## Status

Five resources, 44 tests and 8 package checks. Typecheck clean under `strict` and
`noUncheckedIndexedAccess`.

In use on a Raspberry Pi 5 with real workloads on it. All five resources adopted an existing k3s
installation in place — `K3sBinary` pinned at `v1.36.3+k3s1`, `K3sServer` with its data directory on
an NVMe array and `requiresMount` guarding it, `NodeToken` and `Kubeconfig` reading back from that
same directory — and `pulumi up --refresh` reports no drift across the 118 resources of the stack it
is part of. Flux, Immich and Netdata run above it.

The mount guarding has been exercised rather than merely reasoned about: the machine lost power
twice unexpectedly, and on both occasions the array mounted before k3s started. That is what
`RequiresMountsFor` and `ConditionPathIsMountPoint` are in the unit for, and the failure they
prevent — k3s starting against an empty data directory and building a new cluster on top of the
mount point of the real one — reports no error when it happens.

CI runs `pnpm verify` on every pull request, and `CI` is a required check on `main`. It checks out
`pulumi-homelab` alongside this repository so the `link:` dependency resolves the way it does on a
laptop — and installs that sibling separately, because a `link:` links a directory and does not
install what the directory depends on. That distinction cost the first red build.

## Licence

MIT — see [LICENSE](LICENSE).
