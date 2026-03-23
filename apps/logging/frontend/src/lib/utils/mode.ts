// Map ADIF 3.1.x Submodes to their parent Modes.
// Notes:
// - This is not an exhaustive list, but covers the most common submodes encountered in practice.
// - Keys are matched case-insensitively after trimming.
export const getModeBySubmode = (submode: string): string => {
    const s = submode?.trim().toUpperCase();
    if (!s) return 'unknown';
    const map: Record<string, string> = {
        // DIGITALVOICE family
        C4FM: 'DIGITALVOICE',
        DMR: 'DIGITALVOICE',
        DSTAR: 'DIGITALVOICE',
        FREEDV: 'DIGITALVOICE',
        M17: 'DIGITALVOICE',

        // MFSK family (WSJT-X and other MFSK-based)
        FT4: 'MFSK',
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

        // SSB sidebands as submodes
        LSB: 'SSB',
        USB: 'SSB',
    };
    return map[s] ?? s;
};
