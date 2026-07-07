/*
    ADIF mode-vs-submode resolver.

    The operator-facing mode dropdown and the rig's CAT mode field both
    speak in the names operators actually say at the mic: "USB", "LSB",
    "FT8", "PSK31", "CW", etc. ADIF, however, distinguishes:

      - **MODE** is the parent family (SSB, MFSK, PSK, CW, FM, AM, RTTY,
        DIGITALVOICE, HELL, PACKET).
      - **SUBMODE** is the specific variant (USB/LSB under SSB; FT8/FT4
        under MFSK; PSK31 under PSK; CW-N for narrow CW under CW; etc).

    The daemon's `internal/enums/modes/modes.go` enforces ADIF MODE
    membership strictly — submitting `MODE=USB` returns `400
    invalid_field_value`. The SPA must therefore translate the
    operator's selection into a `(MODE, SUBMODE)` pair before building
    the ADIF record.

    Mirrors `submodeToMode` in `internal/enums/modes/modes.go`. The
    daemon is the source of truth for which submodes resolve to which
    parents; if a new submode lands daemon-side, mirror it here.
*/

/*
    Mirrors the daemon's embedded `adif-modes.json` submodes section.
    Entries that ADIF 3.x promotes to main modes (FT8, FT4, FST4, Q65,
    JS8, JT*, MSK144, MT63, OLIVIA, etc.) live in the daemon's
    main_modes list and are NOT in this table — they pass through
    `resolveModeAndSubmode` as MODE=value SUBMODE='' (the
    pass-through branch below).
*/
const SUBMODE_TO_MODE: Record<string, string> = {
    // SSB sidebands
    USB: 'SSB',
    LSB: 'SSB',

    // MFSK family (WSJT-X et al.) — only the ones that ARE submodes
    // of MFSK in ADIF 3.x.
    MFSK16: 'MFSK',
    MFSK8: 'MFSK',

    // PSK family
    PSK10: 'PSK',
    PSK31: 'PSK',
    PSK63: 'PSK',
    PSK125: 'PSK',
    QPSK31: 'PSK',
    QPSK63: 'PSK',
    BPSK31: 'PSK',
    BPSK63: 'PSK',

    // DIGITALVOICE family
    C4FM: 'DIGITALVOICE',
    DMR: 'DIGITALVOICE',
    DSTAR: 'DIGITALVOICE',
    FREEDV: 'DIGITALVOICE',
    M17: 'DIGITALVOICE',

    // Hellschreiber family
    HELL80: 'HELL',
    FMHELL: 'HELL',
    FSKHELL: 'HELL',
    PSKHELL: 'HELL',

    // Packet family
    APRS: 'PACKET',
};

/*
    The WSJT-X-family weak-signal digital modes report a signed dB SNR
    (e.g. -12, +04) instead of the RST readability/strength (and tone)
    digits used by phone and CW. RST input validation must branch on this
    so an FT8 report like "-12" isn't flagged as invalid (and so its sign
    isn't stripped as the operator edits). Matched case-insensitively
    against the ADIF MODE or SUBMODE. Limited to the SNR-reporting modes —
    PSK/RTTY/OLIVIA/MFSK16 etc. still use ordinary RST.
*/
const SIGNAL_REPORT_MODES = new Set<string>([
    'FT8',
    'FT4',
    'JT65',
    'JT9',
    'JT4',
    'JT6M',
    'JT44',
    'FST4',
    'FST4W',
    'Q65',
    'QRA64',
    'MSK144',
    'FSK441',
    'JTMS',
    'ISCAT',
    'JS8',
    'WSPR',
]);

/**
 * True when the given ADIF mode (or submode) reports signal strength as a
 * signed dB SNR rather than RST digits. Used to pick the right report
 * validator/format. Empty/unknown input is treated as RST (false).
 */
export function usesSignalReport(mode: string): boolean {
    return SIGNAL_REPORT_MODES.has(mode.trim().toUpperCase());
}

export interface ResolvedMode {
    /** ADIF MODE — always populated for non-empty input. */
    mode: string;
    /** ADIF SUBMODE — empty when the operator picked a main mode that has no sub-refinement. */
    subMode: string;
}

/*
    Resolve the operator's selected mode (and any rig-supplied submode
    refinement) into a canonical ADIF (MODE, SUBMODE) pair.

    Cases:
      - opMode is itself a submode (USB, FT8, PSK31, …):
          → MODE = parent, SUBMODE = opMode (overrides opSubMode; the
            dropdown value IS the submode).
      - opMode is already a main mode (CW, FM, AM, RTTY, SSB, …):
          → MODE = opMode, SUBMODE = opSubMode (pass-through; lets a
            rig push CW + CW-N for narrow CW).
      - opMode is empty / unknown:
          → returned as-is; the daemon will reject if invalid.

    All inputs are trimmed and upper-cased before lookup so that
    `'usb'`, `' USB '`, `'Usb'` all resolve identically.
*/
export function resolveModeAndSubmode(opMode: string, opSubMode: string = ''): ResolvedMode {
    const m = opMode.trim().toUpperCase();
    const s = opSubMode.trim().toUpperCase();

    if (m === '') {
        return { mode: '', subMode: s };
    }

    const parent = SUBMODE_TO_MODE[m];
    if (parent !== undefined) {
        return { mode: parent, subMode: m };
    }
    return { mode: m, subMode: s };
}
