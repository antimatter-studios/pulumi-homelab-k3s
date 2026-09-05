# pulumi-homelab-k3s

Pulumi resources for running k3s on a machine you own. Install it, and read back what is actually
running.

Built on [`pulumi-homelab`](../pulumi-homelab), and it inherits that project's one rule:

> **Every resource implements a real `read`.**

Not "this command ran once", but "here is what the machine says right now". `k3s --version` and the
systemd unit are the truth; the state file is only what we last believed.

## Why this is separate from `pulumi-homelab`

`pulumi-homelab` is about generic Linux state — files, packages, services, users — and is finished
when a machine is configured. k3s is one specific piece of software with its own release cadence,
its own upgrade semantics and its own way of being wrong. Keeping it apart means the host provider
can be used without dragging in Kubernetes opinions, and that k3s's versioning does not become the
host provider's versioning.

## Read this before writing a line

There was a native Pulumi k3s provider: [`brumhard/pulumi-k3s`](https://github.com/brumhard/pulumi-k3s),
Go, 26 stars. It is **archived**, and the author left a note saying why:

> "This is just a toy provider... It is not maintained since I personally don't use k3s anymore.
> I now think this provider is **way too complicated** and something that could be achieved using
> the command provider."

Real work on it stopped in 2022; the archive notice went up in 2024. Two lessons, and they pull in
opposite directions, which is the point:

**He is right that k3s does not justify a big abstraction.** Installing k3s is a binary, a unit file
and a token. It is not a subsystem. Do not build a `Cluster` type with node pools, topology and a
model of the control plane — that is what "way too complicated" meant, and it is what killed it.

**He is wrong that the command provider is the answer.** A command records that it ran and knows
nothing afterwards; nothing could then say which version is installed or whether the service is
even up. He traded one problem for another and stopped caring, because he stopped using k3s.

The thing that predicts whether a project like this survives is whether its author uses it. Build
it for the machine in the next room. Publish only if it earns it. Do not design for imagined users.

## What exists elsewhere

- No k3s package in the Pulumi registry, and none on npm.
- [`QC-Labs/orange-lab`](https://github.com/QC-Labs/orange-lab) — K3s, Tailscale and Longhorn on
  consumer hardware. Actively developed. A Pulumi *program*, not a provider, so it is something to
  read and borrow from rather than depend on. It has almost certainly already solved the problems
  below.
- [`Ashpex/homelab`](https://github.com/Ashpex/homelab) — k3s, flux and Pulumi. Also active.

That everybody solves this by hand in their own repo is the gap. It is also the warning.

## The problems this has to solve

1. **Installing without `curl | sh`.** The official path pipes a script from the internet into a
   shell, which leaves nothing readable behind. Prefer a pinned release binary with a verified
   checksum, so the version installed is a fact rather than a memory.
2. **Cgroups on Raspberry Pi OS.** k3s will not start without `cgroup_memory=1 cgroup_enable=memory`
   in `/boot/firmware/cmdline.txt` (older images: `/boot/cmdline.txt`). This needs a reboot, which
   is awkward to model — the SSH connection dies mid-deployment and re-running has to be safe.
   Failing loudly with an instruction may be more honest than rebooting the machine underneath a
   running deployment.
3. **The kubeconfig.** k3s writes `/etc/rancher/k3s/k3s.yaml` with the server as `127.0.0.1`, which
   is useless from anywhere else. It has to come back rewritten to the machine's address, and it is
   a credential — so it belongs in Pulumi's secret handling, not in a plain output.
4. **Knowing it is actually up.** Installed, enabled and running are three different things, and a
   node that is `Ready` is a fourth.

## Resources

| Resource | Reads state from |
|---|---|
| `K3sBinary` | `k3s --version` |
| `K3sServer` | `systemctl show`, the unit file and `/etc/rancher/k3s/config.yaml` |
| `K3sAgent` | the same, for `k3s-agent` |
| `NodeToken` | `/var/lib/rancher/k3s/server/node-token` |
| `Kubeconfig` | `/etc/rancher/k3s/k3s.yaml`, repointed away from `127.0.0.1` |

There is no `Cluster` type and there is not going to be one. High availability here is
`clusterInit` on the first server, `server` and a shared token on the others, and three of them
being a control plane that survives losing one.

## Using it

```ts
import * as pulumi from '@pulumi/pulumi';
import { K3sBinary, K3sServer, K3sAgent, Kubeconfig } from 'pulumi-homelab-k3s';
import type { Host } from 'pulumi-homelab';

const first: Host = { address: '192.168.0.47', user: 'chris' };
const token = new pulumi.Config().requireSecret('k3s-token');

new K3sBinary('k3s', first, {
  version: 'v1.36.4+k3s1',
  // from sha256sum-arm64.txt in that release, keyed by the artifact's own name
  checksums: { 'k3s-arm64': '…' },
});

const server = new K3sServer('server', first, {
  clusterInit: true,          // embedded etcd, so a second server can be added later
  token,
  tlsSan: ['192.168.0.47'],   // every address the API certificate has to cover
});

const kubeconfig = new Kubeconfig('kubeconfig', first, { server: '192.168.0.47' });
```

A second control-plane node is the same thing pointed at the first:

```ts
new K3sServer('server-2', second, { server: 'https://192.168.0.47:6443', token, tlsSan: [...] });
new K3sAgent('worker-1', third, { server: 'https://192.168.0.47:6443', token });
```

Set the token yourself rather than letting k3s invent one: every node then knows how to join before
the first one exists. `NodeToken` reads back the generated one for a cluster that already ran
without.

Always run with `--refresh`. A bare `pulumi up` compares your code against Pulumi's *memory* of the
machine rather than the machine itself, which is the one way to make all of this pointless.

### Moving the data off the SD card

The write load is what kills a Pi's SD card, and most of it is k3s: containerd's image store, the
datastore's constant small fsyncs, and every local-path volume. All three move together:

```ts
new K3sServer('server', host, {
  dataDir: '/mnt/storage/k3s',
  kubeletArg: ['root-dir=/mnt/storage/k3s/kubelet'],
});
```

```ts
new K3sServer('server', host, {
  dataDir: '/mnt/storage/k3s',
  requiresMount: '/mnt/storage',
  kubeletArg: ['root-dir=/mnt/storage/k3s/kubelet'],
});
```

`requiresMount` is the one that matters, and it does two separate jobs because there are two
separate ways this goes wrong.

**At boot**, it becomes `RequiresMountsFor` and `ConditionPathIsMountPoint` in the unit — two lines for two failures. The first pulls in the mount unit and orders after it, so a disk that is late or fails to mount stops k3s. The second covers what the dependency cannot see: a path that exists and is not a mount point, because somebody unmounted the array by hand and there is no failing mount unit to wait on. A failed condition skips the unit rather than failing it, which is what you want when the alternative to not starting is starting empty. An fstab entry for a separate disk should carry `nofail`, so that a missing or late disk does
not hold up the boot — and that is exactly what lets k3s start before the disk is mounted. It then
finds an empty data directory, concludes it is a new node, and builds a second, empty cluster on top
of the mount point of the real one. Nothing fails; the first symptom is that every workload has
vanished. `RequiresMountsFor` means k3s either sees the real data or does not start.

**At deployment**, it is checked before a single byte is written. This is the half that
`RequiresMountsFor` cannot cover: with the disk unmounted, `mkdir -p` creates the data directory on
the root filesystem, and now the mount point is not empty either — mounting the real disk over it
hides what was just written, and k3s has meanwhile been started against a data directory with
nothing in it. So the deployment stops with the reason instead, and the disk underneath is untouched.
An unmounted mount point is still an existing directory, which is why the check is `mountpoint -q`
and not `test -d`: every test based on the directory existing passes on the broken machine.

The same question is `mountedAt(path)` in `pulumi-homelab`, if you would rather have it as a
`Precondition` in the graph as well, where the rest of the stack can depend on it:

```ts
new Precondition('storage-mounted', host, {
  check: mountedAt('/mnt/storage'),
  message: '/mnt/storage is not mounted. k3s data lives there; mount it before deploying.',
});
```

The kubelet keeps its own state and does not follow `dataDir`, which is why `kubeletArg` is there:
move one without the other and pod state is still being written to the card you were sparing.

`NodeToken` takes `dataDir` too — the token lives inside the data directory, so on a moved node the
default path is a file that will never exist.

### Adopting a cluster that is already running

The resources here own `/etc/rancher/k3s/config.yaml` and the unit, and a first `pulumi up` against
a machine that already runs k3s will rewrite both and restart the service. On a cluster with real
workloads on it, do it the other way round: `pulumi import` the resources, read what comes back —
`read` returns the machine's actual config and unit — and adjust the arguments until `pulumi
preview` shows no diff. Only then is the code describing the machine rather than replacing it.

Two differences to expect against a cluster installed by k3s's own script: it passes flags as
`ExecStart` arguments where this writes them into the config file, and its unit is not byte-identical
to the one here. Both are real diffs and both mean a restart, so make sure the config file says
everything the old `ExecStart` said before you apply one.

### Three things that will bite

**`clusterInit` on an SD card.** Turning it on later means migrating the datastore of a running
cluster, which is a real cost and an argument for setting it on the first server even when there is
only one machine. The counter-argument wins on a Pi: embedded etcd fsyncs constantly, and on a Pi
the thing being fsynced onto is the slowest and least durable storage in the house. Buying that
write amplification now, against a second server nobody has ordered, is the worse trade. So:
`clusterInit` is the right default on real disks and the wrong one on SD cards. If you leave it off,
write down *why* next to the code — a single-node cluster that is recreated with `clusterInit` the
week a second machine arrives is a decision; one that was never considered is an afternoon spent
migrating etcd.

**`tls-san` and the kubeconfig address.** The kubeconfig comes back pointed at whatever `server` you
give it, and that address only works if the API certificate covers it — which means the same address
has to be in the server's `tlsSan`. It does not fail as "wrong address"; it fails as a certificate
error, which is a much longer afternoon.

**Cgroups on Raspberry Pi OS.** k3s will not start without `cgroup_memory=1 cgroup_enable=memory` in
`/boot/firmware/cmdline.txt`, and that needs a reboot. Without the memory controller k3s fails part
way up as a container runtime error, which reads as a k3s problem rather than a kernel one — so the
gate belongs in front of it. Nothing here reboots your machine: a reboot mid-deployment kills the ssh
connection and leaves Pulumi unable to say what it finished. `pulumi-homelab` has both halves:

```ts
const cmdline = new KernelCmdline('cgroups', host, {
  flags: ['cgroup_memory=1', 'cgroup_enable=memory'],
});

const booted = new Precondition('cgroups-active', host, {
  // Not `bootedWith`, deliberately. That proves what the kernel was told, and on a Pi 5 the
  // firmware puts its own parameters first: a real machine here carries `cgroup_disable=memory`
  // from the firmware AND `cgroup_enable=memory` from cmdline.txt on the same line. The kernel
  // takes the later one, so it is correct — but the same two in the other order would satisfy a
  // check on the command line while the controller was off. This asks the kernel what it actually
  // enabled.
  check: checkCommand('grep -qw memory /sys/fs/cgroup/cgroup.controllers'),
  message: 'this Pi has not booted with the memory cgroup controller. `sudo reboot`, then deploy again.',
}, { dependsOn: [cmdline] });

new K3sBinary('k3s', host, { ... }, { dependsOn: [booted] });
```

Declaring that pair is left to the caller rather than hidden inside `K3sServer`, because a dynamic
resource cannot own another resource, and a gate you cannot see in the graph is a gate nobody knows
they depend on.

### What this does not own

`/etc/rancher/k3s` is created if it is missing and otherwise left alone — its mode is not read back,
so this does not enforce one. Declare it with `pulumi-homelab`'s `Directory` if you want it modelled.
`/var/lib/rancher/k3s` is never touched by a delete: that is etcd, every workload and every volume on
the node, and no deployment should decide to remove it.

## Status

The five resources are written, typecheck clean under `strict` and `noUncheckedIndexedAccess`, and
are tested where they can be tested without a machine: artifact selection, version parsing, config
rendering and the kubeconfig rewrite.

**None of it has been run against real hardware.** The whole stack also assumes passwordless sudo
for the SSH user, because `pulumi-homelab`'s `asRoot` uses `sudo -n`, and that is unconfirmed. Both
are honest gaps rather than known failures.

## Licence

Apache-2.0
