/*
    FT8 slot parity — even vs odd 15-second slots.

    FT8 runs on a two-parity 15 s grid; the parity of a slot decides who can work
    whom (you transmit on the parity OPPOSITE the station you're working). Mirrors
    the daemon convention (internal/ft8 SlotRefFromTime): (unix / 15) % 2 == 0 →
    "even". On the wall clock that is :00/:30 = even, :15/:45 = odd (UTC), the same
    as WSJT-X's "Tx even/1st".

    Input is the slot's RFC3339 UTC start (DecodeEntry.startUtc / PileupEntry.slotUtc);
    returns '' for a missing/unparseable value so callers can render "no parity".
*/

export type SlotParity = 'even' | 'odd';

const SLOT_SECONDS = 15;

/** Parity of the slot starting at `startUtc` (RFC3339), or '' if unknown. */
export function slotParity(startUtc: string | undefined): SlotParity | '' {
    if (!startUtc) return '';
    const ms = Date.parse(startUtc);
    if (Number.isNaN(ms)) return '';
    // Slot index since the Unix epoch (epoch 0 is a slot boundary), matching the
    // daemon's integer (unix / slotSeconds). The start is slot-aligned, so the
    // floor is exact.
    const slotIndex = Math.floor(ms / 1000 / SLOT_SECONDS);
    return slotIndex % 2 === 0 ? 'even' : 'odd';
}
