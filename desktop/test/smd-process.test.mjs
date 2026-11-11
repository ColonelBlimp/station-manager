// The real-smd acceptance's process ownership (codex-070), with synthetic children only.
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { managedProcess, releaseAfter } from './smd-process.mjs';

const node = process.execPath;
const alive = (pid) => {
    try {
        process.kill(pid, 0);
        return true;
    } catch {
        return false;
    }
};

test('a spawn failure is a reported start failure, not a hang', async () => {
    const p = managedProcess('/nonexistent/smd', [], {
        ready: async () => false,
        readyTimeout: 5000,
    });
    await assert.rejects(p.start(), /did not start: .*ENOENT/);
    assert.equal(p.running, false);
});

test('an early exit is a reported start failure with its code', async () => {
    const p = managedProcess(node, ['-e', 'process.exit(3)'], { ready: async () => false });
    await assert.rejects(p.start(), /did not start: .*"code":3/);
    assert.equal(p.running, false);
});

test('a ready timeout reaps the child before reporting', async () => {
    const p = managedProcess(node, ['-e', 'setInterval(() => {}, 1000)'], {
        ready: async () => false,
        readyTimeout: 400,
        termGrace: 2000,
    });
    let pid;
    const started = p.start();
    await new Promise((r) => setTimeout(r, 100));
    pid = p.pid;
    await assert.rejects(started, /did not start: .*"signal":"SIGTERM"/);
    assert.equal(alive(pid), false, 'the timed-out child is still running');
});

test('stop() returns only after a normal exit on SIGTERM', async () => {
    const p = managedProcess(node, ['-e', 'console.log("ready"); setInterval(() => {}, 1000)'], {
        ready: async () => p.log.includes('ready'),
    });
    await p.start();
    const pid = p.pid;
    const exit = await p.stop();
    assert.deepEqual(exit, { code: null, signal: 'SIGTERM' });
    assert.equal(alive(pid), false);
    assert.equal(p.running, false);
});

test('a child that ignores SIGTERM is killed, and stop() awaits its exit', async () => {
    const p = managedProcess(
        node,
        [
            '-e',
            'process.on("SIGTERM", () => {}); console.log("ready"); setInterval(() => {}, 1000)',
        ],
        { ready: async () => p.log.includes('ready'), termGrace: 500, killGrace: 3000 }
    );
    await p.start();
    const pid = p.pid;
    const exit = await p.stop();
    assert.deepEqual(exit, { code: null, signal: 'SIGKILL' }, 'not escalated to SIGKILL');
    assert.equal(alive(pid), false, 'stop() returned before the child exited');
});

test('stop() on a child that already died returns its exit', async () => {
    const p = managedProcess(
        node,
        ['-e', 'console.log("ready"); setTimeout(() => process.exit(0), 300)'],
        {
            ready: async () => p.log.includes('ready'),
        }
    );
    await p.start();
    await new Promise((r) => setTimeout(r, 600));
    assert.deepEqual(await p.stop(), { code: 0, signal: null });
});

// codex-071: an error after spawn (here a signal that could not be delivered) is not an exit.
// The child must stay owned, stop() must fail, and nothing may report it reaped.
async function reap(pid) {
    if (!alive(pid)) return;
    process.kill(pid, 'SIGKILL');
    for (let i = 0; i < 50 && alive(pid); i++) await new Promise((r) => setTimeout(r, 50));
}

test('an error after spawn never counts as an exit: the child stays owned and stop() fails', async () => {
    let pid;
    try {
        const p = managedProcess(
            node,
            ['-e', 'console.log("ready"); setInterval(() => {}, 1000)'],
            {
                ready: async () => p.log.includes('ready'),
                termGrace: 300,
                killGrace: 300,
                onSpawn: (c) => {
                    // Signals "fail": an EPERM error instead of delivery.
                    c.kill = () => {
                        const e = new Error('synthetic EPERM');
                        e.code = 'EPERM';
                        c.emit('error', e);
                        return false;
                    };
                },
            }
        );
        await p.start();
        pid = p.pid;
        await assert.rejects(p.stop(), /did not exit after SIGKILL/);
        assert.equal(p.running, true, 'ownership was given up while the child lives');
        assert.equal(alive(pid), true, 'the test could not keep its child alive');
        assert.deepEqual(p.errors, ['EPERM', 'EPERM'], 'the delivery errors were not recorded');
    } finally {
        await reap(pid); // this test reaps the exact child it made
    }
    assert.equal(alive(pid), false);
});

test('releaseAfter keeps the scratch directory and reports it when termination is unconfirmed', async () => {
    const fs = await import('node:fs');
    const os = await import('node:os');
    const path = await import('node:path');
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'sm-c1d-release-'));
    let pid;
    try {
        const p = managedProcess(
            node,
            ['-e', 'console.log("ready"); setInterval(() => {}, 1000)'],
            {
                ready: async () => p.log.includes('ready'),
                termGrace: 200,
                killGrace: 200,
                onSpawn: (c) => {
                    c.kill = () => false; // nothing is delivered and no exit follows
                },
            }
        );
        await p.start();
        pid = p.pid;
        const r = await releaseAfter(p, dir);
        assert.equal(r.ok, false);
        assert.equal(r.kept, dir);
        assert.equal(r.pid, pid);
        assert.ok(
            fs.existsSync(dir),
            'the scratch directory was removed before termination was confirmed'
        );
    } finally {
        await reap(pid);
        fs.rmSync(dir, { recursive: true, force: true }); // exactly the directory this test made
    }
});

test('releaseAfter removes the directory once the exit is confirmed', async () => {
    const fs = await import('node:fs');
    const os = await import('node:os');
    const path = await import('node:path');
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'sm-c1d-release-'));
    const p = managedProcess(node, ['-e', 'console.log("ready"); setInterval(() => {}, 1000)'], {
        ready: async () => p.log.includes('ready'),
    });
    await p.start();
    const pid = p.pid;
    const r = await releaseAfter(p, dir);
    assert.equal(r.ok, true);
    assert.equal(fs.existsSync(dir), false);
    assert.equal(alive(pid), false);
});
