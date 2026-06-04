import { describe, it, expect, beforeEach } from 'vitest';
import { flushSync } from 'svelte';
import { qsoEditState, type DaemonQsoForEdit } from './qsoEdit.svelte';

/**
 * qsoEditState — independent edit-overlay singleton.
 *
 * Verifies the contract SessionPanel + QsoEditOverlay rely on:
 *   - populate maps daemon canonical date / time formats to the
 *     SPA-friendly forms the form components expect.
 *   - beginOpen + populate flip open/loading correctly.
 *   - close resets every field so a stale value can't leak.
 *   - toPatchBody emits only the editable fields.
 *
 * Wire shape note: types.Qso embeds QsoDetails / ContactedStation as
 * anonymous structs, so encoding/json promotes their fields to the
 * top level. Every test fixture below uses the FLAT shape — `call`,
 * `band`, `freq` etc. are siblings of `uuid`, not nested under
 * `contacted_station` / `qso_details`. An earlier nested-shape draft
 * silently passed unit tests but failed in production because the
 * wire didn't match.
 */

const makeDaemonQso = (overrides: Partial<DaemonQsoForEdit> = {}): DaemonQsoForEdit => ({
    uuid: '01910d3a-7000-7abc-8def-0123456789ab',
    app_sm_request_qsl: true,
    call: 'M0XYZ',
    name: 'Marc',
    qth: 'London',
    country: 'England',
    rst_sent: '59',
    rst_rcvd: '57',
    mode: 'SSB',
    submode: 'USB',
    freq: '14.250',
    freq_rx: '14.260',
    band: '20m',
    qso_date: '20260509',
    time_on: '1432',
    time_off: '1438',
    comment: 'great copy',
    rig: 'IC-7300',
    rx_pwr: '100',
    notes: 'first contact',
    ...overrides,
});

describe('qsoEditState', () => {
    beforeEach(() => {
        qsoEditState.close();
        flushSync();
    });

    describe('populate', () => {
        it('hydrates the working copy from a daemon GET response', () => {
            qsoEditState.populate(makeDaemonQso());
            flushSync();

            expect(qsoEditState.uuid).toBe('01910d3a-7000-7abc-8def-0123456789ab');
            expect(qsoEditState.callsign).toBe('M0XYZ');
            expect(qsoEditState.name).toBe('Marc');
            expect(qsoEditState.qth).toBe('London');
            expect(qsoEditState.country).toBe('England');
            expect(qsoEditState.rstSent).toBe('59');
            expect(qsoEditState.rstRcvd).toBe('57');
            expect(qsoEditState.mode).toBe('USB');
            expect(qsoEditState.freq).toBe('14.250');
            expect(qsoEditState.freqRx).toBe('14.260');
            expect(qsoEditState.band).toBe('20m');
            expect(qsoEditState.comment).toBe('great copy');
            expect(qsoEditState.rig).toBe('IC-7300');
            expect(qsoEditState.rxPwr).toBe('100');
            expect(qsoEditState.notes).toBe('first contact');
            expect(qsoEditState.requestQsl).toBe(true);
            expect(qsoEditState.open).toBe(true);
            expect(qsoEditState.loading).toBe(false);
        });

        it('converts ADIF YYYYMMDD → YYYY-MM-DD for date inputs', () => {
            qsoEditState.populate(makeDaemonQso());
            flushSync();
            expect(qsoEditState.qsoDate).toBe('2026-05-09');
        });

        it('converts ADIF HHMM → HH:MM for time inputs', () => {
            qsoEditState.populate(makeDaemonQso());
            flushSync();
            expect(qsoEditState.timeOn).toBe('14:32');
            expect(qsoEditState.timeOff).toBe('14:38');
        });

        it('passes through already-dashed dates / colon-separated times', () => {
            qsoEditState.populate(
                makeDaemonQso({
                    qso_date: '2026-05-09',
                    time_on: '14:32',
                    time_off: '14:38',
                })
            );
            flushSync();
            expect(qsoEditState.qsoDate).toBe('2026-05-09');
            expect(qsoEditState.timeOn).toBe('14:32');
            expect(qsoEditState.timeOff).toBe('14:38');
        });

        it('drops seconds from HHMMSS time strings (TimeInput is HH:MM only)', () => {
            qsoEditState.populate(
                makeDaemonQso({
                    time_on: '143215',
                    time_off: '143845',
                })
            );
            flushSync();
            expect(qsoEditState.timeOn).toBe('14:32');
            expect(qsoEditState.timeOff).toBe('14:38');
        });

        it('handles missing fields with empty defaults', () => {
            qsoEditState.populate({});
            flushSync();
            expect(qsoEditState.uuid).toBe('');
            expect(qsoEditState.callsign).toBe('');
            expect(qsoEditState.name).toBe('');
            expect(qsoEditState.qsoDate).toBe('');
            expect(qsoEditState.freqRx).toBe('');
            expect(qsoEditState.rig).toBe('');
            expect(qsoEditState.rxPwr).toBe('');
            expect(qsoEditState.notes).toBe('');
            expect(qsoEditState.requestQsl).toBe(false);
        });

        it('honours app_sm_request_qsl=false from the daemon', () => {
            qsoEditState.populate(makeDaemonQso({ app_sm_request_qsl: false }));
            flushSync();
            expect(qsoEditState.requestQsl).toBe(false);
        });
    });

    describe('beginOpen', () => {
        it('flips open + loading without touching field state', () => {
            qsoEditState.callsign = 'PRE-EXISTING';
            qsoEditState.beginOpen('test-uuid');
            flushSync();
            expect(qsoEditState.open).toBe(true);
            expect(qsoEditState.loading).toBe(true);
            expect(qsoEditState.uuid).toBe('test-uuid');
            // Fields preserved from prior state — populate() will overwrite
            // them; beginOpen only handles the opening transition.
            expect(qsoEditState.callsign).toBe('PRE-EXISTING');
        });
    });

    describe('close', () => {
        it('resets every field to empty + flips open/loading/saving false', () => {
            qsoEditState.populate(makeDaemonQso());
            flushSync();
            expect(qsoEditState.open).toBe(true);

            qsoEditState.close();
            flushSync();

            expect(qsoEditState.open).toBe(false);
            expect(qsoEditState.loading).toBe(false);
            expect(qsoEditState.saving).toBe(false);
            expect(qsoEditState.uuid).toBe('');
            expect(qsoEditState.callsign).toBe('');
            expect(qsoEditState.name).toBe('');
            expect(qsoEditState.qth).toBe('');
            expect(qsoEditState.country).toBe('');
            expect(qsoEditState.comment).toBe('');
            expect(qsoEditState.mode).toBe('');
            expect(qsoEditState.freq).toBe('');
            expect(qsoEditState.freqRx).toBe('');
            expect(qsoEditState.band).toBe('');
            expect(qsoEditState.rstSent).toBe('');
            expect(qsoEditState.rstRcvd).toBe('');
            expect(qsoEditState.qsoDate).toBe('');
            expect(qsoEditState.qsoDateOff).toBe('');
            expect(qsoEditState.timeOn).toBe('');
            expect(qsoEditState.timeOff).toBe('');
            expect(qsoEditState.rig).toBe('');
            expect(qsoEditState.rxPwr).toBe('');
            expect(qsoEditState.notes).toBe('');
            expect(qsoEditState.requestQsl).toBe(false);
        });
    });

    describe('toPatchBody', () => {
        it('emits the editable fields as a flat body matching the daemon wire shape', () => {
            qsoEditState.populate(makeDaemonQso());
            flushSync();
            const body = qsoEditState.toPatchBody();
            expect(body.uuid).toBeUndefined();
            expect(body).toEqual({
                app_sm_request_qsl: true,
                call: 'M0XYZ',
                name: 'Marc',
                qth: 'London',
                country: 'England',
                rst_sent: '59',
                rst_rcvd: '57',
                mode: 'SSB',
                submode: 'USB',
                freq: '14.250',
                freq_rx: '14.260',
                band: '20m',
                qso_date: '2026-05-09',
                qso_date_off: '',
                time_on: '14:32',
                time_off: '14:38',
                comment: 'great copy',
                rig: 'IC-7300',
                rx_pwr: '100',
                notes: 'first contact',
            });
        });

        it('reflects post-populate operator edits', () => {
            qsoEditState.populate(makeDaemonQso());
            flushSync();
            qsoEditState.callsign = 'M0EDIT';
            qsoEditState.comment = 'edited comment';
            qsoEditState.rig = 'FT-991A';
            qsoEditState.rxPwr = '5';
            qsoEditState.notes = 'updated notes';
            qsoEditState.requestQsl = false;
            const body = qsoEditState.toPatchBody();
            expect(body.call).toBe('M0EDIT');
            expect(body.comment).toBe('edited comment');
            expect(body.rig).toBe('FT-991A');
            expect(body.rx_pwr).toBe('5');
            expect(body.notes).toBe('updated notes');
            expect(body.app_sm_request_qsl).toBe(false);
        });

        // ---- H3: MODE/SUBMODE round-trip (review 2026-06-04) ----

        it('shows the operator-facing submode and resolves it back to the ADIF pair', () => {
            // Fixture is a phone QSO stored as MODE=SSB SUBMODE=USB.
            qsoEditState.populate(makeDaemonQso());
            flushSync();
            expect(qsoEditState.mode).toBe('USB'); // dropdown shows the sideband, not "SSB"
            const body = qsoEditState.toPatchBody();
            expect(body.mode).toBe('SSB');
            expect(body.submode).toBe('USB');
        });

        it('shows the parent mode + empty submode for a submode-less mode', () => {
            qsoEditState.populate(makeDaemonQso({ mode: 'CW', submode: '' }));
            flushSync();
            expect(qsoEditState.mode).toBe('CW');
            const body = qsoEditState.toPatchBody();
            expect(body.mode).toBe('CW');
            expect(body.submode).toBe('');
        });

        it('clears a stale submode when the operator changes the mode (corruption fix)', () => {
            qsoEditState.populate(makeDaemonQso()); // SSB + USB → shows "USB"
            flushSync();
            qsoEditState.mode = 'CW'; // operator switches phone → CW
            const body = qsoEditState.toPatchBody();
            expect(body.mode).toBe('CW');
            // submode is sent as "" (present, not omitted) so the daemon's
            // PATCH-merge clears the old USB rather than preserving it.
            expect(body.submode).toBe('');
        });
    });
});
