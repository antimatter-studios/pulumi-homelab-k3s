import { describe, expect, it } from 'vitest';
import { repoint } from './kubeconfig';

/**
 * k3s writes the kubeconfig with the server as 127.0.0.1, which is correct on the node and useless
 * from anywhere else. Getting this wrong gives a kubeconfig that works when tested on the machine
 * and fails everywhere it is actually needed.
 */
describe('pointing the kubeconfig somewhere useful', () => {
  it('rewrites the loopback address k3s writes', () => {
    expect(repoint('    server: https://127.0.0.1:6443\n', '192.168.0.47'))
      .toBe('    server: https://192.168.0.47:6443\n');
  });

  it('rewrites every occurrence, not the first', () => {
    const both = 'a: https://127.0.0.1:6443\nb: https://127.0.0.1:6443\n';
    expect(repoint(both, 'pi')).not.toContain('127.0.0.1');
  });

  it('leaves an address that was already correct alone', () => {
    const already = '    server: https://192.168.0.47:6443\n';
    expect(repoint(already, '192.168.0.47')).toBe(already);
  });

  it('does not touch a different loopback port', () => {
    // 6443 is the API; something else on 127.0.0.1 in this file is not ours to redirect
    expect(repoint('x: https://127.0.0.1:8080\n', 'pi')).toContain('127.0.0.1:8080');
  });
});
