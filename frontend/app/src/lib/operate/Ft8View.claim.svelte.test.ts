// ADR 0080 (W-0019 slice 4): a refused profile claim keeps the STOP paths
// reachable. stopFt8 cleared the old tx/qso frames, so the Operate anchor's own
// Disable/Abandon controls cannot act — the banner offers Disable TX for an
// armed or in-flight refusal, and Abandon (which disarms too) for an active
// session; each acts on the daemon and the view re-claims at once.
//
// The fake daemon below has the claim's REAL precedence and the real stop
// semantics (internal/ft8 ClaimProfile, AbandonQso, disarmTx): busy → in flight
// → session active → armed; an abandon ends the session but leaves TX ARMED and
// the cancelled rung still returning; a disarm cancels, waits for the rung,
// ends the session and closes the device. A fixture that granted the second
// claim unconditionally would hide that an abandon alone can never open the
// stream (operator review, 2026-09-11).
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, within, cleanup } from '@testing-library/svelte';
import Ft8View from './Ft8View.svelte';
import { router, setMode } from '../router.svelte';
import { rig } from './rig.svelte';
import {
    ft8State,
    resetFt8ForTests,
    setFt8Transport,
    setFt8Claimer,
    setFt8TxActions,
    type ClaimResult,
} from './ft8.svelte';

const ABANDON_LABEL = 'Abandon session and disable TX';

type Daemon = { busy: boolean; inFlight: boolean; session: boolean; armed: boolean };

const MESSAGES: Record<string, string> = {
    ft8_profile_busy: 'another subscriber holds a capture on the other profile',
    ft8_tx_in_flight: 'a transmission is in flight; the profile changes only between sessions',
    ft8_session_active: 'a sequenced session is active; the profile changes only between sessions',
    ft8_tx_armed: 'FT8 transmit is armed; disarm before changing profile',
};

/** Wire the fake daemon: TX actions mutate it the way the real ones do, and the
 *  claimer refuses in the real precedence. `log` records every call in order;
 *  `afterDisarm` lets a case change the daemon between the disarm and the
 *  re-claim (another subscriber arriving, say). */
function wireDaemon(d: Daemon, log: string[], afterDisarm?: (d: Daemon) => void): void {
    const ok = () => Promise.resolve({ ok: true, message: '' });
    setFt8TxActions({
        arm: (armed) => {
            log.push(`arm:${armed}`);
            if (armed) {
                d.armed = true;
            } else {
                // disarmTx: cancel the rung, WAIT for it, abandon, close the device —
                // all before the 202.
                d.inFlight = false;
                d.session = false;
                d.armed = false;
                afterDisarm?.(d);
            }
            return Promise.resolve({ kind: 'accepted' });
        },
        callCq: ok,
        answerCq: ok,
        workCaller: ok,
        stopAutoWork: ok,
        bagAnswerer: ok,
        unbagAnswerer: ok,
        resumeDrain: ok,
        pickAnswerer: ok,
        abandon: () => {
            log.push('abandon');
            // AbandonQso: the session ends and the rung on the air is cancelled,
            // but TX stays ARMED, and the cancelled goroutine clears in-flight on
            // its own time — a rung that started after the refusal is still
            // returning here (the daemon answers the abandon before it does).
            d.session = false;
            d.inFlight = d.armed;
            return ok();
        },
        skip: ok,
        next: ok,
    });
    setFt8Claimer((mode): Promise<ClaimResult> => {
        log.push(`claim:${mode}`);
        const code = d.busy
            ? 'ft8_profile_busy'
            : d.inFlight
              ? 'ft8_tx_in_flight'
              : d.session
                ? 'ft8_session_active'
                : d.armed
                  ? 'ft8_tx_armed'
                  : '';
        return Promise.resolve(
            code === ''
                ? { kind: 'ok', mode: mode.toUpperCase() }
                : { kind: 'refused', code, message: MESSAGES[code], retryAfterMs: 0 }
        );
    });
}

describe('Ft8View — stop paths through a refused claim', () => {
    let log: string[] = [];
    let opened: (string | undefined)[] = [];

    beforeEach(() => {
        resetFt8ForTests();
        log = [];
        opened = [];
        setFt8Transport((h, mode) => {
            opened.push(mode);
            h.onOpen();
            return () => undefined;
        });
        rig.cat = 'connected';
        router.view = 'operate';
        setMode('ft4');
    });

    afterEach(() => {
        cleanup();
        setMode('phone');
    });

    it('ft8_tx_armed: Disable TX disarms the daemon and re-claims', async () => {
        wireDaemon({ busy: false, inFlight: false, session: false, armed: true }, log);
        render(Ft8View);

        const banner = await screen.findByTestId('ft8-claim-banner');
        expect(within(banner).getByText(MESSAGES.ft8_tx_armed)).toBeTruthy();
        expect(within(banner).queryByRole('button', { name: ABANDON_LABEL })).toBeNull();
        await fireEvent.click(within(banner).getByRole('button', { name: 'Disable TX' }));

        await vi.waitFor(() => expect(opened).toEqual(['ft4']));
        expect(log).toEqual(['claim:ft4', 'arm:false', 'claim:ft4']);
        expect(ft8State.claimRefusal).toBeNull();
        expect(ft8State.claimed).toBe(true);
    });

    it('ft8_tx_in_flight: Disable TX cancels the rung, waits for it and re-claims', async () => {
        wireDaemon({ busy: false, inFlight: true, session: false, armed: true }, log);
        render(Ft8View);

        const banner = await screen.findByTestId('ft8-claim-banner');
        expect(within(banner).getByText(MESSAGES.ft8_tx_in_flight)).toBeTruthy();
        expect(within(banner).queryByRole('button', { name: ABANDON_LABEL })).toBeNull();
        await fireEvent.click(within(banner).getByRole('button', { name: 'Disable TX' }));

        await vi.waitFor(() => expect(opened).toEqual(['ft4']));
        expect(log).toEqual(['claim:ft4', 'arm:false', 'claim:ft4']);
        expect(ft8State.claimed).toBe(true);
    });

    it('ft8_session_active: Abandon drops the contact, then disarms, then re-claims', async () => {
        // The daemon leaves TX armed after an abandon, and the rung it cancelled
        // is still returning, so an abandon alone would be refused again
        // (ft8_tx_in_flight, then ft8_tx_armed) — the action disarms before the
        // re-claim, in that order.
        const d: Daemon = { busy: false, inFlight: false, session: true, armed: true };
        wireDaemon(d, log);
        render(Ft8View);

        const banner = await screen.findByTestId('ft8-claim-banner');
        expect(within(banner).getByText(MESSAGES.ft8_session_active)).toBeTruthy();
        expect(within(banner).getByRole('button', { name: 'Disable TX' })).toBeEnabled();
        await fireEvent.click(within(banner).getByRole('button', { name: ABANDON_LABEL }));

        await vi.waitFor(() => expect(opened).toEqual(['ft4']));
        expect(log).toEqual(['claim:ft4', 'abandon', 'arm:false', 'claim:ft4']);
        expect(d).toEqual({ busy: false, inFlight: false, session: false, armed: false });
        expect(ft8State.claimRefusal).toBeNull();
        expect(ft8State.claimed).toBe(true);
    });

    it('ft8_session_active: Disable TX alone ends the session too (the daemon disarm abandons)', async () => {
        wireDaemon({ busy: false, inFlight: false, session: true, armed: true }, log);
        render(Ft8View);

        const banner = await screen.findByTestId('ft8-claim-banner');
        await fireEvent.click(within(banner).getByRole('button', { name: 'Disable TX' }));

        await vi.waitFor(() => expect(opened).toEqual(['ft4']));
        expect(log).toEqual(['claim:ft4', 'arm:false', 'claim:ft4']);
    });

    it('a re-claim refused after the stop action shows the new reason, and opens nothing', async () => {
        // The stop cleared TX, but another subscriber took a capture meanwhile:
        // the banner tells the truth about the second refusal instead of opening.
        const d: Daemon = { busy: false, inFlight: false, session: false, armed: true };
        wireDaemon(d, log, (daemon) => (daemon.busy = true));
        render(Ft8View);

        const banner = await screen.findByTestId('ft8-claim-banner');
        await fireEvent.click(within(banner).getByRole('button', { name: 'Disable TX' }));

        await vi.waitFor(() => expect(log).toEqual(['claim:ft4', 'arm:false', 'claim:ft4']));
        const again = await screen.findByTestId('ft8-claim-banner');
        await vi.waitFor(() =>
            expect(within(again).getByText(MESSAGES.ft8_profile_busy)).toBeTruthy()
        );
        expect(within(again).queryByRole('button')).toBeNull();
        expect(opened).toEqual([]);
        expect(ft8State.claimed).toBe(false);
    });

    it('a stop action resolving after the view is gone re-claims nothing (codex 67cc1b96 P2)', async () => {
        const d: Daemon = { busy: false, inFlight: false, session: false, armed: true };
        wireDaemon(d, log);
        let release: (() => void) | null = null;
        setFt8TxActions({
            arm: (armed) => {
                log.push(`arm:${armed}`);
                return new Promise((resolve) => {
                    release = () => {
                        d.armed = armed;
                        resolve({ kind: 'accepted' });
                    };
                });
            },
            callCq: () => Promise.resolve({ ok: true, message: '' }),
            answerCq: () => Promise.resolve({ ok: true, message: '' }),
            workCaller: () => Promise.resolve({ ok: true, message: '' }),
            stopAutoWork: () => Promise.resolve({ ok: true, message: '' }),
            bagAnswerer: () => Promise.resolve({ ok: true, message: '' }),
            unbagAnswerer: () => Promise.resolve({ ok: true, message: '' }),
            resumeDrain: () => Promise.resolve({ ok: true, message: '' }),
            pickAnswerer: () => Promise.resolve({ ok: true, message: '' }),
            abandon: () => Promise.resolve({ ok: true, message: '' }),
            skip: () => Promise.resolve({ ok: true, message: '' }),
            next: () => Promise.resolve({ ok: true, message: '' }),
        });
        const view = render(Ft8View);

        const banner = await screen.findByTestId('ft8-claim-banner');
        await fireEvent.click(within(banner).getByRole('button', { name: 'Disable TX' }));
        await vi.waitFor(() => expect(release).not.toBeNull());

        view.unmount(); // the operator left for Phone/CW while the daemon was disarming
        release!();
        await Promise.resolve();
        await Promise.resolve();
        await new Promise((r) => setTimeout(r, 0));
        expect(log).toEqual(['claim:ft4', 'arm:false']);
        expect(opened).toEqual([]);
        expect(ft8State.claimed).toBe(false);
    });

    it('ft8_profile_busy: no stop path — another subscriber holds the capture', async () => {
        wireDaemon({ busy: true, inFlight: false, session: false, armed: false }, log);
        render(Ft8View);

        const banner = await screen.findByTestId('ft8-claim-banner');
        expect(within(banner).queryByRole('button')).toBeNull();
        expect(opened).toEqual([]);
        expect(log).toEqual(['claim:ft4']);
    });
});
