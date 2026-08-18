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

// Lay the operator's OWN edits back over a freshly re-read form, mirroring the FT8
// section's mergeEdits (#reconcileAfterTimeout). A field is the operator's iff this
// save asked to change it (sent ≠ before) or they edited it while the save was in
// flight (draft ≠ sent); everything else — including a SECOND writer's change to a
// different band — is adopted from `stored`, so a resend of this whole-block map
// never reverts it. General needs no all/some/none verdict the way FT8 does: FT8's
// verdict exists only to adopt its daemon-clamped values, whereas General's config
// is validated-or-rejected with NO silent normalisation (config.go band_colors and
// restore_rig checks), so a landed field's stored value already equals what was sent.
function mergeGeneral(
    before: GeneralForm,
    sent: GeneralForm,
    draft: GeneralForm,
    stored: GeneralForm
): GeneralForm {
    const bandColors: Record<string, string> = { ...stored.bandColors };
    const bands = new Set<string>([
        ...Object.keys(before.bandColors),
        ...Object.keys(sent.bandColors),
        ...Object.keys(draft.bandColors),
    ]);
    for (const band of bands) {
        const b = before.bandColors[band];
        const s = sent.bandColors[band];
        const d = draft.bandColors[band];
        if (!(d !== s || s !== b)) continue; // not operator-owned ⇒ keep stored's
        if (d === undefined) delete bandColors[band];
        else bandColors[band] = d;
    }
    const rB = before.restoreRigOnModeSwitch;
    const rS = sent.restoreRigOnModeSwitch;
    const rD = draft.restoreRigOnModeSwitch;
    return {
        restoreRigOnModeSwitch: rD !== rS || rS !== rB ? rD : stored.restoreRigOnModeSwitch,
        bandColors,
    };
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
        // Captured BEFORE the write. The inputs stay live while a save is in flight
        // (only Save/Cancel are disabled, matching the other sections), so the form
        // can move underneath the request — "did this save ask for this field?" must
        // be answered from what was SENT, not from what is on screen when the answer
        // (or a timeout) finally arrives. `before` is the saved baseline a timed-out
        // write is judged against; `sent` is what this save carries.
        const before = JSON.parse(this.#pristine) as GeneralForm;
        const sent: GeneralForm = {
            restoreRigOnModeSwitch: this.form.restoreRigOnModeSwitch,
            bandColors: { ...this.form.bandColors },
        };
        try {
            const res = await saveGeneral({ ...sent, mapRest: this.#mapRest });
            if (res.kind === 'error') {
                if (res.timedOut) {
                    await this.#reconcileAfterTimeout(before, sent);
                    return;
                }
                toasts.error(`Save failed: ${res.message}`);
                return;
            }
            // Adopt the daemon's stored values as the new baseline but KEEP the live
            // form: an edit made while the PUT was in flight is unsaved work, not a
            // value to overwrite with what we just sent (clean-room review 16cb3ea3 P1).
            this.#rebaseline(res.config);
            toasts.info('General settings saved.');
        } finally {
            this.saving = false;
        }
    }

    /**
     * Settle a write whose outcome we never learned (clean-room review 16cb3ea3 P2).
     *
     * A timed-out PUT reached the daemon with no response, so it MAY already have
     * committed. Reporting "Save failed" asserts the one thing we do not know, and
     * the retry it invites resends the WHOLE `map` block as a whole-block replace,
     * which can revert a change made in between — the standalone config SPA's
     * General tab is still served at /config/ until this port retires it, so a
     * second writer of these very fields genuinely exists.
     *
     * So re-read the authoritative config, lay the operator's own edits back over it
     * (mergeGeneral), and move the baseline to what is stored NOW: `dirty` then means
     * "differs from the daemon", Cancel reveals the daemon's values, and #mapRest is
     * refreshed so a resend round-trips the CURRENT map rather than a stale one.
     * Never discard typed input on an inference; hedge the wording instead.
     */
    async #reconcileAfterTimeout(before: GeneralForm, sent: GeneralForm): Promise<void> {
        const res = await fetchGeneral();
        if (res.kind === 'error') {
            // Re-read failed too — the whole-block state can't be refreshed here, so
            // Save stays enabled and dirty. A retry is the next chance to reconcile:
            // a later timed-out PUT re-reads and merges, and one that lands is a
            // deliberate whole-block write. The only residual is the inherent
            // whole-block hazard (a retry that lands cleanly drops a concurrent change
            // to an UNTOUCHED band), which every save shares and which matches the FT8
            // section's identical branch; the map carries no opaque fields beyond
            // band_colors (types.MapConfig), so nothing else can be reverted. Blocking
            // Save until a reload was weighed and rejected — the reload discards the
            // operator's draft under the daemon-authoritative design, a worse cost for
            // this rare double-fault. Clean-room review 69ed99d9 P1 (accepted).
            toasts.error(
                'Save timed out and the daemon could not be re-read — it is unknown whether your General settings were stored.'
            );
            return;
        }
        const stored: GeneralForm = {
            restoreRigOnModeSwitch: res.config.restoreRigOnModeSwitch,
            bandColors: res.config.bandColors,
        };
        this.form = mergeGeneral(before, sent, this.form, stored);
        this.#rebaseline(res.config);
        toasts.warn(
            'Save timed out — the daemon may or may not have stored your changes. Your edits are kept; review and save again if needed.'
        );
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

    // Load a config into the form AND the baseline (initial load / explicit reset
    // point). Overwrites the live form, so it is NOT used after a save — see
    // #rebaseline for why a save keeps the form.
    #apply(cfg: GeneralConfig): void {
        this.form = {
            restoreRigOnModeSwitch: cfg.restoreRigOnModeSwitch,
            bandColors: { ...cfg.bandColors },
        };
        this.#rebaseline(cfg);
    }

    // Adopt cfg as the saved baseline WITHOUT touching the live form. Used after a
    // save (success or timeout reconcile) so an in-flight edit survives as unsaved
    // work; also refreshes the opaque map fields echoed back on the next save.
    #rebaseline(cfg: GeneralConfig): void {
        this.#mapRest = cfg.mapRest;
        this.#pristine = canon({
            restoreRigOnModeSwitch: cfg.restoreRigOnModeSwitch,
            bandColors: cfg.bandColors,
        });
    }
}

export const generalState = new GeneralState();
