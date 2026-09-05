/**
 * Prove every provider here survives the trip into the state file.
 *
 * Pulumi does one thing to a dynamic provider that nothing else in this repository does: it
 * serialises the whole closure — every function the provider object can reach, and everything those
 * functions capture — and writes it into the state file. A provider that typechecks, passes its
 * tests and imports cleanly can still be impossible to serialise, and the failure arrives on
 * somebody's first `pulumi up` as a message about neither Pulumi nor ssh.
 *
 * That is not hypothetical here. `NodeToken` and `Kubeconfig` both failed against a real machine
 * with `Failed to parse URL from [object Object]` while `K3sBinary` and `K3sServer` created
 * successfully, and nothing in this repository could tell the difference.
 *
 * **This deliberately does not run under vitest.** Vite's SSR transform rewrites imports into
 * captured variables, which is the exact shape the serialiser rejects, so a vitest version fails on
 * correct code and proves nothing. It has to run the way Pulumi runs it: plain node, real modules.
 */
import { createRequire } from 'node:module';
import { randomUUID } from 'node:crypto';
import { rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import * as pulumi from '@pulumi/pulumi';
import { providerFor as binary } from '../src/resources/binary.ts';
import { providerFor as node } from '../src/resources/node.ts';
import { providerFor as token } from '../src/resources/token.ts';
import { providerFor as kubeconfig } from '../src/resources/kubeconfig.ts';

// TEST-NET-3: routed nowhere, so ssh fails fast rather than reaching anything real
const host = { address: '198.51.100.1', user: 'nobody', timeout: 3 };

const checks: [string, () => unknown][] = [
  ['K3sBinary', () => binary(host)],
  ['K3sServer', () => node(host, 'server', 'k3s')],
  ['K3sAgent', () => node(host, 'agent', 'k3s-agent')],
  ['NodeToken', () => token(host)],
  ['Kubeconfig', () => kubeconfig(host)],
];

let failed = false;

/**
 * Run the provider the way Pulumi will: from its serialised text, evaluated somewhere else.
 *
 * Serialising successfully is not the same as surviving serialisation. A closure can produce
 * perfectly valid text whose bindings resolve differently when it is evaluated again — the way a
 * local helper named after a global does, where losing the binding does not raise anything, it
 * silently calls the global instead. So the text is written out, loaded back, and actually invoked
 * against an address that cannot answer. Reaching ssh at all is the proof: it means every binding
 * on the way there survived the trip.
 */
async function runsAfterSerialising(provider: () => unknown): Promise<string | null> {
  const { text } = await pulumi.runtime.serializeFunction(provider);
  const file = join(tmpdir(), `serialise-check-${randomUUID()}.cjs`);
  await writeFile(file, text);
  try {
    const loaded = createRequire(import.meta.url)(file) as { handler: () => pulumi.dynamic.ResourceProvider };
    const rebuilt = loaded.handler();
    await rebuilt.read?.('probe', {} as never);
    return 'reached the machine, which cannot happen against an unroutable address';
  } catch (error) {
    const message = (error as Error).message;
    // The transport refusing to connect is the good outcome: everything in between worked.
    if (message.includes('cannot reach')) return null;
    return message;
  } finally {
    await rm(file, { force: true });
  }
}

try {
  const surface = await import('../src/index.ts');
  console.log(`  ok   the package loads through node's own loader (${Object.keys(surface).length} exports)`);
} catch (error) {
  failed = true;
  console.error(`  FAIL the package does not load: ${(error as Error).message}`);
}

for (const [what, provider] of checks) {
  try {
    const serialised = await pulumi.runtime.serializeFunction(provider);
    const broken = await runsAfterSerialising(provider);
    if (broken) {
      failed = true;
      console.error(`  FAIL ${what} serialises but does not run: ${broken}`);
    } else {
      console.log(`  ok   ${what} serialises and runs (${serialised.text.length} bytes)`);
    }
  } catch (error) {
    failed = true;
    console.error(`  FAIL ${what}: ${(error as Error).message}`);
  }
}

/**
 * Proof that this check can fail.
 *
 * A guard nobody has watched fail is not evidence, so one provider here is broken on purpose: its
 * `read` calls something that does not exist, which is what a binding lost on the way into the
 * state file looks like from the outside. If this stops being reported as broken, the revive stage
 * has stopped surfacing anything and every "ok" above means only that nothing threw.
 *
 * What it deliberately does not claim: it is not a reproduction of the `fetch` bug that prompted
 * this file. That one — a helper named after a global, defined inside the provider factory — failed
 * on a real machine and reproduces in pulumi-homelab's environment, but not in this one: the local
 * binding survives revival under the @pulumi/pulumi version here, so the same code that broke a
 * deployment passes this check. The rename to `collect` stands on the machine's evidence and on it
 * being the shape of the providers that never failed, not on anything demonstrated here.
 */
function unrevivableProvider(): pulumi.dynamic.ResourceProvider {
  return {
    async read(id: string) {
      const missing = (globalThis as Record<string, unknown>)['__helperThatDoesNotExist'] as
        | ((x: string) => Promise<Record<string, unknown>>)
        | undefined;
      return { id, props: await missing!(id) };
    },
  } as unknown as pulumi.dynamic.ResourceProvider;
}

const caught = await runsAfterSerialising(unrevivableProvider);
if (caught && !caught.includes('cannot reach') && !caught.includes('reached the machine')) {
  console.log(`  ok   a broken provider is still reported as broken (${caught})`);
} else {
  failed = true;
  console.error(`  FAIL a deliberately broken provider was accepted (${caught ?? 'reported clean'}), so the passes above mean nothing`);
}

console.log(failed ? 'package checks: FAILED' : `package checks: ${checks.length + 2} passed`);
process.exit(failed ? 1 : 0);
