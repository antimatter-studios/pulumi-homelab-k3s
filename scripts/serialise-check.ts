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
import { readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import * as pulumi from '@pulumi/pulumi';
import { providerFor as binary } from '../src/resources/binary.ts';
import { providerFor as node } from '../src/resources/node.ts';
import { providerFor as token } from '../src/resources/token.ts';
import { providerFor as kubeconfig } from '../src/resources/kubeconfig.ts';

// TEST-NET-3: routed nowhere, so ssh fails fast rather than reaching anything real
const host = { address: '198.51.100.1', user: 'nobody', timeout: 3 };

/**
 * Built first, then captured — because that is the shape Pulumi gets, and it is the only shape that
 * can fail.
 *
 * `new K3sServer(name, host, args)` calls `super(providerFor(host), ...)`, so the factory has
 * already run and Pulumi serialises the *object*. Serialising `() => providerFor(host)` instead
 * would put the factory body into the state file, where its locals are redeclared on revival and
 * everything works. That distinction is the whole bug: the same four lines pass one way and throw
 * `Failed to parse URL from /x` the other, in the same process, on the same version.
 *
 * This file tested the safe shape until pulumi-homelab pointed it out, which is why the providers
 * that broke a real deployment passed every check here.
 */
const providers: [string, unknown][] = [
  ['K3sBinary', binary(host)],
  ['K3sServer', node(host, 'server', 'k3s')],
  ['K3sAgent', node(host, 'agent', 'k3s-agent')],
  ['NodeToken', token(host)],
  ['Kubeconfig', kubeconfig(host)],
];

const checks: [string, () => unknown][] = providers.map(([what, built]) => [what, () => built]);

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
 * Proof that this check can fail, in the exact shape of the bug that prompted it.
 *
 * A helper named after a global, declared inside the provider factory, called from a provider
 * method — which is what `NodeToken` and `Kubeconfig` were when they failed against a real machine.
 * Captured the way Pulumi captures a provider, the local binding does not survive revival, the name
 * resolves to the global `fetch`, and it answers with a message that names neither the helper nor
 * the resource.
 *
 * If this is ever reported clean, the check has stopped being able to see the thing it exists for.
 */
function shadowedProvider(): unknown {
  const fetch = async (path: string) => ({ found: path });
  return {
    async read(id: string) {
      return { id, props: await fetch(id) };
    },
  };
}

const shadowed = shadowedProvider();

/**
 * The same bug found by reading rather than by running, which is the difference between catching
 * the instance and catching the class.
 *
 * The round trip above only exercises the provider methods it actually invokes, so a shadowing
 * local in a method nothing calls would sail through. This reads every provider source instead and
 * fails on any declaration that is both function-scoped and named after something on `globalThis`.
 * Indentation stands in for scope, which is crude and right often enough: a declaration at column
 * zero is module scope, and module scope is exactly what survives revival.
 *
 * Borrowed from pulumi-homelab, which wrote it after this repository supplied the bug.
 */
const DECLARATION = /^(\s+)(?:const|let|var|function)\s+([A-Za-z_$][\w$]*)/;

function shadowedLocals(source: string): { line: number; name: string }[] {
  const found: { line: number; name: string }[] = [];
  source.split('\n').forEach((text, index) => {
    const match = DECLARATION.exec(text);
    const name = match?.[2];
    if (name && name in globalThis) found.push({ line: index + 1, name });
  });
  return found;
}

// the guard proving itself, on a fixture rather than by anyone editing a source file to watch it go red
const fixtureFindings = shadowedLocals('function f() {\n  const fetch = 1;\n}\nconst crypto = 2;\n');
if (fixtureFindings.length !== 1 || fixtureFindings[0]?.name !== 'fetch') {
  failed = true;
  console.error('  FAIL the shadowing scan does not detect a shadowed local, so its passes mean nothing');
}

const sources = ['binary', 'node', 'token', 'kubeconfig'];
const shadowing: string[] = [];
for (const name of sources) {
  const file = new URL(`../src/resources/${name}.ts`, import.meta.url);
  for (const { line, name: local } of shadowedLocals(await readFile(file, 'utf8'))) {
    shadowing.push(`${name}.ts:${line} declares '${local}', which is also a global`);
  }
}
if (shadowing.length) {
  failed = true;
  console.error(`  FAIL a provider local shadows a global:\n    ${shadowing.join('\n    ')}`);
} else {
  console.log(`  ok   no provider local shadows a global (${sources.length} sources scanned)`);
}

const caught = await runsAfterSerialising(() => shadowed);
if (caught?.includes('Failed to parse URL')) {
  console.log(`  ok   the shadowing bug is still caught (${caught})`);
} else {
  failed = true;
  console.error(`  FAIL the shadowing bug was not caught (${caught ?? 'reported clean'}), so the passes above mean nothing`);
}

console.log(failed ? 'package checks: FAILED' : `package checks: ${checks.length + 3} passed`);
process.exit(failed ? 1 : 0);
