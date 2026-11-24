// The words of the banner a refused FT8/FT4 claim shows (operator, 2026-11-24: the daemon's
// own wording, "another subscriber holds a capture on the other profile", means nothing to
// anyone but its developer).
import { describe, expect, it } from 'vitest';
import { claimRefusalText } from './claimRefusal';

const DAEMON = 'another subscriber holds a capture on the other profile';

describe('claimRefusalText', () => {
    it('says what is in the way, in the operator\u2019s terms, naming both modes', () => {
        expect(claimRefusalText('ft8_profile_busy', 'FT4', DAEMON, false)).toBe(
            'FT8 is still open, and only one of FT8 and FT4 can be open at a time.'
        );
        expect(claimRefusalText('ft8_tx_in_flight', 'FT4', 'x', false)).toBe(
            'FT8 is still busy with a transmission. Disable TX to change to FT4.'
        );
        expect(claimRefusalText('ft8_session_active', 'FT4', 'x', false)).toBe(
            'A contact or run is in progress on FT8. End it to change to FT4.'
        );
        expect(claimRefusalText('ft8_tx_armed', 'FT4', 'x', false)).toBe(
            'Transmit is enabled on FT8. Disable TX to change to FT4.'
        );
        expect(claimRefusalText('archive_switch_pending', 'FT4', 'x', false)).toBe(
            'The archive is being switched and Station Manager is restarting.'
        );
    });

    it('swaps the two modes when FT8 is the one wanted', () => {
        expect(claimRefusalText('ft8_profile_busy', 'FT8', DAEMON, false)).toBe(
            'FT4 is still open, and only one of FT8 and FT4 can be open at a time.'
        );
        expect(claimRefusalText('ft8_tx_armed', 'FT8', 'x', false)).toBe(
            'Transmit is enabled on FT4. Disable TX to change to FT8.'
        );
    });

    it('tells the operator to try again only where there is a button to try again with', () => {
        const withButton = claimRefusalText('ft8_profile_busy', 'FT4', DAEMON, true);
        expect(withButton).toMatch(/If you have just switched, try again in a few seconds\.$/);
        expect(claimRefusalText('ft8_profile_busy', 'FT4', DAEMON, false)).not.toMatch(/try again/);
        // The other reasons have their own way out, and no such sentence.
        for (const code of ['ft8_tx_in_flight', 'ft8_session_active', 'ft8_tx_armed']) {
            expect(claimRefusalText(code, 'FT4', 'x', true)).not.toMatch(/try again/);
        }
    });

    it('never shows the daemon\u2019s own wording for a reason it knows', () => {
        for (const code of [
            'ft8_profile_busy',
            'ft8_tx_in_flight',
            'ft8_session_active',
            'ft8_tx_armed',
            'archive_switch_pending',
        ]) {
            expect(claimRefusalText(code, 'FT4', DAEMON, true)).not.toContain('subscriber');
        }
    });

    it('claims no more than the refusal means', () => {
        // A stream of the other mode is open; whether anything is being received is not
        // known from the refusal. And waiting for a transmission to end does not unblock
        // the change: TX is still enabled afterwards.
        expect(claimRefusalText('ft8_profile_busy', 'FT4', DAEMON, true)).not.toMatch(/receiv/i);
        const inFlight = claimRefusalText('ft8_tx_in_flight', 'FT4', 'x', false);
        expect(inFlight).not.toMatch(/when it has finished/);
        expect(inFlight).toMatch(/Disable TX/);
        // A send that was cancelled before keying still holds the transmit slot for a
        // moment: nothing is on the air and nothing will be (the clean-room review of
        // 0e996b94), so the sentence does not say that anything is.
        expect(inFlight).not.toMatch(/is transmitting|about to|in progress|on the air/);
    });

    it('keeps what it was given for a reason it does not know', () => {
        expect(claimRefusalText('network', 'FT4', 'The profile could not be claimed.', true)).toBe(
            'The profile could not be claimed.'
        );
        expect(claimRefusalText('', 'FT8', 'something else', false)).toBe('something else');
    });
});
