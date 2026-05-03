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

const SUBMODE_TO_MODE: Record<string, string> = {
    // SSB sidebands
    USB: 'SSB',
    LSB: 'SSB',

    // MFSK family (WSJT-X et al.)
    FT4: 'MFSK',
    FT8: 'MFSK',
    FST4: 'MFSK',
    FST4W: 'MFSK',
    Q65: 'MFSK',
    OLIVIA: 'MFSK',
    CONTESTIA: 'MFSK',
    DOMINOEX: 'MFSK',
    FSQ: 'MFSK',
    JS8: 'MFSK',
    MFSK16: 'MFSK',
    MFSK8: 'MFSK',
    MT63: 'MFSK',
    THOR: 'MFSK',
    THROB: 'MFSK',

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
    HFSK: 'HELL',
    HHELL: 'HELL',
    PSKHELL: 'HELL',

    // Packet family
    PKT: 'PACKET',
    APRS: 'PACKET',
};

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
