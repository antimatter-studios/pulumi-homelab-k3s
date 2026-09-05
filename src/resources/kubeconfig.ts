import * as pulumi from '@pulumi/pulumi';
import { ask, asRoot, readFile, shellQuote, type Host } from 'pulumi-homelab';

/**
 * The cluster's admin credentials, fetched off the node once it is answering.
 *
 * k3s writes this file itself when the server first starts, so this resource creates nothing — it
 * waits and then reads. It exists as a resource rather than a script because the kubeconfig is the
 * one output the rest of the estate depends on: everything deployed into the cluster needs it, and
 * a value Pulumi holds and can hand over is worth more than an instruction to scp a file.
 */

export interface KubeconfigArgs {
  /**
   * The address the credentials should point at.
   *
   * k3s writes `https://127.0.0.1:6443`, which is correct on the node and useless anywhere else.
   * Whatever is substituted here has to be a name the API server's certificate covers, which is
   * why `K3sServer` takes `tlsSan` — otherwise every connection fails verification and the error
   * talks about certificates rather than about the address being wrong.
   */
  server: string;
  /** How long to wait for a cluster that is still coming up. First boot is the slow one. */
  readySeconds?: number;
}

interface KubeconfigState {
  server: string;
  readySeconds: number;
  config: string;
}

const KUBECONFIG_PATH = '/etc/rancher/k3s/k3s.yaml';
const DEFAULT_READY_SECONDS = 300;

/** Point a kubeconfig at somewhere other than the node's own loopback address. */
export function repoint(config: string, server: string): string {
  return config.replace(/https:\/\/127\.0\.0\.1:6443/g, `https://${server}:6443`);
}

/** Read the file and repoint it, or null while k3s has not written it yet. */
export async function readKubeconfig(host: Host, server: string): Promise<string | null> {
  const file = await readFile(host, KUBECONFIG_PATH);
  if (!file) return null;
  return repoint(file.content, server);
}

function providerFor(host: Host): pulumi.dynamic.ResourceProvider<KubeconfigArgs, KubeconfigState> {
  const fetch = async (args: KubeconfigArgs): Promise<KubeconfigState> => {
    const readySeconds = args.readySeconds ?? DEFAULT_READY_SECONDS;
    // Waiting on the node rather than in a polling loop from here: one ssh session instead of
    // dozens, and `kubectl wait` is watching the API rather than guessing from a sleep. The file
    // appears before the node is ready, so both conditions are checked — a kubeconfig for a cluster
    // that cannot yet schedule anything would let the next stack start and fail confusingly.
    const waited = await ask(host, asRoot(
      `for _ in $(seq 1 ${readySeconds}); do test -f ${shellQuote(KUBECONFIG_PATH)} && break; sleep 1; done; ` +
      `test -f ${shellQuote(KUBECONFIG_PATH)} || exit 9; ` +
      `k3s kubectl wait --for=condition=Ready node --all --timeout=${readySeconds}s`,
    ));
    if (waited.code === 9) {
      throw new Error(
        `k3s never wrote ${KUBECONFIG_PATH} within ${readySeconds}s; ` +
        `check \`systemctl status k3s\` on ${host.address}`,
      );
    }
    if (waited.code !== 0) {
      throw new Error(
        `the k3s node did not become ready within ${readySeconds}s: ${(waited.err || waited.out).trim()}`,
      );
    }

    const config = await readKubeconfig(host, args.server);
    if (config === null) throw new Error(`${KUBECONFIG_PATH} vanished between waiting for it and reading it`);
    return { server: args.server, readySeconds, config };
  };

  return {
    async create(args) {
      return { id: `${host.address}:kubeconfig`, outs: await fetch(args) };
    },

    async read(id, state) {
      const server = state?.server ?? host.address;
      const config = await readKubeconfig(host, server);
      // A cluster that has been uninstalled should come back as a resource that no longer exists,
      // so the next up rebuilds it rather than handing out credentials to nothing.
      if (config === null) return { id: undefined, props: undefined };
      return { id, props: { readySeconds: DEFAULT_READY_SECONDS, ...state, server, config } };
    },

    async update(_id, _old, args) {
      return { outs: await fetch(args) };
    },

    async diff(_id, old, args) {
      const readySeconds = args.readySeconds ?? DEFAULT_READY_SECONDS;
      return {
        changes: old.server !== args.server || old.readySeconds !== readySeconds,
        replaces: [],
        stables: [],
        deleteBeforeReplace: false,
      };
    },

    async delete() {
      // Nothing to undo: this resource never wrote anything to the machine. Deleting the real
      // credentials would mean deleting the cluster, which is not this resource's decision to make.
    },
  };
}

/** The admin kubeconfig, pointed somewhere useful. Treat the value as a root password, because it is. */
export class Kubeconfig extends pulumi.dynamic.Resource {
  declare readonly config: pulumi.Output<string>;
  declare readonly server: pulumi.Output<string>;

  constructor(name: string, host: Host, args: KubeconfigArgs, opts?: pulumi.CustomResourceOptions) {
    super(providerFor(host), name, { config: undefined, ...args }, {
      // The file contains a client certificate and key with cluster-admin rights. Marking it here
      // rather than at the call site means it cannot be forgotten by whoever uses this next.
      additionalSecretOutputs: ['config'],
      ...opts,
    });
  }
}
