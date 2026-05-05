/**
 * Per-QSO emission defaults that the operator pre-selects in the My
 * Station QSO sub-tab. These are NOT ADIF MY_* identity fields and
 * are NOT daemon-persisted via /v1/config — they are local operator
 * preferences for emission, hydrated from localStorage so they
 * survive page reloads.
 *
 * Currently a single tri-state for ADIF QSO_RANDOM: 'Y' / 'N' /
 * 'off'. 'off' is the default; QSO_RANDOM is omitted from the wire
 * record entirely, matching the behaviour of every other optional
 * ADIF field. 'Y' / 'N' force the value on every QSO until the
 * operator changes it.
 *
 * Persistence tier: localStorage (per-device, survives reloads,
 * survives tab close) — matches manualState's pattern. Not
 * sessionStorage because operator preferences should outlive a tab.
 *
 * @see docs/v2-design/frontend-spa.md "Five-tier persistence"
 */
const QSO_RANDOM_KEY = 'sm.qsoDefaults.qsoRandom';

export type QsoRandomDefault = 'Y' | 'N' | 'off';
const VALID_QSO_RANDOM: readonly QsoRandomDefault[] = ['Y', 'N', 'off'];

function loadQsoRandom(): QsoRandomDefault {
    try {
        const raw = localStorage.getItem(QSO_RANDOM_KEY);
        if (raw !== null && (VALID_QSO_RANDOM as readonly string[]).includes(raw)) {
            return raw as QsoRandomDefault;
        }
    } catch {
        // localStorage unavailable — fall through to default.
    }
    return 'off';
}

function save(value: QsoRandomDefault): void {
    try {
        localStorage.setItem(QSO_RANDOM_KEY, value);
    } catch {
        // Storage write failed — in-memory state still correct.
    }
}

class QsoDefaults {
    qsoRandom: QsoRandomDefault = $state(loadQsoRandom());
}

export const qsoDefaults = new QsoDefaults();

// Module-level $effect mirrors writes to localStorage. $effect.root
// is the canonical pattern here (see manual.svelte.ts) — needed
// because $effect can't be used at the top level of a module without
// a root context.
$effect.root(() => {
    $effect(() => save(qsoDefaults.qsoRandom));
});
