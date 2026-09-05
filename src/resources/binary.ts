import * as pulumi from '@pulumi/pulumi';
import { ask, escalate, must, shellQuote, type Host } from 'pulumi-homelab';

/**
 * k3s, installed as a pinned binary rather than by piping a script into a shell.
 *
 * The official instruction is `curl -sfL https://get.k3s.io | sh -`, and it is precisely the thing
 * this codebase exists to avoid: it is a command with no state to read back. Afterwards nothing can
 * answer "which version is on the machine" except by looking, and Pulumi would only know that it
 * once ran a command that fetched whatever was newest on that particular afternoon. Two machines
 * built a month apart would silently be two different clusters.
 *
 * So this fetches one named release, checks it against a hash written down in the source, and reads
 * its state back from `k3s --version`. An upgrade becomes a diff in the source; a binary somebody
 * replaced by hand becomes drift on the next refresh.
 */

/**
 * Which release artifact each machine needs.
 *
 * k3s publishes one binary per architecture under names that do not match `uname -m`, and getting
 * this wrong is not a clean failure: an armhf binary on an arm64 kernel gives "cannot execute
 * binary file" long after the download appeared to succeed. The mapping is small enough to state.
 */
const ARTIFACTS: Record<string, string> = {
  aarch64: 'k3s-arm64',
  arm64: 'k3s-arm64',
  armv7l: 'k3s-armhf',
  armv6l: 'k3s-armhf',
  // What a 32-bit process is told by a 64-bit kernel running it in compat mode, which is the whole
  // of Raspberry Pi OS 32-bit on a Pi 4 or 5. The string comes from the kernel's COMPAT_UTS_MACHINE
  // and is armv8l on an arm64 kernel, where an armhf kernel would have said armv7l for the same
  // userland. Without this entry the correct machine is refused outright.
  armv8l: 'k3s-armhf',
  x86_64: 'k3s',
};

const DEFAULT_PATH = '/usr/local/bin/k3s';

/**
 * The same question answered by the userland rather than the kernel.
 *
 * A 64-bit kernel with a 32-bit userland is not an exotic configuration on a Raspberry Pi, it is
 * the standard one: Raspberry Pi OS 32-bit ships a 64-bit kernel by default on a Pi 4, and must on
 * a Pi 5, whose A76 has no aarch32 at EL1 at all. Every binary on such a machine is armhf.
 *
 * What `uname -m` says there is a kernel configuration detail rather than a fact about the machine.
 * A 32-bit process is told whatever COMPAT_UTS_MACHINE holds — `armv8l` on an arm64 kernel, where
 * the same userland on an armhf kernel would have been told `armv7l` — and a process that asks for
 * the PER_LINUX personality is told `aarch64` instead. Three answers, one machine, and only one of
 * them is about what a binary needs to be.
 *
 * `dpkg --print-architecture` asks the userland what it is, which is the question that decides
 * whether the binary will run.
 */
const DEBIAN_ARCHITECTURES: Record<string, string> = {
  arm64: 'k3s-arm64',
  armhf: 'k3s-armhf',
  amd64: 'k3s',
};

/** The artifact a machine reporting this `uname -m` needs, or null where we have never seen one. */
export function artifactFor(machine: string): string | null {
  return ARTIFACTS[machine.trim()] ?? null;
}

/** The artifact for a `dpkg --print-architecture`, or null on a machine that has no dpkg to ask. */
export function artifactForDebianArch(arch: string): string | null {
  return DEBIAN_ARCHITECTURES[arch.trim()] ?? null;
}

/**
 * Which artifact this machine needs, given what both halves of it say.
 *
 * The userland wins where it answers, because the binary being installed runs in the userland. The
 * kernel is the fallback for machines without dpkg, which is every non-Debian one, and there the
 * two almost always agree anyway.
 */
export function artifactFrom(machine: string, debianArch: string): string | null {
  return artifactForDebianArch(debianArch) ?? artifactFor(machine);
}

/**
 * The version out of `k3s --version`, or null when the output is not what we expect.
 *
 * The first line reads `k3s version v1.36.4+k3s1 (0a1b2c3)`; the second is the bundled containerd
 * and moves independently of the release tag, so it is not what we are comparing against.
 */
export function parseVersion(output: string): string | null {
  return output.match(/^k3s version (\S+)/m)?.[1] ?? null;
}

export interface K3sBinaryArgs {
  /** A release tag exactly as k3s publishes it, including the build suffix: 'v1.36.4+k3s1'. */
  version: string;
  /**
   * The SHA-256 of each release artifact, keyed by the artifact's own name ('k3s-arm64').
   *
   * Keyed by artifact rather than by architecture because that is how the release publishes them,
   * in `sha256sum-<arch>.txt`, so a human can check what is written here against the source without
   * having to translate anything first. A missing entry is a hard failure rather than an unverified
   * download: an install that skips the check on the one machine nobody thought about is worse than
   * no check at all, because it looks the same as the ones that did check.
   */
  checksums: Record<string, string>;
  /** Where the binary goes. `/usr/local/bin` because it is not the distribution's to manage. */
  path?: string;
}

interface K3sBinaryState {
  version: string;
  checksums: Record<string, string>;
  path: string;
  /** The artifact this machine actually needed, kept so a diff can explain itself. */
  artifact: string;
}

/** What the installed binary says it is, or null when there is nothing installed. */
export async function readVersion(host: Host, path: string): Promise<string | null> {
  const asked = await ask(host, `test -x ${shellQuote(path)} || exit 9; ${shellQuote(path)} --version`);
  if (asked.code === 9) return null;
  if (asked.code !== 0) throw new Error(`could not run ${path} --version: ${asked.err.trim()}`);
  const version = parseVersion(asked.out);
  if (!version) throw new Error(`${path} --version said something unexpected: ${asked.out.trim()}`);
  return version;
}

/**
 * The URL of one release artifact.
 *
 * The tag contains a '+', which is a space once it reaches a URL, so it has to be encoded — an
 * unencoded one fetches a 404 page and the checksum then fails with a message about a hash
 * mismatch, which sends you looking in entirely the wrong place.
 */
export function releaseUrl(version: string, artifact: string): string {
  return `https://github.com/k3s-io/k3s/releases/download/${encodeURIComponent(version)}/${artifact}`;
}

/** Fetch the pinned release, prove it is the one we meant, and put it in place. */
async function install(
  host: Host,
  args: { version: string; checksums: Record<string, string>; path: string },
): Promise<string> {
  // Both in one round trip: an ssh handshake costs far more than either question. `dpkg` is absent
  // on a machine that is not Debian-derived, and a missing answer is an answer here rather than a
  // failure, so the exit code is swallowed and the line comes back empty.
  const asked = await must(host, 'uname -m; dpkg --print-architecture 2>/dev/null || true');
  const [machine = '', debianArch = ''] = asked.trim().split('\n').map((line) => line.trim());
  const artifact = artifactFrom(machine, debianArch);
  if (!artifact) {
    throw new Error(
      `no k3s release artifact known for a machine reporting '${machine}'` +
      (debianArch ? ` with a '${debianArch}' userland` : ''),
    );
  }
  const sum = args.checksums[artifact];
  if (!sum) {
    throw new Error(
      `this machine needs the '${artifact}' artifact and no checksum was given for it; ` +
      `take it from sha256sum-*.txt in the ${args.version} release`,
    );
  }

  await must(host, escalate(host,
    `set -e; tmp=$(mktemp); trap 'rm -f "$tmp"' EXIT; ` +
    `curl -fsSL -o "$tmp" ${shellQuote(releaseUrl(args.version, artifact))}; ` +
    // printf rather than echo, because sha256sum -c wants exactly two spaces between hash and name
    // and shells disagree about what echo does with its arguments.
    `printf '%s  %s\\n' ${shellQuote(sum)} "$tmp" | sha256sum -c - >/dev/null; ` +
    // install(1) rather than mv, so the binary is never briefly present and non-executable, and
    // never replaced in place underneath a running kernel that has it mapped.
    `install -m 0755 "$tmp" ${shellQuote(args.path)}; ` +
    // A new binary on disk changes nothing about the process already running from the old one, and
    // the unit file has not changed so systemd has no reason to act. Without this, an upgrade would
    // apply cleanly and the cluster would carry on running the previous version until somebody
    // rebooted the machine months later and wondered what had changed. Either unit may be the one
    // on this machine — a node is a server or an agent — and neither being active is normal on the
    // first install, when the unit does not exist yet.
    `for unit in k3s k3s-agent; do ` +
    `if systemctl is-active --quiet "$unit"; then systemctl restart "$unit"; fi; ` +
    `done`,
  ));

  const installed = await readVersion(host, args.path);
  if (installed !== args.version) {
    throw new Error(`installed ${args.version} but ${args.path} reports ${installed ?? 'nothing'}`);
  }
  return artifact;
}

function providerFor(host: Host): pulumi.dynamic.ResourceProvider<K3sBinaryArgs, K3sBinaryState> {
  return {
    async create(args) {
      const wanted = { ...args, path: args.path ?? DEFAULT_PATH };
      const artifact = await install(host, wanted);
      return { id: wanted.path, outs: { ...wanted, artifact } };
    },

    async read(id, state) {
      const version = await readVersion(host, id);
      if (version === null) return { id: undefined, props: undefined };
      // The checksums describe what we would install, not what is installed, so an import that
      // arrives without them carries none: the next up supplies them from the source.
      return { id, props: { checksums: {}, artifact: '', ...state, path: id, version } };
    },

    async update(id, _old, args) {
      const wanted = { ...args, path: id };
      const artifact = await install(host, wanted);
      return { outs: { ...wanted, artifact } };
    },

    async diff(_id, old, args) {
      const path = args.path ?? DEFAULT_PATH;
      return {
        changes: old.version !== args.version || old.path !== path,
        // A different path is a different installation, not a move: the old binary would otherwise
        // be left behind, still executable, for a stale unit file to keep starting.
        replaces: old.path !== path ? ['path'] : [],
        stables: [],
        deleteBeforeReplace: true,
      };
    },

    async delete(id) {
      // Only the binary goes. The cluster's data lives in /var/lib/rancher/k3s and removing it is
      // not something a deployment should decide on its own — that is every workload, every secret
      // and every persistent volume on the node, and it is unrecoverable. Tearing the cluster down
      // properly is a deliberate act with k3s's own uninstall script.
      await must(host, escalate(host,
        `for unit in k3s k3s-agent; do ` +
        `if systemctl is-active --quiet "$unit"; then systemctl stop "$unit"; fi; ` +
        `done; rm -f ${shellQuote(id)}`,
      ));
    },
  };
}

/** A pinned k3s release on the machine, honest about which version is actually there. */
export class K3sBinary extends pulumi.dynamic.Resource {
  declare readonly version: pulumi.Output<string>;
  declare readonly path: pulumi.Output<string>;
  declare readonly artifact: pulumi.Output<string>;

  constructor(name: string, host: Host, args: K3sBinaryArgs, opts?: pulumi.CustomResourceOptions) {
    super(providerFor(host), name, { artifact: undefined, path: DEFAULT_PATH, ...args }, opts);
  }
}
