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

// Injected by main.ts (per ADR 0045 DI — this module never imports the app
// bootstrap): push the just-saved identity into the app's shared station
// context so the operate/QSO path picks up a changed operator/grid with no page
// reload (review 2026-07-20 #2). Receives the saved logging_station fields
// straight from the PUT response — main.ts applies them WITHOUT a second GET, so
// there is no refresh-failure window and the context can never diverge from the
// save (review round 2 #1 / round 3 #1). Null in tests / before wiring.
let onSaved: ((station: StationFields) => void | Promise<void>) | null = null;
export function setStationSaved(fn: (station: StationFields) => void | Promise<void>): void {
    onSaved = fn;
}

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

    dirty = $derived(JSON.stringify(this.form) !== this.#pristine);

    async load(): Promise<void> {
        if (this.loading) return;
        this.loading = true;
        this.error = '';
        const res = await fetchStation();
        this.loading = false;
        if (res.kind === 'error') {
            this.error = res.message;
            // Unloaded, not merely errored — see the note on emailState.load.
            // Settings unmounts on navigation while this module survives, so a
            // failed remount reload would leave stale values on screen looking
            // current, and logging_station is round-tripped WHOLE.
            this.loaded = false;
            return;
        }
        this.#apply(res.config);
        this.loaded = true;
    }

    async save(): Promise<void> {
        if (this.saving || !this.dirty) return;
        this.saving = true;
        // Hold `saving` across BOTH the PUT and the onSaved context refresh
        // (finally), not just the PUT: clearing it early let a second save start
        // while the first refresh was still in flight, and an out-of-order
        // completion could apply a stale operator/grid to the shared context
        // (review 2026-07-20 round 2 #2). The latch serialises them.
        try {
            const res = await saveStation({ station: { ...this.form } });
            if (res.kind === 'error') {
                toasts.error(`Save failed: ${res.message}`);
                return;
            }
            this.#apply(res.config);
            // Push the just-saved identity into the shared station context so a
            // changed operator/grid takes effect app-wide without a reload — from
            // the response we already hold (form == the daemon's post-save
            // values), never a second fetch (review 2026-07-20 #2 / round 3 #1).
            await onSaved?.({ ...this.form });
            toasts.info('Station settings saved.');
        } finally {
            this.saving = false;
        }
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
    }
}

export const stationState = new StationState();
