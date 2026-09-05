import * as pulumi from '@pulumi/pulumi';
import { asRoot, ask, shellQuote, type Host } from 'pulumi-homelab';

/**
 * The token a first server generated, read back so other nodes can join it.
 *
 * Only needed when the token was not set in the source. Setting it there is better — every node
 * then knows how to join before the first one exists, and rebuilding the cluster from scratch gives
 * the same token rather than a new one nothing else has been told about. This exists for the
 * cluster that already ran once without one, which is most of them, and for reading back what a
 * machine actually accepts rather than what we believe it accepts.
 */

export interface NodeTokenArgs {
  /** How long to wait for a server that is still starting for the first time. */
  readySeconds?: number;
}

interface NodeTokenState {
  readySeconds: number;
  token: string;
}

/** k3s writes it here on a server, and only on a server. Root-only, because it is the way in. */
const TOKEN_PATH = '/var/lib/rancher/k3s/server/node-token';
const DEFAULT_READY_SECONDS = 300;

/** What the server will accept from a joining node, or null while it has not written it yet. */
export async function readNodeToken(host: Host): Promise<string | null> {
  const asked = await ask(host, asRoot(`test -f ${shellQuote(TOKEN_PATH)} || exit 9; cat ${shellQuote(TOKEN_PATH)}`));
  if (asked.code === 9) return null;
  if (asked.code !== 0) throw new Error(`could not read ${TOKEN_PATH}: ${asked.err.trim()}`);
  // The file ends in a newline and the token does not; a token with a newline on the end is
  // accepted by nothing and the failure it produces talks about credentials rather than whitespace.
  return asked.out.trim();
}

function providerFor(host: Host): pulumi.dynamic.ResourceProvider<NodeTokenArgs, NodeTokenState> {
  const fetch = async (args: NodeTokenArgs): Promise<NodeTokenState> => {
    const readySeconds = args.readySeconds ?? DEFAULT_READY_SECONDS;
    const waited = await ask(host, asRoot(
      `for _ in $(seq 1 ${readySeconds}); do test -f ${shellQuote(TOKEN_PATH)} && break; sleep 1; done; ` +
      `test -f ${shellQuote(TOKEN_PATH)}`,
    ));
    if (waited.code !== 0) {
      throw new Error(
        `${host.address} has not written ${TOKEN_PATH} within ${readySeconds}s; ` +
        'it is written by a server, so check that this node is one and that it started',
      );
    }
    const token = await readNodeToken(host);
    if (token === null) throw new Error(`${TOKEN_PATH} vanished between waiting for it and reading it`);
    return { readySeconds, token };
  };

  return {
    async create(args) {
      return { id: `${host.address}:node-token`, outs: await fetch(args) };
    },

    async read(id, state) {
      const token = await readNodeToken(host);
      // A server that has been uninstalled takes its token with it, and the one in state is then a
      // secret for a cluster that does not exist. Better that it comes back as absent.
      if (token === null) return { id: undefined, props: undefined };
      return { id, props: { readySeconds: DEFAULT_READY_SECONDS, ...state, token } };
    },

    async update(_id, _old, args) {
      return { outs: await fetch(args) };
    },

    async diff(_id, old, args) {
      const readySeconds = args.readySeconds ?? DEFAULT_READY_SECONDS;
      return { changes: old.readySeconds !== readySeconds, replaces: [], stables: [], deleteBeforeReplace: false };
    },

    async delete() {
      // Nothing written, so nothing to remove. Deleting the real file would lock every future node
      // out of a cluster that is still running.
    },
  };
}

/** The join token off a running server. Anything holding it can add a node, so it is a secret. */
export class NodeToken extends pulumi.dynamic.Resource {
  declare readonly token: pulumi.Output<string>;

  constructor(name: string, host: Host, args: NodeTokenArgs = {}, opts?: pulumi.CustomResourceOptions) {
    super(providerFor(host), name, { token: undefined, ...args }, {
      additionalSecretOutputs: ['token'],
      ...opts,
    });
  }
}
