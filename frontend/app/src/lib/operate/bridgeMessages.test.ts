import { describe, expect, it } from 'vitest';
import { disconnectMessage, bridgeErrorMessage } from './bridgeMessages';

/*
    Ported bridge wording (W-0003 AC3) — the app must render the friendly,
    operator-actionable text the retired logging SPA carried, not the raw daemon
    codes. Unknown codes must still surface (raw fallback), and {token} slots must
    fill from the event's details.
*/

describe('disconnectMessage', () => {
    it('renders the friendly text for a known rig-disconnected code', () => {
        expect(disconnectMessage('rig_no_data')).toBe('The rig has gone quiet — is it powered on?');
        expect(disconnectMessage('serial_port_error')).toBe(
            'Lost the connection to the rig — check it is powered on and the cable is connected.'
        );
    });

    it('falls back to the raw code (+ details) for an unknown code', () => {
        expect(disconnectMessage('some_new_code')).toBe('some_new_code');
        expect(disconnectMessage('some_new_code', { port: '/dev/ttyUSB0' })).toBe(
            'some_new_code (/dev/ttyUSB0)'
        );
    });
});

describe('bridgeErrorMessage', () => {
    it('renders the friendly text for a known bridge-error code', () => {
        expect(bridgeErrorMessage('serial_port_not_found')).toContain('was not found');
    });

    it('substitutes {token} placeholders from details', () => {
        expect(bridgeErrorMessage('serial_permission_denied', { port: '/dev/ttyUSB0' })).toContain(
            'serial port /dev/ttyUSB0'
        );
        expect(
            bridgeErrorMessage('identity_mismatch', {
                driver: 'ftdx10',
                expected: '0761',
                actual: '0650',
            })
        ).toBe('Configured driver is "ftdx10" (0761), but the rig identifies as "0650"');
    });

    it('leaves a token verbatim when its detail is missing (visible gap, not dropped)', () => {
        expect(bridgeErrorMessage('serial_open_failed')).toContain('{port}');
    });

    it('falls back to the raw code (+ details) for an unknown code', () => {
        expect(bridgeErrorMessage('brand_new_fault', { error: 'boom' })).toBe(
            'brand_new_fault (boom)'
        );
    });
});
