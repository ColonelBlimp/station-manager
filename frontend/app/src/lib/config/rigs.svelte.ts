/*
    Rigs settings state (app Settings → Rigs section, ADR 0044) — the daemon's
    configured rig list (GET /v1/rigs) plus which one is selected in the
    master-detail view. Daemon is the source of truth (ADR 0003); no local
    cache. First increment is read-only (list + selection); the per-rig details
    editor + write path land next.
*/
import { fetchRigs, type RigConfig, type RigDef } from '../api/rigs';

class RigsState {
    loading = $state(false);
    loaded = $state(false);
    error = $state('');
    rigs = $state<RigConfig[]>([]);
    defaultRigId = $state(0);
    selectedId = $state<number | null>(null);
    // rigdef id → catalogue entry (name + identity + defaults); see nameFor/defFor.
    catalogue = $state<Record<string, RigDef>>({});

    // The rig backing the details panel, or null (no rigs / none selected).
    selected = $derived(this.rigs.find((r) => r.id === this.selectedId) ?? null);

    // The rigdef catalogue entry for a rig, or undefined (unknown/legacy rigdef).
    defFor(rig: RigConfig): RigDef | undefined {
        return this.catalogue[rig.model];
    }

    // The friendly rigdef name for display, falling back to the raw model id if
    // the catalogue doesn't carry it.
    nameFor(rig: RigConfig): string {
        return this.catalogue[rig.model]?.name ?? rig.model;
    }

    // The effective FT8 mode: the rig's own override, else the rigdef default.
    ft8ModeFor(rig: RigConfig): string {
        return rig.ft8_mode || this.catalogue[rig.model]?.ft8_mode || '';
    }

    async load(): Promise<void> {
        if (this.loading) return;
        this.loading = true;
        this.error = '';
        const res = await fetchRigs();
        this.loading = false;
        if (res.kind === 'error') {
            this.error = res.message;
            return;
        }
        this.rigs = res.data.rigs;
        this.defaultRigId = res.data.defaultRigId;
        this.catalogue = res.data.catalogue;
        // Reconcile the selection against the (possibly changed) list: keep a
        // still-valid manual selection, otherwise fall back to the active rig,
        // then the first, then null. Guarding only on `selectedId === null`
        // would strand a selection whose rig disappeared on reload — no list
        // item highlighted and an empty detail panel (review 2026-07-20 Rigs #2).
        const stillSelected =
            this.selectedId !== null && this.rigs.some((r) => r.id === this.selectedId);
        if (!stillSelected) {
            this.selectedId = this.rigs.some((r) => r.id === this.defaultRigId)
                ? this.defaultRigId
                : (this.rigs[0]?.id ?? null);
        }
        this.loaded = true;
    }

    select(id: number): void {
        this.selectedId = id;
    }
}

export const rigsState = new RigsState();
