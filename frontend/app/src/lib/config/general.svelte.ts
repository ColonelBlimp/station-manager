/*
    General settings state (app Settings view, ADR 0044) — the mode-switch rig-restore
    knob + the contacts-map band-colour overrides as an editable form, plus the
    read-only About/build info. Daemon-authoritative (ADR 0003): load on mount, edit,
    save via PUT /v1/config; no localStorage. Dirty = the form differs from the last
    loaded/saved snapshot. The rest of the `map` block is held opaque and echoed on
    save (see api/general.ts) so a colour edit never disturbs other map fields.
*/
import {
    fetchGeneral,
    saveGeneral,
    fetchBuildInfo,
    type GeneralConfig,
    type BuildInfo,
} from '../api/general';
import { toasts } from '../ui/toasts.svelte';

interface GeneralForm {
    restoreRigOnModeSwitch: boolean;
    bandColors: Record<string, string>;
}

// The form's rest state: rig-restore ON (matching the *bool default) and no colour
// overrides. #pristine is seeded from this so a NEVER-LOADED state reads clean —
// otherwise dirty is true from construction and the leave-guard counts General as
// unsaved before it has even been opened (unsaved.test.ts R2).
const DEFAULT_FORM: GeneralForm = { restoreRigOnModeSwitch: true, bandColors: {} };

// Canonical JSON of a form for the dirty compare — band keys sorted so a reordered
// map isn't spuriously dirty.
function canon(f: GeneralForm): string {
    const bands: Record<string, string> = {};
    for (const k of Object.keys(f.bandColors).sort()) bands[k] = f.bandColors[k];
    return JSON.stringify({
        restoreRigOnModeSwitch: f.restoreRigOnModeSwitch,
        bandColors: bands,
    });
}

class GeneralState {
    loading = $state(false);
    saving = $state(false);
    loaded = $state(false);
    error = $state('');
    form = $state<GeneralForm>({
        restoreRigOnModeSwitch: DEFAULT_FORM.restoreRigOnModeSwitch,
        bandColors: {},
    });

    // The opaque rest of the `map` block (everything except band_colors), echoed on
    // save. Not part of the form ⇒ not part of dirty; it is never edited here.
    #mapRest: Record<string, unknown> = {};

    // Canonical JSON of the last loaded/saved form for the dirty compare. Seeded from
    // DEFAULT_FORM (not '{}') so a never-loaded state is clean; see DEFAULT_FORM.
    #pristine = $state(canon(DEFAULT_FORM));
    dirty = $derived(canon(this.form) !== this.#pristine);

    // About / build info (read-only diagnostics).
    buildInfo = $state<BuildInfo | null>(null);
    buildLoading = $state(false);
    buildError = $state('');

    async load(): Promise<void> {
        if (this.loading) return;
        this.loading = true;
        // Invalidate before awaiting: a pending reload's stale values must neither
        // render nor save (matches stationState.load).
        this.loaded = false;
        this.error = '';
        const res = await fetchGeneral();
        this.loading = false;
        if (res.kind === 'error') {
            this.error = res.message;
            return;
        }
        this.#apply(res.config);
        this.loaded = true;
        if (this.buildInfo === null) void this.loadBuildInfo();
    }

    async save(): Promise<void> {
        if (this.saving || !this.loaded || !this.dirty) return;
        this.saving = true;
        try {
            const res = await saveGeneral({
                restoreRigOnModeSwitch: this.form.restoreRigOnModeSwitch,
                bandColors: { ...this.form.bandColors },
                mapRest: this.#mapRest,
            });
            if (res.kind === 'error') {
                toasts.error(`Save failed: ${res.message}`);
                return;
            }
            this.#apply(res.config);
            toasts.info('General settings saved.');
        } finally {
            this.saving = false;
        }
    }

    reset(): void {
        const p = JSON.parse(this.#pristine) as GeneralForm;
        this.form = {
            restoreRigOnModeSwitch: p.restoreRigOnModeSwitch,
            bandColors: { ...p.bandColors },
        };
    }

    async loadBuildInfo(): Promise<void> {
        if (this.buildLoading) return;
        this.buildLoading = true;
        this.buildError = '';
        const res = await fetchBuildInfo();
        this.buildLoading = false;
        if (res.kind === 'error') {
            this.buildError = res.message;
            return;
        }
        this.buildInfo = res.info;
    }

    // A colour equal to the band's default is "no override" — the stored block stays
    // sparse, so a future default-palette improvement still reaches untouched bands.
    setBandColor(band: string, value: string, def: string): void {
        const v = value.toLowerCase();
        if (v === def.toLowerCase()) delete this.form.bandColors[band];
        else this.form.bandColors[band] = v;
    }

    #apply(cfg: GeneralConfig): void {
        this.#mapRest = cfg.mapRest;
        const f: GeneralForm = {
            restoreRigOnModeSwitch: cfg.restoreRigOnModeSwitch,
            bandColors: { ...cfg.bandColors },
        };
        this.form = f;
        this.#pristine = canon(f);
    }
}

export const generalState = new GeneralState();
