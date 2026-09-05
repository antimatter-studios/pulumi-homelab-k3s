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

## Status

Nothing built yet. This README is the brief.

## Licence

Apache-2.0
