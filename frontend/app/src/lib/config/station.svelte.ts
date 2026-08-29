/*
    Station settings state (app Settings view, ADR 0044) — the daemon-
    authoritative logging_station block as an editable form. Daemon is the single
    source of truth (ADR 0003): load on mount, edit, save via PUT /v1/config; no
    localStorage cache. Dirty = the form differs from the last loaded/saved
    snapshot. The operational `station` block is held opaque and echoed on save
    (see api/config.ts) so a Station save never disturbs amp/power/bands.
*/
import {
    fetchStation,
    saveStation,
    type StationFields,
    type StationConfig,
    type QslFields,
} from '../api/config';
import { noteConfigDurability } from './durability';
import { toasts } from '../ui/toasts.svelte';

const EMPTY_QSL: QslFields = { qsl_via: '', qslmsg: '', qsl_sent_via: '' };

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
    // The standing QSL defaults block, edited in the same section but tracked and
    // sent INDEPENDENTLY of logging_station (config SPA parity), so a save touches
    // only the block(s) actually changed — never resending a stale sibling block.
    qslForm = $state<QslFields>({ ...EMPTY_QSL });

    // Per-block dirty snapshots, seeded from the empty defaults so a never-loaded
    // state reads clean. Kept separate (not one combined snapshot) so save() can
    // send only the block that changed.
    #pristineStation = $state('{}');
    #pristineQsl = $state(JSON.stringify(EMPTY_QSL));

    stationDirty = $derived(JSON.stringify(this.form) !== this.#pristineStation);
    qslDirty = $derived(JSON.stringify(this.qslForm) !== this.#pristineQsl);
    dirty = $derived(this.stationDirty || this.qslDirty);

    async load(): Promise<void> {
        if (this.loading) return;
        this.loading = true;
        // Invalidate before awaiting: while a reload is pending the retained
        // identity is not known-current and must neither render nor save
        // (clean-room review 2c64c7aa P1).
        this.loaded = false;
        this.error = '';
        const res = await fetchStation();
        this.loading = false;
        if (res.kind === 'error') {
            this.error = res.message;
            // Unloaded, not merely errored — see the note on emailState.load.
            // Settings unmounts on navigation while this module survives, so a
            // failed remount reload would leave stale values on screen looking
            // current, and logging_station is round-tripped WHOLE.
            return;
        }
        this.#apply(res.config);
        this.loaded = true;
    }

    async save(): Promise<void> {
        // logging_station is a whole-block write, so a successfully loaded
        // baseline is a data-safety precondition, not merely a UI status.
        if (this.saving || !this.loaded || !this.dirty) return;
        this.saving = true;
        // Hold `saving` across BOTH the PUT and the onSaved context refresh
        // (finally), not just the PUT: clearing it early let a second save start
        // while the first refresh was still in flight, and an out-of-order
        // completion could apply a stale operator/grid to the shared context
        // (review 2026-07-20 round 2 #2). The latch serialises them.
        try {
            // Send ONLY the changed block(s) — a station-only edit must not resend
            // a stale qsl (and vice versa), or it would clobber a concurrent change.
            const patch: Partial<StationConfig> = {};
            if (this.stationDirty) patch.station = { ...this.form };
            if (this.qslDirty) patch.qsl = { ...this.qslForm };
            const res = await saveStation(patch);
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
            if (!noteConfigDurability(res.durabilityUnconfirmed ?? false)) {
                toasts.info('Station settings saved.');
            }
        } finally {
            this.saving = false;
        }
    }

    // Revert edits to the last loaded/saved snapshot (both blocks).
    reset(): void {
        this.form = JSON.parse(this.#pristineStation) as StationFields;
        this.qslForm = JSON.parse(this.#pristineQsl) as QslFields;
    }

    #apply(cfg: StationConfig): void {
        const f: StationFields = { ...cfg.station };
        for (const k of STATION_KEYS) {
            if (!(k in f)) f[k] = '';
        }
        this.form = f;
        this.qslForm = { ...cfg.qsl };
        this.#pristineStation = JSON.stringify(f);
        this.#pristineQsl = JSON.stringify(this.qslForm);
    }
}

export const stationState = new StationState();
