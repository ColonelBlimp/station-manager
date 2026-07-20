/*
    Station settings state (app Settings view, ADR 0044) — the daemon-
    authoritative logging_station block as an editable form. Daemon is the single
    source of truth (ADR 0003): load on mount, edit, save via PUT /v1/config; no
    localStorage cache. Dirty = the form differs from the last loaded/saved
    snapshot. The operational `station` block is held opaque and echoed on save
    (see api/config.ts) so a Station save never disturbs amp/power/bands.
*/
import { fetchStation, saveStation, type StationFields, type StationConfig } from '../api/config';
import { toasts } from '../ui/toasts.svelte';

// The identity fields the form renders. Ensured present (as '') after load so
// binding is clean even when the daemon config predates a field (e.g. a station
// that never set a postal code / CW key). Saving an empty field is harmless —
// it maps to an empty ADIF MY_* value.
export const STATION_KEYS = [
    'station_callsign',
    'operator',
    'owner_callsign',
    'my_name',
    'my_sig',
    'my_sig_info',
    'my_gridsquare',
    'my_country',
    'my_dxcc',
    'my_cq_zone',
    'my_itu_zone',
    'my_altitude',
    'my_street',
    'my_city',
    'my_postal_code',
    'my_antenna',
    'my_morse_key_type',
    'my_morse_key_info',
] as const;

class StationState {
    loading = $state(false);
    saving = $state(false);
    loaded = $state(false);
    error = $state('');
    form = $state<StationFields>({});

    // JSON of the last loaded/saved form, for the dirty compare. Key order is
    // stable (the form is always rebuilt by #apply in the same order), so a
    // string compare is a valid change check.
    #pristine = $state('{}');
    // The operational `station` block — held opaque, echoed verbatim on save.
    #operational: Record<string, unknown> = {};

    dirty = $derived(JSON.stringify(this.form) !== this.#pristine);

    async load(): Promise<void> {
        if (this.loading) return;
        this.loading = true;
        this.error = '';
        const res = await fetchStation();
        this.loading = false;
        if (res.kind === 'error') {
            this.error = res.message;
            return;
        }
        this.#apply(res.config);
        this.loaded = true;
    }

    async save(): Promise<void> {
        if (this.saving || !this.dirty) return;
        this.saving = true;
        const res = await saveStation({ station: { ...this.form }, operational: this.#operational });
        this.saving = false;
        if (res.kind === 'error') {
            toasts.error(`Save failed: ${res.message}`);
            return;
        }
        this.#apply(res.config);
        toasts.info('Station settings saved.');
    }

    // Revert edits to the last loaded/saved snapshot.
    reset(): void {
        this.form = JSON.parse(this.#pristine) as StationFields;
    }

    #apply(cfg: StationConfig): void {
        const f: StationFields = { ...cfg.station };
        for (const k of STATION_KEYS) {
            if (!(k in f)) f[k] = '';
        }
        this.form = f;
        this.#pristine = JSON.stringify(f);
        this.#operational = cfg.operational;
    }
}

export const stationState = new StationState();
