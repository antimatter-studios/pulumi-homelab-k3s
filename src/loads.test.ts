import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { describe, expect, it } from 'vitest';

const run = promisify(execFile);

/**
 * That the package can actually be loaded the way Pulumi loads it.
 *
 * This exists because everything else in this repository passed while the package was unloadable.
 * `tsc --noEmit` resolves an extensionless relative import happily, and so does vitest — but Pulumi
 * runs the program through Node's ESM loader, which will not guess an extension, and every import
 * here failed with ERR_MODULE_NOT_FOUND at the first `pulumi preview` a consumer ran.
 *
 * The lesson is more general than the extension: the half of the contract `tsc` checks is not the
 * half that runs, and a repository whose verification never executes its own package is checking
 * the wrong thing confidently. So this executes it, in a real Node, from outside.
 *
 * The boundary of what it proves is worth stating, because it is smaller than "the package loads":
 * it proves that everything reachable from `src/index.ts` loads. A module imported lazily inside a
 * provider method, or a script that nothing re-exports, would not be covered. There are none today
 * — all four resource modules are re-exported from the index and nothing here imports dynamically —
 * and the day one appears, this test keeps passing while the package breaks for whoever reaches it.
 */
describe('the package as Pulumi will load it', () => {
  it('imports through Node\'s ESM loader with every export present', async () => {
    const { stdout } = await run(process.execPath, [
      '--experimental-strip-types',
      '--no-warnings',
      '-e',
      `import('./src/index.ts').then(m => console.log(Object.keys(m).sort().join(',')))`,
    ], { cwd: process.cwd(), timeout: 60_000 });

    const exported = stdout.trim().split(',');
    // The five resources are the package. If any of them stops loading, a consumer's deployment is
    // what finds out.
    for (const name of ['K3sBinary', 'K3sServer', 'K3sAgent', 'NodeToken', 'Kubeconfig']) {
      expect(exported, name).toContain(name);
    }
  }, 60_000);
});
