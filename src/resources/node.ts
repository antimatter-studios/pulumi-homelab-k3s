import * as pulumi from '@pulumi/pulumi';
import { ask, escalate, heredoc, mountedAt, must, readFile, readUnit, shellQuote, type Host } from 'pulumi-homelab';

/**
 * A k3s node: its config file and its systemd unit, as one resource.
 *
 * They are one thought rather than two. A config file without a unit is a machine that is
 * configured and does nothing; a unit without the config is a node that joins nothing, or worse,
 * starts a second cluster of its own. The two-resource version of that is a dependency edge
 * somebody eventually forgets to draw, and the failure arrives as a node that is running and
 * pointing at nowhere.
 *
 * A node is a server or an agent and never both, which is why they share `/etc/rancher/k3s/config.yaml`
 * and differ only in the unit they install and the subcommand it runs. That is also the whole
 * shape of "HA" here: the first server sets `cluster-init`, every other server points at it, and
 * agents point at it too. There is deliberately no `Cluster` type holding node pools and a model of
 * the control plane — the only previous native k3s provider was archived by its author with the
 * note that it was "way too complicated", and that abstraction is what he meant.
 */

/** What can be written into k3s's config file. It mirrors the CLI flags, minus the leading dashes. */
export type ConfigValue = string | number | boolean | string[];

const CONFIG_DIR = '/etc/rancher/k3s';
const CONFIG_PATH = `${CONFIG_DIR}/config.yaml`;
/**
 * The config file holds the join token, which is the whole cluster's credential: anything holding
 * it can add a control-plane node. Root-only, and read back, so a file somebody chmod-ed while
 * debugging comes back as drift rather than staying quietly readable for years.
 */
const CONFIG_MODE = '0600';
/**
 * The unit file's mode, read back for the same reason the config's is: a mode written on every
 * deployment and never checked is a claim nothing can contradict, and a world-writable unit file is
 * a way to run anything as root at the next boot.
 */
const UNIT_MODE = '0644';
const DEFAULT_BINARY = '/usr/local/bin/k3s';

const BANNER = '# Managed by pulumi-homelab-k3s. Edits here are drift and will be overwritten.';

/** One YAML scalar. */
function scalar(value: string | number | boolean): string {
  // Always quote strings: a version like 1.36 is a number to YAML, `no` is false, and a token
  // beginning with a digit is whatever the parser decides that afternoon. JSON's string escaping is
  // a subset of YAML's double-quoted style, so stringify is exactly right and not merely close.
  return typeof value === 'string' ? JSON.stringify(value) : String(value);
}

/**
 * Render k3s's config file.
 *
 * Written by hand rather than through a YAML library because the output has to be byte-stable: it
 * is compared against what is on the machine on every refresh, and a serialiser that changes its
 * mind about quoting between versions would show a config drifting when nothing had changed.
 */
export function renderConfig(pairs: Array<[string, ConfigValue | undefined]>): string {
  const lines = [BANNER];
  for (const [key, value] of pairs) {
    if (value === undefined) continue;
    if (Array.isArray(value)) {
      // An empty list and an absent key mean the same thing to k3s, so writing `disable: []` would
      // only be a way for a default to look different from an explicit nothing.
      if (value.length === 0) continue;
      lines.push(`${key}:`);
      for (const item of value) lines.push(`  - ${scalar(item)}`);
    } else {
      lines.push(`${key}: ${scalar(value)}`);
    }
  }
  return `${lines.join('\n')}\n`;
}

/**
 * The systemd unit, which is k3s's own with two lines that matter kept and nothing else added.
 *
 * `Delegate=yes` hands the cgroup subtree to containerd. Without it systemd and containerd both
 * believe they own it, and containers are killed for reasons that appear in no log either of them
 * writes. `KillMode=process` stops a k3s restart taking every running container down with it, which
 * is the difference between upgrading the binary and an outage.
 */
export function renderUnit(role: 'server' | 'agent', binary: string, mount?: string): string {
  return [
    '[Unit]',
    `Description=Lightweight Kubernetes (k3s ${role})`,
    'Documentation=https://k3s.io',
    'Wants=network-online.target',
    'After=network-online.target',
    // The single most valuable line here when the data directory is on a separate disk. A `nofail`
    // fstab entry is correct — it stops a late or missing array holding up the boot — and it is
    // exactly what lets k3s start before the array is mounted. k3s then finds an empty data
    // directory, concludes it is a new node, and builds a second, empty cluster on top of the mount
    // point of the real one. It does not fail; it succeeds at the wrong thing, and the first sign is
    // that every workload has vanished. `RequiresMountsFor` pulls in the mount unit and orders after
    // it, so k3s either sees the real data or does not start.
    // Two lines for two different failures. `RequiresMountsFor` pulls in the mount unit and orders
    // after it, so a disk that is late or fails to mount stops k3s starting. The condition covers
    // the case the dependency cannot see: the path exists, is not a mount point, and systemd has
    // nothing to wait for — an array that was unmounted by hand, or a mount unit that succeeded
    // against the wrong device. A failed condition skips the unit rather than failing it, which is
    // right here, because the alternative to not starting is starting empty and building a second
    // cluster over the top of the real one.
    ...(mount ? [`RequiresMountsFor=${mount}`, `ConditionPathIsMountPoint=${mount}`] : []),
    '',
    '[Service]',
    // k3s tells systemd when the API is actually up, so dependent units start after the cluster
    // answers rather than after the process exists.
    'Type=notify',
    'EnvironmentFile=-/etc/default/%N',
    'ExecStartPre=-/sbin/modprobe br_netfilter',
    'ExecStartPre=-/sbin/modprobe overlay',
    `ExecStart=${binary} ${role}`,
    'KillMode=process',
    'Delegate=yes',
    'LimitNOFILE=1048576',
    'LimitNPROC=infinity',
    'LimitCORE=infinity',
    'TasksMax=infinity',
    // A control plane coming up on a cold Pi takes minutes, and a start timeout would kill it
    // half way and then do the same thing again.
    'TimeoutStartSec=0',
    'Restart=always',
    'RestartSec=5s',
    '',
    '[Install]',
    'WantedBy=multi-user.target',
    '',
  ].join('\n');
}

interface NodeState {
  /** The rendered config file, which contains the join token. Secret, always. */
  config: string;
  unit: string;
  configMode: string;
  unitMode: string;
  enabled: boolean;
  started: boolean;
}

interface SharedArgs {
  /** The address of a server to join, as a URL: 'https://192.168.0.47:6443'. */
  server?: string;
  /**
   * The shared secret a node presents to join.
   *
   * Set it yourself and the cluster is reproducible: every node knows the token before the first
   * one exists. Leave it unset on a first server and k3s invents one, which then has to be read
   * back off that machine with `NodeToken` before anything else can join.
   */
  token?: string;
  /**
   * Where k3s keeps everything: containerd's image store, the datastore, local-path volumes.
   *
   * Moving it off an SD card is the difference between a Pi that lasts and one that does not — the
   * killer is not bulk writing but the datastore's constant small fsyncs. Setting this also derives
   * a `RequiresMountsFor` into the unit, which is not optional once the data lives on another disk.
   */
  dataDir?: string;
  /**
   * A mount point the data directory depends on, which must be mounted for any of this to be safe.
   *
   * Set it whenever `dataDir` is on another disk. It does two separate jobs, and both are needed.
   * In the unit it becomes `RequiresMountsFor`, so systemd will not start k3s before the disk is
   * there. At deployment time it is checked before anything is written, because a deployment is the
   * other way this goes wrong: with the disk unmounted, `mkdir -p` cheerfully creates the data
   * directory on the root filesystem, k3s starts, finds it empty, and builds a brand new cluster on
   * top of the mount point of the real one. Nothing fails. The workloads are simply gone, and the
   * disk underneath is still fine — which is the good news and the reason to stop rather than
   * proceed.
   */
  requiresMount?: string;
  /** Passed through to the kubelet: 'root-dir=/mnt/storage/k3s/kubelet'. */
  kubeletArg?: string[];
  /**
   * containerd's snapshotter. k3s defaults to overlayfs, which works on btrfs and is almost always
   * what you want; 'btrfs' is only worth asking for deliberately, and costs a subvolume.
   */
  snapshotter?: string;
  nodeName?: string;
  nodeLabel?: string[];
  nodeTaint?: string[];
  /** Anything else k3s accepts in its config file, written straight through, keys sorted. */
  extra?: Record<string, ConfigValue>;
  /** Where `K3sBinary` put it. */
  binary?: string;
  enabled?: boolean;
  started?: boolean;
}

export interface K3sServerArgs extends SharedArgs {
  /**
   * Start a new cluster with embedded etcd, rather than the single-node sqlite datastore.
   *
   * This is the flag that decides whether a second server can ever be added. Turning it on later
   * means migrating the datastore of a running cluster, so it is worth setting on the first server
   * even when there is only one machine and no plan to buy another.
   */
  clusterInit?: boolean;
  /** Every name or address the API certificate has to cover, beyond the node's own. */
  tlsSan?: string[];
  /** k3s components to leave out: 'traefik', 'servicelb', 'local-storage'. */
  disable?: string[];
}

export interface K3sAgentArgs extends SharedArgs {
  /** Which cluster to join. An agent without one is a node that does nothing. */
  server: string;
  /** What it presents on the way in. */
  token: string;
}

const DEFAULTS = { enabled: true, started: true } as const;

const unitPath = (name: string) => `/etc/systemd/system/${name}.service`;

/**
 * The config file this node should have, and the reasons it might not be describable at all.
 *
 * Exported because the three ways of getting a cluster wrong are all decided here — a server told
 * to both start and join, a node joining with no token, an agent with nowhere to go — and a caller
 * that wants to see the file before a deployment writes it should not have to run one.
 */
export function configFor(role: 'server' | 'agent', args: K3sServerArgs | K3sAgentArgs): string {
  const server = 'clusterInit' in args && args.clusterInit ? undefined : args.server;
  if (role === 'server' && 'clusterInit' in args && args.clusterInit && args.server) {
    throw new Error(
      'a server cannot both start a cluster and join one: set clusterInit on the first server, ' +
      'and server on the others',
    );
  }
  if (server && !args.token) {
    throw new Error(
      `this node joins ${server} and no token was given; ` +
      'set the same token on every node, or read the first server\'s with NodeToken',
    );
  }
  if (role === 'agent' && !server) {
    throw new Error('an agent has to be told which server to join');
  }

  const serverArgs = args as K3sServerArgs;
  return renderConfig([
    ['token', args.token],
    ['cluster-init', role === 'server' && serverArgs.clusterInit ? true : undefined],
    ['server', server],
    ['tls-san', role === 'server' ? serverArgs.tlsSan : undefined],
    ['disable', role === 'server' ? serverArgs.disable : undefined],
    ['data-dir', args.dataDir],
    ['snapshotter', args.snapshotter],
    ['node-name', args.nodeName],
    ['node-label', args.nodeLabel],
    ['node-taint', args.nodeTaint],
    ['kubelet-arg', args.kubeletArg],
    // Sorted, so that reordering the keys of an object in the source is not a change to the file.
    ...Object.entries(args.extra ?? {}).sort(([a], [b]) => a.localeCompare(b)),
  ]);
}

function wanted(role: 'server' | 'agent', args: K3sServerArgs | K3sAgentArgs): NodeState {
  return {
    config: configFor(role, args),
    unit: renderUnit(role, args.binary ?? DEFAULT_BINARY, args.requiresMount ?? args.dataDir),
    configMode: CONFIG_MODE,
    unitMode: UNIT_MODE,
    enabled: args.enabled ?? DEFAULTS.enabled,
    started: args.started ?? DEFAULTS.started,
  };
}

/** Put both files where they belong and make systemd's world match them. */
async function apply(host: Host, name: string, state: NodeState, requiresMount?: string): Promise<void> {
  const unit = shellQuote(name);
  if (requiresMount) {
    // Before anything is created, because the first thing this would otherwise do is `mkdir -p` the
    // data directory onto the root filesystem, at which point the mount point is no longer empty
    // and mounting the real disk over it hides what was just written there.
    const mounted = await ask(host, escalate(host, mountedAt(requiresMount)));
    if (mounted.code !== 0) {
      throw new Error(
        `${requiresMount} on ${host.address} is not mounted, and k3s's data directory is on it. ` +
        'Refusing to write anything: with the disk absent this would create the data directory on ' +
        'the root filesystem, and k3s would then start, find it empty, and build a new empty ' +
        'cluster on the mount point of the real one. Mount it and deploy again.',
      );
    }
  }
  await must(host, escalate(host,
    // The directory is made but not otherwise owned: its mode is not read back, so enforcing one
    // here would be a setting nothing checks, re-applied on every deployment, quietly fighting
    // both k3s and anyone who declares the directory with `pulumi-homelab`'s `Directory`. What
    // actually protects the token is the file's own 0600, which is read back.
    `mkdir -p ${shellQuote(CONFIG_DIR)} && ` +
    // The config is written before the unit is told to start, because k3s reads it once at startup
    // and a server that came up without its token joins nothing and cannot be told to later.
    `${heredoc(CONFIG_PATH, state.config)}\n` +
    `chmod ${state.configMode} ${shellQuote(CONFIG_PATH)} && ` +
    `${heredoc(unitPath(name), state.unit)}\n` +
    `chmod ${state.unitMode} ${shellQuote(unitPath(name))} && ` +
    // systemd caches unit files, and a changed one it has not re-read is the classic "why is it
    // still running the old command" afternoon.
    `systemctl daemon-reload && ` +
    `systemctl ${state.enabled ? 'enable' : 'disable'} ${unit} && ` +
    `systemctl ${state.started ? 'restart' : 'stop'} ${unit}`,
  ));
}

/**
 * What the machine says about this node now, or null when the unit is not there at all.
 *
 * Two round trips rather than one hand-rolled command, because both halves are already answered
 * properly by `pulumi-homelab` — and a provider that re-implements the base one's reads is how the
 * two drift apart.
 */
async function readNode(host: Host, name: string): Promise<NodeState | null> {
  const unit = await readUnit(host, name);
  if (!unit) return null;
  const config = await readFile(host, CONFIG_PATH);
  return {
    unit: unit.unit,
    // Both modes come back already normalised by the base provider — `stat` says 644 where this
    // code says 0644, and a normalisation written twice is one that eventually differs.
    unitMode: unit.mode,
    enabled: unit.enabled,
    started: unit.started,
    // A missing config on a machine that has the unit is real drift and worth showing as an empty
    // file rather than as an absent resource: the unit is still there, still starting something.
    config: config?.content ?? '',
    configMode: config?.mode ?? '',
  };
}

function providerFor(
  host: Host,
  role: 'server' | 'agent',
  name: string,
): pulumi.dynamic.ResourceProvider<K3sServerArgs | K3sAgentArgs, NodeState> {
  return {
    async create(args) {
      const state = wanted(role, args);
      await apply(host, name, state, args.requiresMount);
      return { id: name, outs: state };
    },

    async read(id, state) {
      const actual = await readNode(host, id);
      if (!actual) return { id: undefined, props: undefined };
      return { id, props: { ...state, ...actual } };
    },

    async update(id, _old, args) {
      const state = wanted(role, args);
      await apply(host, id, state, args.requiresMount);
      return { outs: state };
    },

    async diff(_id, old, args) {
      const state = wanted(role, args);
      return {
        changes: old.config !== state.config
          || old.unit !== state.unit
          || old.configMode !== state.configMode
          || old.unitMode !== state.unitMode
          || old.enabled !== state.enabled
          || old.started !== state.started,
        replaces: [],
        stables: [],
        deleteBeforeReplace: false,
      };
    },

    async delete(id) {
      // The unit and the config go, because this resource wrote them. `/var/lib/rancher/k3s` stays:
      // that is etcd, every workload and every volume on the node, and no deployment should decide
      // to remove it. `|| true` on the stop, because a service that already died is not a failure
      // to tidy up after.
      await must(host, escalate(host,
        `systemctl disable --now ${shellQuote(id)} || true; ` +
        `rm -f ${shellQuote(unitPath(id))} ${shellQuote(CONFIG_PATH)}; ` +
        `systemctl daemon-reload`,
      ));
    },
  };
}

/**
 * A k3s control-plane node.
 *
 * The first one sets `clusterInit`; the rest point `server` at it and present the same `token`.
 * Three of them is a control plane that survives losing one, which is the only definition of HA
 * this provider has an opinion about.
 */
export class K3sServer extends pulumi.dynamic.Resource {
  declare readonly config: pulumi.Output<string>;
  declare readonly unit: pulumi.Output<string>;
  declare readonly enabled: pulumi.Output<boolean>;
  declare readonly started: pulumi.Output<boolean>;

  constructor(name: string, host: Host, args: K3sServerArgs, opts?: pulumi.CustomResourceOptions) {
    super(providerFor(host, 'server', 'k3s'), name, { config: undefined, unit: undefined, ...args }, {
      // The rendered config contains the join token, which is enough to add a control-plane node to
      // the cluster. Marking it here rather than at the call site means it cannot be forgotten.
      additionalSecretOutputs: ['config', 'token'],
      ...opts,
    });
  }
}

/** A k3s worker. It runs no control plane and needs only somewhere to join and the token to do it. */
export class K3sAgent extends pulumi.dynamic.Resource {
  declare readonly config: pulumi.Output<string>;
  declare readonly unit: pulumi.Output<string>;
  declare readonly enabled: pulumi.Output<boolean>;
  declare readonly started: pulumi.Output<boolean>;

  constructor(name: string, host: Host, args: K3sAgentArgs, opts?: pulumi.CustomResourceOptions) {
    super(providerFor(host, 'agent', 'k3s-agent'), name, { config: undefined, unit: undefined, ...args }, {
      additionalSecretOutputs: ['config', 'token'],
      ...opts,
    });
  }
}
