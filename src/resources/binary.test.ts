import { describe, expect, it } from 'vitest';
import { artifactFor, artifactFrom, parseVersion, releaseUrl } from './binary.ts';

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

/**
 * The kernel and the userland are different questions, and on a Raspberry Pi they routinely give
 * different answers. A 32-bit Pi OS image on a Pi 4 or 5 runs a 64-bit kernel, so `uname -m` says
 * aarch64 while every binary on the machine is armhf. Believing the kernel there installs a k3s
 * that cannot execute, and the failure surfaces long after the download and the checksum have both
 * reported success.
 */
describe('a kernel and a userland that disagree', () => {
  it('believes the userland, because that is where the binary runs', () => {
    expect(artifactFrom('aarch64', 'armhf')).toBe('k3s-armhf');
  });

  it('agrees with itself when they agree', () => {
    expect(artifactFrom('aarch64', 'arm64')).toBe('k3s-arm64');
    expect(artifactFrom('x86_64', 'amd64')).toBe('k3s');
  });

  it('falls back to the kernel where there is no dpkg to ask', () => {
    // every machine that is not Debian-derived, where the two agree anyway
    expect(artifactFrom('aarch64', '')).toBe('k3s-arm64');
    expect(artifactFrom('x86_64', '')).toBe('k3s');
  });

  it('still gives up rather than guessing', () => {
    expect(artifactFrom('riscv64', '')).toBeNull();
    expect(artifactFrom('riscv64', 'riscv64')).toBeNull();
  });

  it('leaves the kernel-only answer alone', () => {
    expect(artifactFor('aarch64')).toBe('k3s-arm64');
  });
});
