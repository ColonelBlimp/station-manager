/**
 * Per-QSO emission defaults + notification preferences that the
 * operator pre-selects in the My Station QSO sub-tab. These are NOT
 * ADIF MY_* identity fields and are NOT daemon-persisted via
 * /v1/config — they are local operator preferences hydrated from
 * localStorage so they survive page reloads.
 *
 * Fields:
 *   - `qsoRandom` — tri-state for ADIF QSO_RANDOM: 'Y' / 'N' / 'off'.
 *     Default 'off' (omits the field from every record, matching the
 *     behaviour of every other optional ADIF field). 'Y' / 'N' force
 *     the value on every QSO until the operator changes it.
 *   - `notifyQsoStored` — show an info toast after a successful QSO
 *     submit. Default true (the operator wants confirmation that
 *     submit landed). Mute via the QSO sub-tab if the toast becomes
 *     noise during a high-rate run.
 *   - `notifyConfigSaved` — show an info toast after a successful My
 *     Station Update. Default true.
 *
 * Errors and warnings (validation/server/network/duplicate) ALWAYS
 * toast regardless of these flags — those outcomes are never noise.
 *
 * Persistence tier: localStorage (per-device, survives reloads,
 * survives tab close) — matches manualState's pattern. Not
 * sessionStorage because operator preferences should outlive a tab.
 *
 * @see docs/v2-design/frontend-spa.md "Five-tier persistence"
 */
const QSO_RANDOM_KEY = 'sm.qsoDefaults.qsoRandom';
const NOTIFY_QSO_STORED_KEY = 'sm.qsoDefaults.notifyQsoStored';
const NOTIFY_CONFIG_SAVED_KEY = 'sm.qsoDefaults.notifyConfigSaved';

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

function loadBool(key: string, fallback: boolean): boolean {
    try {
        const raw = localStorage.getItem(key);
        if (raw === 'true') return true;
        if (raw === 'false') return false;
    } catch {
        // localStorage unavailable — fall through to default.
    }
    return fallback;
}

function saveString(key: string, value: string): void {
    try {
        localStorage.setItem(key, value);
    } catch {
        // Storage write failed — in-memory state still correct.
    }
}

class QsoDefaults {
    qsoRandom: QsoRandomDefault = $state(loadQsoRandom());
    notifyQsoStored: boolean = $state(loadBool(NOTIFY_QSO_STORED_KEY, true));
    notifyConfigSaved: boolean = $state(loadBool(NOTIFY_CONFIG_SAVED_KEY, true));
}

export const qsoDefaults = new QsoDefaults();

// Module-level $effect mirrors writes to localStorage. $effect.root
// is the canonical pattern here (see manual.svelte.ts) — needed
// because $effect can't be used at the top level of a module without
// a root context.
$effect.root(() => {
    $effect(() => saveString(QSO_RANDOM_KEY, qsoDefaults.qsoRandom));
    $effect(() => saveString(NOTIFY_QSO_STORED_KEY, String(qsoDefaults.notifyQsoStored)));
    $effect(() => saveString(NOTIFY_CONFIG_SAVED_KEY, String(qsoDefaults.notifyConfigSaved)));
});
