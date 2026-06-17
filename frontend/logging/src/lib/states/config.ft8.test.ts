import { describe, expect, it } from 'vitest';
import { configState } from './config.svelte';
import type { ConfigResponse } from '../api/config';

// Minimal /v1/config response — applyResponse only derefs these blocks (the rest
// is optional-chained / ?? defaulted).
function baseResp(overrides: Partial<ConfigResponse> = {}): ConfigResponse {
    return {
        setup_complete: true,
        logging_station: {},
        default_logbook: { id: 1 },
        default_rig: { id: 1 },
        station: {},
        bridge: {},
        mailer: { enabled: false },
        ...overrides,
    } as ConfigResponse;
}

describe('configState ft8Display', () => {
    it('hydrates from the ft8_display block', () => {
        configState.applyResponse(
            baseResp({
                ft8_display: {
                    history_max: 250,
                    feed_mode: 'single',
                    highlight_unworked: '#abcdef',
                    highlight_worked: '#123456',
                    cq_to_top: true,
                },
            })
        );
        expect(configState.ft8Display.historyMax).toBe(250);
        expect(configState.ft8Display.feedMode).toBe('single');
        expect(configState.ft8Display.highlightUnworked).toBe('#abcdef');
        expect(configState.ft8Display.highlightWorked).toBe('#123456');
        expect(configState.ft8Display.cqToTop).toBe(true);
    });

    it('falls back to defaults when ft8_display is absent (older daemon)', () => {
        configState.applyResponse(baseResp({ ft8_display: undefined }));
        expect(configState.ft8Display.historyMax).toBe(100);
        expect(configState.ft8Display.feedMode).toBe('accumulate');
        expect(configState.ft8Display.highlightUnworked).toBe('#15803d');
        expect(configState.ft8Display.highlightWorked).toBe('#9ca3af');
        expect(configState.ft8Display.cqToTop).toBe(false);
    });
});

describe('configState PUT payload helpers', () => {
    it('toLoggingStationFields/toStationFields reflect current state', () => {
        configState.applyResponse(
            baseResp({
                logging_station: { station_callsign: 'G0TST', my_gridsquare: 'IO91' },
                station: { amp_enabled: true, amp_multiplier: 5, default_power: 100 },
            })
        );
        const ls = configState.toLoggingStationFields();
        expect(ls.station_callsign).toBe('G0TST');
        expect(ls.my_gridsquare).toBe('IO91');

        const st = configState.toStationFields();
        expect(st.amp_enabled).toBe(true);
        expect(st.amp_multiplier).toBe(5);
        expect(st.default_power).toBe(100);
    });
});
