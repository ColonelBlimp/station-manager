/*
    FT-family slot clock — even vs odd slots on a profile's lattice (ADR 0080).

    FT8 runs on a two-parity 15 s grid, FT4 on a 7.5 s one; the parity of a slot
    decides who can work whom (you transmit on the parity OPPOSITE the station
    you're working). Mirrors the daemon convention (internal/ft8 Profile.
    SlotRefFromTime): floor(unixMillis / slotMs) mod 2 == 0 → "even". On the wall
    clock that is :00/:30 even and :15/:45 odd for FT8 (WSJT-X's "Tx even/1st"),
    and :00.0/:15.0 even, :07.5/:22.5 odd for FT4.

    Input is the slot's RFC3339 UTC start (DecodeEntry.startUtc / PileupEntry.slotUtc,
    with milliseconds on the FT4 lattice); '' for a missing/unparseable value so
    callers can render "no parity".
*/

export type SlotParity = 'even' | 'odd';

/** The period of a profile by its wire name (`ft8-tx.mode`); FT8's when unknown. */
export function slotMsFor(mode: string): number {
    return mode === 'FT4' ? 7_500 : 15_000;
}

/** Parity of the slot starting at `startUtc` (RFC3339) on a lattice of `slotMs`, or '' if unknown. */
export function slotParity(startUtc: string | undefined, slotMs = 15_000): SlotParity | '' {
    if (!startUtc) return '';
    const ms = Date.parse(startUtc);
    if (Number.isNaN(ms)) return '';
    // Slot index since the Unix epoch (epoch 0 is a boundary on both lattices),
    // matching the daemon's integer division. The start is slot-aligned, so the
    // floor is exact.
    const slotIndex = Math.floor(ms / slotMs);
    return slotIndex % 2 === 0 ? 'even' : 'odd';
}

/** The live slot clock at `nowMs` on a lattice of `slotMs`: whole seconds until the
 *  next boundary (1…period) and the parity of the slot in progress — a pure
 *  function of the wall clock, so the countdown pill never lags the daemon. */
export function slotClock(nowMs: number, slotMs: number): { secsLeft: number; parity: SlotParity } {
    const boundary = Math.floor(nowMs / slotMs) * slotMs;
    const period = Math.ceil(slotMs / 1000);
    const secsLeft = Math.max(1, Math.min(period, Math.ceil((boundary + slotMs - nowMs) / 1000)));
    const parity: SlotParity = Math.floor(nowMs / slotMs) % 2 === 0 ? 'even' : 'odd';
    return { secsLeft, parity };
}
