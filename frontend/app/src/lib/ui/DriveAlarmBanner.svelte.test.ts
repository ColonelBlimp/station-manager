// Drive-collapse banner — the operator-facing half of the 2026-07-29 acceptance
// criterion:
//
//   When I transmit and no RF is leaving the rig, Station Manager raises an SSE
//   alarm banner DURING the slot rather than waiting for it to end — and stays
//   silent both when the meter instrumentation itself has failed and when drive
//   is merely reduced.
//
// The daemon half (internal/bridge/drivealarm_test.go) decides WHEN to raise.
// What is pinned here is what the operator actually sees, and the load-bearing
// rules are the two SEPARATION ones (S5/S6): a drive fault and a stuck
// transmitter must never render as each other. They demand opposite responses —
// one is "your audio path died, fix it and carry on", the other is "your rig may
// be transmitting right now, go and look at it" — so confusing them at the only
// layer the operator reads would undo the point of giving them separate events.
//
// The drive alarm is a per-transmission ONE-SHOT: the daemon publishes no clear,
// because nothing it can observe proves a drive fault is over. The banner
// therefore stays until dismissed, and a NEW alarm re-shows it — the same
// dismissal contract the tx-alarm banner already has, where dismissing hides the
// warning without claiming the fault is fixed.

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import DriveAlarmBanner from './DriveAlarmBanner.svelte';
import TxAlarmBanner from './TxAlarmBanner.svelte';
import { rig, catLink } from '../operate/rig.svelte';

// Markup wrapping puts newlines mid-phrase in textContent, which is a
// formatting artefact and not part of what the operator reads.
function bannerText(el: HTMLElement): string {
    return (el.textContent ?? '').replace(/\s+/g, ' ');
}

function raiseDriveAlarm(): void {
    catLink.onDriveAlarm({ active: true, code: 'drive_no_output' });
    flushSync();
}

beforeEach(() => {
    rig.driveAlarmActive = false;
    rig.driveAlarmCode = '';
    rig.driveAlarmDismissed = false;
    rig.txAlarmActive = false;
    rig.txAlarmCode = '';
    rig.txAlarmDismissed = false;
});

afterEach(() => {
    rig.driveAlarmActive = false;
    rig.driveAlarmDismissed = false;
    rig.txAlarmActive = false;
});

describe('DriveAlarmBanner', () => {
    // S1 — nothing to report, nothing on screen. A banner that is always present
    // is a banner the operator stops reading.
    it('renders nothing until the daemon raises a drive alarm', () => {
        render(DriveAlarmBanner);
        expect(screen.queryByRole('alert')).toBeNull();
    });

    // S2 — the criterion's visible half.
    it('shows the banner when the daemon reports no RF leaving the rig', () => {
        render(DriveAlarmBanner);
        raiseDriveAlarm();
        expect(screen.getByRole('alert')).toBeTruthy();
    });

    // S3 — the operator can clear it. There is no daemon clear for this alarm,
    // so without a dismiss the banner would be permanent for the session.
    it('hides when dismissed', async () => {
        render(DriveAlarmBanner);
        raiseDriveAlarm();

        screen.getByRole('button', { name: /dismiss/i }).click();
        flushSync();
        await Promise.resolve();

        expect(screen.queryByRole('alert')).toBeNull();
    });

    // S4 — a dismissal must not silence the NEXT fault. Dismissing says "I have
    // read this one", not "stop telling me about drive collapses".
    it('re-shows on a new alarm after being dismissed', async () => {
        render(DriveAlarmBanner);
        raiseDriveAlarm();
        screen.getByRole('button', { name: /dismiss/i }).click();
        flushSync();
        await Promise.resolve();
        expect(screen.queryByRole('alert')).toBeNull();

        raiseDriveAlarm();
        expect(screen.getByRole('alert')).toBeTruthy();
    });

    // S5 — SEPARATION, and the reason these are distinct events. A drive fault
    // must not raise the stuck-TX banner: that one tells the operator their rig
    // may be transmitting and offers a safety re-check, which is the wrong
    // instruction and the wrong action for an audio-path failure.
    it('does not raise the stuck-TX banner', () => {
        render(TxAlarmBanner);
        raiseDriveAlarm();

        expect(screen.queryByText(/CHECK YOUR RADIO/i)).toBeNull();
        expect(screen.queryByRole('button', { name: /re-check/i })).toBeNull();
        expect(rig.txAlarmActive).toBe(false);
    });

    // S6 — the converse. A stuck transmitter is a safety emergency; rendering it
    // as "check your drive" would tell the operator to go and fiddle with audio
    // levels while the rig sits keyed.
    it('does not show for a stuck-TX alarm', () => {
        render(DriveAlarmBanner);
        catLink.onTxAlarm({ active: true, code: 'tx_still_keyed' });
        flushSync();

        expect(screen.queryByRole('alert')).toBeNull();
    });

    // S7 — the wording must name THIS fault. Both banners are red alerts in the
    // same shell position, so the text is the only thing distinguishing them,
    // and the operator's next action depends on reading it correctly.
    it('names the drive fault rather than a possible stuck transmission', () => {
        render(DriveAlarmBanner);
        raiseDriveAlarm();

        const text = bannerText(screen.getByRole('alert'));
        expect(text).toMatch(/no RF|drive/i);
        expect(text).not.toMatch(/still be transmitting/i);
    });

    // S8 — the wording may only claim what the detector actually establishes.
    // It fires on the ABSENCE of meter updates for the silence window, so it
    // never observes a zero reading, never waits for the transmission to end,
    // and deliberately also fires when output was present and then collapsed
    // (daemon rule D2). Text asserting an all-slot zero is therefore
    // demonstrably false in the commonest half of the cases it renders for, and
    // a diagnostic banner that lies about its own evidence sends the operator
    // looking in the wrong place.
    it('does not claim a measurement the detector never makes', () => {
        render(DriveAlarmBanner);
        raiseDriveAlarm();

        const text = bannerText(screen.getByRole('alert'));
        expect(text).not.toMatch(/whole transmission|read(s|ing)? zero|produced nothing/i);
        expect(text).toMatch(/reported nothing|no output|stopped/i);
    });

    // S9 — the banner SPANS BOTH KEY STATES, so it may not assert either one.
    // The daemon raises it mid-slot while ft8TxActive is still true (daemon rule
    // D1 requires exactly that), leaving ~9 s of a 12.6 s slot still keyed; and
    // there is no daemon clear, so it then persists after the slot ends until
    // dismissed. Present tense is false for the second half, past tense for the
    // first — a tense cannot be chosen, so the wording must describe the
    // OBSERVATION rather than the rig's current state.
    //
    // The first version of this rule asserted /was keyed/, which pinned one of
    // the two wrong answers instead of ruling out both. That is the failure mode
    // the CLAUDE.md testing section names: a failing spec, not a failing
    // implementation. Written as two negatives so no tense can satisfy it.
    it('asserts neither that the rig is still keyed nor that it has stopped', () => {
        render(DriveAlarmBanner);
        raiseDriveAlarm();

        const text = bannerText(screen.getByRole('alert'));
        // Would be false once the slot ends — and is what the STUCK-TX banner
        // means, so it blurs the S5/S6 separation in the dangerous direction.
        expect(text).not.toMatch(/\bis keyed\b|\bis transmitting\b|currently transmitting/i);
        // Would be false for the ~9 s the rig is still keyed when it first shows.
        expect(text).not.toMatch(/\bwas keyed\b|\bhas (ended|stopped|finished)\b/i);
    });
});
