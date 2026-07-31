// Drive-monitoring-off notice — operator's instruction, 2026-07-31:
//
//   "Inform the user: 'drive monitoring off - rig meter not on PO'"
//
// ACCEPTANCE CRITERION:
//
//   When my rig's meter is not on PO, the SPA tells me drive monitoring is OFF
//   and names the reason — and I can tell that apart from monitoring being ON
//   with nothing wrong, and from the drive alarm itself firing. When I move the
//   meter back to PO the notice clears BY ITSELF, unlike the alarm, which I must
//   dismiss.
//
// WHY THIS EXISTS. On 2026-07-31 the drive detector raised two NO RF OUTPUT
// alarms (03:58:36, 03:59:06) while RF was leaving the rig perfectly normally.
// The operator had moved the rig's meter to ALC — which is the meter you watch
// to set audio drive, so it is the normal way to meet the bug. The rig pushes
// RM0 = whatever meter is SELECTED, on CHANGE; a correctly-driven FT8 signal
// reads near zero on ALC, so the stream went quiet (532 samples -> 8, max gap
// 239 ms -> 9.7 s) and a gap-based detector called it dead air.
//
// The daemon fix is to stop arming in that state (internal/bridge/drivealarm.go).
// This is the other half: NOT ARMING SILENTLY would leave the operator believing
// they are covered when they are not, which is the failure the operator asked to
// close here.
//
// SELF-CLEARING IS THE LOAD-BEARING DIFFERENCE from DriveAlarmBanner, and the
// reason this is a separate component rather than a third branch inside it. A
// drive alarm has no daemon clear — nothing observable proves a drive fault is
// over — so dismissal is its only exit. This condition is the opposite: the
// daemon KNOWS when it ends, because the rig reports the meter selection, so a
// Dismiss button here would let the operator hide a warning that is still true
// and would then have to be re-shown, reinventing the alarm's contract for a
// state that does not need it. S4 pins the self-clear.
//
// The two must also never read as each other (S5): "monitoring is off" means
// nothing is being checked, "NO RF OUTPUT" means something was checked and
// failed. An operator who confuses them either ignores a real fault or hunts a
// fault that never happened.

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import DriveMonitorNotice from './DriveMonitorNotice.svelte';
import DriveAlarmBanner from './DriveAlarmBanner.svelte';
import { rig, catLink } from '../operate/rig.svelte';

function noticeText(el: HTMLElement): string {
    return (el.textContent ?? '').replace(/\s+/g, ' ');
}

beforeEach(() => {
    rig.driveMonitor = '';
    rig.driveAlarmActive = false;
    rig.driveAlarmCode = '';
    rig.driveAlarmDismissed = false;
    rig.driveAlarmAt = null;
    rig.driveAlarmRecovered = false;
});

describe('DriveMonitorNotice', () => {
    // S1 — silence by default. Before any meter selection has been seen the
    // daemon reports nothing, and a notice claiming either state would be a
    // guess. Empty is how every RigStatePayload field says "no value in this
    // frame"; the daemon deliberately never fills it in.
    it('renders nothing while the meter selection is unknown', () => {
        render(DriveMonitorNotice);
        expect(screen.queryByRole('status')).toBeNull();
    });

    // S2 — monitoring is running. The discriminator for S3: without this, an
    // implementation that renders the notice unconditionally would pass.
    it('renders nothing while the rig meter is on PO', () => {
        render(DriveMonitorNotice);
        catLink.onRigState({ driveMonitor: 'ok' });
        flushSync();
        expect(screen.queryByRole('status')).toBeNull();
    });

    // S3 — THE CRITERION. Meter not on PO: say so, and say why.
    it('tells the operator monitoring is off and names the reason', () => {
        render(DriveMonitorNotice);
        catLink.onRigState({ driveMonitor: 'meter_not_po' });
        flushSync();

        const el = screen.getByRole('status');
        const text = noticeText(el);
        expect(text).toContain('Drive monitoring off');
        // The reason has to be actionable — "monitoring is off" alone leaves the
        // operator with nothing to do about it.
        expect(text).toContain('PO');
    });

    // S4 — SELF-CLEARING, the difference from the alarm. Moving the rig's meter
    // back to PO retires the notice with no operator action. An implementation
    // that latched (copying the alarm's dismissal contract) passes S3 and fails
    // here, leaving a permanent warning about a condition that is over.
    it('clears by itself when the meter goes back to PO', () => {
        render(DriveMonitorNotice);
        catLink.onRigState({ driveMonitor: 'meter_not_po' });
        flushSync();
        expect(screen.queryByRole('status')).not.toBeNull();

        catLink.onRigState({ driveMonitor: 'ok' });
        flushSync();
        expect(screen.queryByRole('status')).toBeNull();
    });

    // S5 — SEPARATION. The notice must not read as the alarm. They demand
    // opposite responses: this one means nothing is being checked (go turn the
    // meter knob), the alarm means something WAS checked and failed (go look at
    // the audio path). Asserted on the words the operator actually reads, and on
    // the roles, so the two cannot collapse into one another.
    it('does not read as the NO RF OUTPUT alarm', () => {
        render(DriveMonitorNotice);
        catLink.onRigState({ driveMonitor: 'meter_not_po' });
        flushSync();

        const text = noticeText(screen.getByRole('status'));
        expect(text).not.toContain('NO RF OUTPUT');
        // role=status, not role=alert: this is information, not a fault.
        expect(screen.queryByRole('alert')).toBeNull();
    });

    // S6 — a rig-state frame that carries no meter selection must not wipe a
    // standing notice. Most frames (a dial push, a mode change) carry no MS tag,
    // and the merge keeps the last value for every other field; treating absent
    // as "monitoring restored" would blink the notice off on every VFO turn.
    it('survives rig-state frames that carry no meter selection', () => {
        render(DriveMonitorNotice);
        catLink.onRigState({ driveMonitor: 'meter_not_po' });
        flushSync();

        catLink.onRigState({ vfoA: 14074000 });
        flushSync();
        expect(screen.queryByRole('status')).not.toBeNull();
    });

    // S7 — both can be on screen at once and stay distinguishable. The meter was
    // on PO and output genuinely failed, then the operator moved the meter to ALC
    // to investigate: the alarm is still standing (undismissed, no daemon clear)
    // while monitoring is now off. Neither may suppress the other.
    it('coexists with a standing drive alarm without either hiding the other', () => {
        render(DriveAlarmBanner);
        render(DriveMonitorNotice);
        catLink.onDriveAlarm({ active: true, code: 'drive_no_output' });
        catLink.onRigState({ driveMonitor: 'meter_not_po' });
        flushSync();

        expect(noticeText(screen.getByRole('alert'))).toContain('NO RF OUTPUT');
        expect(noticeText(screen.getByRole('status'))).toContain('Drive monitoring off');
    });
});
