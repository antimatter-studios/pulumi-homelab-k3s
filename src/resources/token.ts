import * as pulumi from '@pulumi/pulumi';
import { escalate, ask, shellQuote, type Host } from 'pulumi-homelab';

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
  /**
   * The server's data directory, when it is not the default.
   *
   * The token lives inside it, so a node whose data lives on another disk keeps its token there
   * too. Without this, waiting for the default path is waiting for a file that will never appear on
   * a machine that is running perfectly well.
   */
  dataDir?: string;
}

interface NodeTokenState {
  readySeconds: number;
  dataDir: string;
  token: string;
}

/** Where k3s keeps everything unless it was told otherwise. */
const DEFAULT_DATA_DIR = '/var/lib/rancher/k3s';
const DEFAULT_READY_SECONDS = 300;

/** k3s writes it here on a server, and only on a server. Root-only, because it is the way in. */
export function nodeTokenPath(dataDir?: string): string {
  return `${dataDir ?? DEFAULT_DATA_DIR}/server/node-token`;
}

/** What the server will accept from a joining node, or null while it has not written it yet. */
export async function readNodeToken(host: Host, dataDir?: string): Promise<string | null> {
  const path = nodeTokenPath(dataDir);
  const asked = await ask(host, escalate(host, `test -f ${shellQuote(path)} || exit 9; cat ${shellQuote(path)}`));
  if (asked.code === 9) return null;
  if (asked.code !== 0) throw new Error(`could not read ${path}: ${asked.err.trim()}`);
  // The file ends in a newline and the token does not; a token with a newline on the end is
  // accepted by nothing and the failure it produces talks about credentials rather than whitespace.
  return asked.out.trim();
}

/**
 * Wait for the server to write its token, then read it.
 *
 * At module scope, and not called `fetch`, both deliberately. A provider's closure is serialised
 * into the state file as text and evaluated again somewhere else, so a helper defined inside the
 * provider factory is one more thing that has to survive that trip — and a helper *named* after a
 * global is worse than that, because the name resolves either way. Shadow `fetch` and the code is
 * correct here and, wherever the binding is lost, quietly becomes a call to the global one, which
 * answers an object with `Failed to parse URL from [object Object]` and mentions nothing that would
 * lead you back here.
 */
async function collect(host: Host, args: NodeTokenArgs): Promise<NodeTokenState> {
  const readySeconds = args.readySeconds ?? DEFAULT_READY_SECONDS;
  const dataDir = args.dataDir ?? DEFAULT_DATA_DIR;
  const path = nodeTokenPath(dataDir);
  const waited = await ask(host, escalate(host,
    `for _ in $(seq 1 ${readySeconds}); do test -f ${shellQuote(path)} && break; sleep 1; done; ` +
    `test -f ${shellQuote(path)}`,
  ));
  if (waited.code !== 0) {
    throw new Error(
      `${host.address} has not written ${path} within ${readySeconds}s; ` +
      'it is written by a server, so check that this node is one, that it started, and that ' +
      'dataDir matches the data-dir it is actually running with',
    );
  }
  const token = await readNodeToken(host, dataDir);
  if (token === null) throw new Error(`${path} vanished between waiting for it and reading it`);
return { readySeconds, dataDir, token };
}

export function providerFor(host: Host): pulumi.dynamic.ResourceProvider<NodeTokenArgs, NodeTokenState> {
  return {
    async create(args) {
      return { id: `${host.address}:node-token`, outs: await collect(host, args) };
    },

    async read(id, state) {
      const token = await readNodeToken(host, state?.dataDir);
      // A server that has been uninstalled takes its token with it, and the one in state is then a
      // secret for a cluster that does not exist. Better that it comes back as absent.
      if (token === null) return { id: undefined, props: undefined };
      return { id, props: { readySeconds: DEFAULT_READY_SECONDS, dataDir: DEFAULT_DATA_DIR, ...state, token } };
    },

    async update(_id, _old, args) {
      return { outs: await collect(host, args) };
    },

    async diff(_id, old, args) {
      const readySeconds = args.readySeconds ?? DEFAULT_READY_SECONDS;
      const dataDir = args.dataDir ?? DEFAULT_DATA_DIR;
      return {
        changes: old.readySeconds !== readySeconds || old.dataDir !== dataDir,
        replaces: [],
        stables: [],
        deleteBeforeReplace: false,
      };
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
