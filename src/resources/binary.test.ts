import { describe, expect, it } from 'vitest';
import { artifactFor, parseVersion, releaseUrl } from './binary.ts';

/**
 * Choosing the artifact is the one decision here that fails late rather than loudly: the wrong
 * binary downloads happily, verifies against its own checksum, installs, and only then says
 * "cannot execute binary file" — by which point the obvious suspect is the download, not the
 * architecture.
 */
describe('choosing the release artifact for a machine', () => {
  it('knows the architectures a homelab actually has', () => {
    expect(artifactFor('aarch64')).toBe('k3s-arm64');
    expect(artifactFor('arm64')).toBe('k3s-arm64');
    expect(artifactFor('armv7l')).toBe('k3s-armhf');
    expect(artifactFor('armv6l')).toBe('k3s-armhf');
    expect(artifactFor('x86_64')).toBe('k3s');
  });

  it('ignores the newline uname leaves behind', () => {
    // `uname -m` over ssh arrives with its newline, and a lookup on 'aarch64\n' misses silently
    expect(artifactFor('aarch64\n')).toBe('k3s-arm64');
  });

  it('says it does not know, rather than guessing', () => {
    // A guess here is an unbootable node; null becomes a hard error with the machine's own word in it
    expect(artifactFor('riscv64')).toBeNull();
    expect(artifactFor('')).toBeNull();
  });
});

describe('reading the installed version back', () => {
  const real = 'k3s version v1.36.4+k3s1 (0a1b2c3d)\ngo version go1.24.3\n';

  it('takes the release tag, not the go version underneath it', () => {
    // The second line moves independently of the release, so comparing against it would report an
    // upgrade nobody asked for on a node nobody touched
    expect(parseVersion(real)).toBe('v1.36.4+k3s1');
  });

  it('keeps the build suffix, because that is what the release is called', () => {
    // '+k3s1' has to survive: stripped here and kept in the declared version, every refresh is drift
    expect(parseVersion(real)).toContain('+k3s1');
  });

  it('gives up rather than inventing a version', () => {
    expect(parseVersion('command not found')).toBeNull();
    expect(parseVersion('')).toBeNull();
  });
});

describe('building the release URL', () => {
  it('encodes the plus in the tag', () => {
    // Unencoded, the '+' is a space by the time it reaches the server: GitHub answers with a 404
    // page, curl saves it, and the checksum fails with a hash mismatch — which sends you looking
    // at the checksum you copied rather than at the URL
    expect(releaseUrl('v1.36.4+k3s1', 'k3s-arm64'))
      .toBe('https://github.com/k3s-io/k3s/releases/download/v1.36.4%2Bk3s1/k3s-arm64');
  });
});
