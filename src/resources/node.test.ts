import { describe, expect, it } from 'vitest';
import { configFor, renderConfig, renderUnit } from './node';

/**
 * The config file is compared against the machine's copy on every refresh, so its rendering has to
 * be byte-stable and its quoting has to be right. A YAML parser that reads `1.36` as a number or
 * `no` as false does not fail — it starts a node with a setting nobody wrote.
 */
describe('rendering the config file', () => {
  it('quotes every string, whatever YAML would otherwise make of it', () => {
    const out = renderConfig([['node-name', 'no'], ['token', '1.36'], ['server', 'https://a:6443']]);
    expect(out).toContain('node-name: "no"');
    expect(out).toContain('token: "1.36"');
    expect(out).toContain('server: "https://a:6443"');
  });

  it('escapes a string that contains a quote', () => {
    expect(renderConfig([['node-name', 'it"s']])).toContain('node-name: "it\\"s"');
  });

  it('writes booleans and numbers as themselves', () => {
    expect(renderConfig([['cluster-init', true], ['x', 3]])).toContain('cluster-init: true');
    expect(renderConfig([['cluster-init', true], ['x', 3]])).toContain('x: 3');
  });

  it('writes a list as a list', () => {
    expect(renderConfig([['tls-san', ['a', 'b']]])).toContain('tls-san:\n  - "a"\n  - "b"');
  });

  it('leaves out what was not asked for', () => {
    // An explicit empty list and an absent key mean the same thing to k3s, and writing one would
    // make a default look different from a deliberate nothing on every diff
    const out = renderConfig([['disable', []], ['tls-san', undefined], ['token', 'x']]);
    expect(out).not.toContain('disable');
    expect(out).not.toContain('tls-san');
    expect(out).toContain('token: "x"');
  });

  it('says who wrote it', () => {
    // The file is overwritten on every deployment; somebody debugging at 2am deserves to be told
    expect(renderConfig([])).toContain('pulumi-homelab-k3s');
  });
});

describe('the systemd unit', () => {
  it('hands the cgroup subtree to containerd', () => {
    // Without Delegate=yes, systemd and containerd both believe they own the subtree and containers
    // are killed for reasons neither of them logs anywhere useful
    expect(renderUnit('server', '/usr/local/bin/k3s')).toContain('Delegate=yes');
  });

  it('does not take the containers down with the service', () => {
    // KillMode=process is the difference between restarting k3s and an outage
    expect(renderUnit('server', '/usr/local/bin/k3s')).toContain('KillMode=process');
  });

  it('runs the subcommand the role needs, from where the binary actually is', () => {
    expect(renderUnit('server', '/opt/k3s')).toContain('ExecStart=/opt/k3s server');
    expect(renderUnit('agent', '/usr/local/bin/k3s')).toContain('ExecStart=/usr/local/bin/k3s agent');
  });

  it('gives a cold control plane as long as it needs to come up', () => {
    // A start timeout kills a Pi half way through bringing up etcd, then does it again
    expect(renderUnit('server', '/usr/local/bin/k3s')).toContain('TimeoutStartSec=0');
  });
});

describe('describing a node', () => {
  it('starts a cluster when told to', () => {
    const out = configFor('server', { clusterInit: true, tlsSan: ['192.168.0.47'] });
    expect(out).toContain('cluster-init: true');
    expect(out).toContain('tls-san:\n  - "192.168.0.47"');
    expect(out).not.toContain('server:');
  });

  it('refuses to both start a cluster and join one', () => {
    // Both is not a node that does something reasonable, it is a node that does one of them
    // depending on flag order, and finding out which takes an afternoon
    expect(() => configFor('server', { clusterInit: true, server: 'https://a:6443', token: 't' }))
      .toThrow(/cannot both start a cluster and join one/);
  });

  it('refuses to join anything without the token', () => {
    expect(() => configFor('server', { server: 'https://a:6443' })).toThrow(/no token was given/);
    expect(() => configFor('agent', { server: 'https://a:6443', token: '' })).toThrow(/no token was given/);
  });

  it('refuses an agent with nowhere to go', () => {
    expect(() => configFor('agent', { server: '', token: 't' })).toThrow(/which server to join/);
  });

  it('keeps control-plane settings off an agent', () => {
    // tls-san and disable are server flags; k3s agent refuses to start when it is handed one
    const out = configFor('agent', {
      server: 'https://a:6443',
      token: 't',
      extra: { 'node-ip': '192.168.0.48' },
    });
    expect(out).not.toContain('tls-san');
    expect(out).toContain('node-ip: "192.168.0.48"');
  });

  it('sorts the escape hatch, so reordering an object is not a change to the machine', () => {
    const one = configFor('server', { clusterInit: true, extra: { b: '2', a: '1' } });
    const other = configFor('server', { clusterInit: true, extra: { a: '1', b: '2' } });
    expect(one).toBe(other);
  });
});

/**
 * Everything below is about a data directory that is not on the root filesystem, which is the
 * normal shape on a Pi: the SD card cannot survive the write load and the cluster lives on another
 * disk. That decision has one consequence that is not obvious and is not recoverable.
 */
describe('a data directory on another disk', () => {
  it('tells systemd to wait for the mount', () => {
    // The dangerous case is a `nofail` fstab entry, which is correct for booting and is exactly what
    // lets k3s start before the array is mounted. It then finds an empty data directory, decides it
    // is a new node, and builds a second empty cluster on the mount point of the real one. It does
    // not fail — it succeeds at the wrong thing, and the first symptom is that everything is gone.
    expect(renderUnit('server', '/usr/local/bin/k3s', '/mnt/storage/k3s'))
      .toContain('RequiresMountsFor=/mnt/storage/k3s');
  });

  it('says nothing about mounts when the data is where k3s puts it', () => {
    expect(renderUnit('server', '/usr/local/bin/k3s')).not.toContain('RequiresMountsFor');
  });

  it('writes the directory into the config as well as the unit', () => {
    const out = configFor('server', { clusterInit: false, dataDir: '/mnt/storage/k3s' });
    expect(out).toContain('data-dir: "/mnt/storage/k3s"');
  });

  it('carries kubelet arguments, which is where the kubelet root ends up', () => {
    // The kubelet keeps its own state and does not follow data-dir, so a node with its data moved
    // and its kubelet not moved is still writing pod state to the card it was trying to spare
    const out = configFor('agent', {
      server: 'https://a:6443',
      token: 't',
      dataDir: '/mnt/storage/k3s',
      kubeletArg: ['root-dir=/mnt/storage/k3s/kubelet'],
    });
    expect(out).toContain('kubelet-arg:\n  - "root-dir=/mnt/storage/k3s/kubelet"');
  });
});
