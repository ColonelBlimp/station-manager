// The words of the banner a refused FT8/FT4 claim shows (Ft8View.svelte).
//
// The daemon answers a refused claim with a code and a sentence of its own, written for whoever
// reads its API: "another subscriber holds a capture on the other profile". The operator asked
// for something he could use (2026-11-24: "ambiguous and almost meaningless unless you are the
// developer who knows what a subscriber refers to"). So for every reason it knows by its code,
// the page says what is in the way in the operator's terms and names both modes: the one
// wanted, and the one still in the way. A reason it does not know keeps the sentence it came
// with (a transport failure's, say), so nothing is ever shown blank.

/** `wanted` is the mode the view is for; the other one is what is still in the way.
 *  `canTryAgain` is whether the banner shows its "Try again" button: an instruction is given
 *  only where it can be acted on (operator ruling 2026-09-15), so the sentence that tells the
 *  operator to try again is left out while the page is counting down to its own retry. */
export function claimRefusalText(
    code: string,
    wanted: 'FT8' | 'FT4',
    fallback: string,
    canTryAgain: boolean
): string {
    const other = wanted === 'FT4' ? 'FT8' : 'FT4';
    switch (code) {
        case 'ft8_profile_busy': {
            // "Open", not "receiving": the refusal means a stream of the other mode is
            // still open, and a stream can be open while capture never started (the
            // clean-room review of b8ad8dba).
            const still = `${other} is still open, and only one of FT8 and FT4 can be open at a time.`;
            return canTryAgain
                ? `${still} If you have just switched, try again in a few seconds.`
                : still;
        }
        case 'ft8_tx_in_flight':
            // Not "it can start when the transmission has finished": a manual send is in
            // flight while it still waits for its slot, and when it ends TX is still
            // enabled, which refuses the change in its turn. Disable TX is what unblocks
            // it, and the banner offers that button (the same review).
            return `${other} is transmitting, or about to. Disable TX to change to ${wanted}.`;
        case 'ft8_session_active':
            return `A contact or run is in progress on ${other}. End it to change to ${wanted}.`;
        case 'ft8_tx_armed':
            return `Transmit is enabled on ${other}. Disable TX to change to ${wanted}.`;
        case 'archive_switch_pending':
            return 'The archive is being switched and Station Manager is restarting.';
        default:
            return fallback;
    }
}
