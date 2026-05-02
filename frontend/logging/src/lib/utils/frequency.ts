/**
 * Frequency utility helpers — band lookup, etc.
 *
 * Lifted from `Vfos.svelte` 2026-05-02 once a second consumer (QSO
 * submit / ADIF payload building) appeared. Per the project's "build
 * specific, not generic" rule, the inline copy was correct until the
 * second use case showed up.
 *
 * Boundaries follow IARU allocations broad enough to cover Region 1,
 * 2, and 3 for the common bands; 60m is widened to 5.25–5.45 MHz to
 * match most regional variations. Out-of-band frequencies (between
 * bands or below 160m) return an empty string — callers decide how to
 * render or treat the absence.
 */

const BANDS: { low: number; high: number; name: string }[] = [
    { low: 1_800_000,     high: 2_000_000,     name: '160m' },
    { low: 3_500_000,     high: 4_000_000,     name: '80m' },
    { low: 5_250_000,     high: 5_450_000,     name: '60m' },
    { low: 7_000_000,     high: 7_300_000,     name: '40m' },
    { low: 10_100_000,    high: 10_150_000,    name: '30m' },
    { low: 14_000_000,    high: 14_350_000,    name: '20m' },
    { low: 18_068_000,    high: 18_168_000,    name: '17m' },
    { low: 21_000_000,    high: 21_450_000,    name: '15m' },
    { low: 24_890_000,    high: 24_990_000,    name: '12m' },
    { low: 28_000_000,    high: 29_700_000,    name: '10m' },
    { low: 50_000_000,    high: 54_000_000,    name: '6m' },
    { low: 70_000_000,    high: 70_500_000,    name: '4m' },
    { low: 144_000_000,   high: 148_000_000,   name: '2m' },
    { low: 222_000_000,   high: 225_000_000,   name: '1.25m' },
    { low: 420_000_000,   high: 450_000_000,   name: '70cm' },
    { low: 902_000_000,   high: 928_000_000,   name: '33cm' },
    { low: 1_240_000_000, high: 1_300_000_000, name: '23cm' },
];

/**
 * Map a frequency in Hz to its amateur-radio band name (ADIF BAND
 * field convention — "20m", "70cm", etc.). Returns '' for out-of-band
 * frequencies.
 */
export function frequencyToBand(hz: number): string {
    for (const { low, high, name } of BANDS) {
        if (hz >= low && hz <= high) return name;
    }
    return '';
}
