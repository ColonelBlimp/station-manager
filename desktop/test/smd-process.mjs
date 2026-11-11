// A child process owned from spawn until it has exited (W-0027 C1d; codex-070): the real-smd
// acceptance starts and stops smd with it. The exit is tracked from spawn time; a spawn failure
// or an early exit is a reported start failure, never a hang; stop() sends SIGTERM, escalates to
// SIGKILL after the grace, and returns only once the process has actually exited (a signal sent
// is not a process stopped). A signal-terminated child has a null exitCode and a signalCode, so
// completion is taken from the 'exit' event, not from exitCode. An 'error' event means a failed
// spawn only before the 'spawn' event; afterwards it is a failed operation on a live child (a
// signal that could not be delivered), recorded and never taken as an exit (codex-071).
import { spawn } from 'node:child_process';
import fs from 'node:fs';

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

/** Resolves true if `p` settles within `ms`, false otherwise. */
const within = (p, ms) => Promise.race([p.then(() => true), sleep(ms).then(() => false)]);

export function managedProcess(
    bin,
    args,
    { env, ready, readyTimeout = 30000, termGrace = 15000, killGrace = 5000, onSpawn } = {}
) {
    let child = null;
    let exited = null;
    let log = '';
    const errors = [];
    return {
        get log() {
            return log;
        },
        get pid() {
            return child?.pid;
        },
        get running() {
            return child !== null;
        },
        /** Errors reported for the live child after it spawned (codes only). */
        get errors() {
            return [...errors];
        },
        /** Spawn and wait until `ready()` is true. Rejects (with the child reaped) on a spawn
         *  failure, an early exit or the ready timeout. */
        async start() {
            if (child) throw new Error('already running');
            const c = spawn(bin, args, { env, stdio: ['ignore', 'pipe', 'pipe'] });
            const done = new Promise((resolve) => {
                let spawned = false;
                c.once('spawn', () => (spawned = true));
                c.on('error', (e) => {
                    if (!spawned)
                        resolve({ error: e.code ?? 'error' }); // it never started
                    else errors.push(e.code ?? 'error'); // a failed operation; it still runs
                });
                c.once('exit', (code, signal) => resolve({ code, signal }));
            });
            onSpawn?.(c); // a test seam: lets a test make signal delivery fail
            c.stdout?.on('data', (d) => (log += d));
            c.stderr?.on('data', (d) => (log += d));
            child = c;
            exited = done;
            let gone = false;
            void done.then(() => (gone = true));
            const end = Date.now() + readyTimeout;
            while (!gone && Date.now() < end) {
                if (await ready().catch(() => false)) return;
                await sleep(150);
            }
            if (!gone) await this.stop(); // the ready timeout: reap it before reporting
            const outcome = await done;
            child = null;
            exited = null;
            throw new Error(`${bin} did not start: ${JSON.stringify(outcome)}`);
        },
        /** Stop and return the exit ({ code, signal } or { error }) once the process has exited. */
        async stop() {
            if (!child) return null;
            const c = child;
            const done = exited;
            c.kill('SIGTERM');
            if (!(await within(done, termGrace))) {
                c.kill('SIGKILL');
                if (!(await within(done, killGrace))) {
                    throw new Error(`${bin} (pid ${c.pid}) did not exit after SIGKILL`);
                }
            }
            child = null;
            exited = null;
            return done;
        },
    };
}

/**
 * Stop `proc`, then remove `dir`, only once the exit is confirmed. If termination cannot be
 * established, nothing is removed: the result names the kept directory and the child's pid, so the
 * caller can fail and report them (codex-071).
 */
export async function releaseAfter(proc, dir) {
    const pid = proc.pid;
    try {
        await proc.stop();
    } catch (e) {
        return { ok: false, kept: dir, pid, reason: e.message };
    }
    if (dir) fs.rmSync(dir, { recursive: true, force: true });
    return { ok: true };
}
