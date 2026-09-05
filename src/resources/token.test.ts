import { describe, expect, it } from 'vitest';
import { nodeTokenPath } from './token.ts';

/**
 * The token lives inside the data directory, so a node whose data was moved off the SD card keeps
 * its token there too. Reading the default path on such a machine is not a wrong answer, it is a
 * wait for a file that will never appear on a server that is running perfectly well.
 */
describe('where the join token lives', () => {
  it('follows the data directory', () => {
    expect(nodeTokenPath('/mnt/storage/k3s')).toBe('/mnt/storage/k3s/server/node-token');
  });

  it('falls back to where k3s puts it', () => {
    expect(nodeTokenPath()).toBe('/var/lib/rancher/k3s/server/node-token');
  });
});
