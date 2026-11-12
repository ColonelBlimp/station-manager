// W-0027 D1: the installed app ships src/ without test/ or harness/, so no shipped module may
// import them statically; a static import would fail the packaged start. The rule is ESLint's
// parser-based no-restricted-imports as an allowlist on src/ (eslint.config.mjs); this test
// proves the rule refuses every static form and that src/ passes it.
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { ESLint } from 'eslint';

const cwd = fileURLToPath(new URL('..', import.meta.url));
const RULE = 'no-restricted-imports';
const eslint = new ESLint({ cwd });

async function refused(code, filePath = 'src/probe.mjs') {
    const [r] = await eslint.lintText(code, { filePath });
    return r.messages.filter((m) => m.ruleId === RULE).length;
}

test('the rule refuses every static form of a test or harness import in src/', async () => {
    for (const code of [
        `import a from '../harness/server.mjs';`,
        `import { b } from "../test/x.mjs";`,
        `import '../test/side.mjs';`,
        `export { c } from '../harness/y.mjs';`,
        `export * from '../test/z.mjs';`,
        `export * as ns from '../test/z.mjs';`,
        `import {\n    d,\n} from '../harness/multi.mjs';`,
        `import x from 'some-package';`,
        `import y from '/abs/path.mjs';`,
        `import z from './nested/dir.mjs';`,
    ]) {
        assert.equal(await refused(code), 1, `not refused: ${code}`);
    }
});

test('the rule allows electron, node: built-ins and src/ siblings', async () => {
    const code = [
        `import fs from 'node:fs';`,
        `import { app } from 'electron';`,
        `import { trace } from './trace.mjs';`,
        `export { x } from './mode.mjs';`,
        `void import('../harness/server.mjs'); // dynamic: the development path`,
    ].join('\n');
    assert.equal(await refused(code), 0);
});

test('the rule applies to src/ only (tests may import fixtures)', async () => {
    assert.equal(await refused(`import a from '../harness/server.mjs';`, 'test/probe.mjs'), 0);
});

test('every shipped module passes the rule', async () => {
    const results = await eslint.lintFiles(['src/**/*.mjs']);
    assert.ok(
        results.some((r) => r.filePath.endsWith('/src/main.mjs')),
        'main.mjs not linted'
    );
    const bad = results.flatMap((r) =>
        r.messages.filter((m) => m.ruleId === RULE).map((m) => `${r.filePath}: ${m.message}`)
    );
    assert.deepEqual(bad, []);
});
