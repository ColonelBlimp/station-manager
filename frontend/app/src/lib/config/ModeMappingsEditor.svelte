<script lang="ts">
    // Per-rig Mode Mappings editor (app Settings → Rigs section, ADR 0044) —
    // ported from the config SPA's ModeMappingsEditor, restyled to the app's
    // design tokens. Each row maps one of the rig's own mode literals (rigdef
    // `rig_modes`, e.g. "DATA-U") to an ADIF MODE + optional SUBMODE. The rigdef
    // ships sensible defaults; the operator overrides a row only when their setup
    // differs (e.g. PSK31 on DATA-U instead of FT8).
    //
    // Stores ONLY deviations in rig.mode_mappings (matching what the daemon
    // persists + returns): an edited row equal to the rigdef default, or with an
    // empty MODE, clears the override and inherits the default. Free text +
    // daemon-side validation on save (validateRigs rejects unknown ADIF values).
    //
    // `rig` is the RigsSection draft: writing rig.mode_mappings mutates the draft,
    // which flips rigsState.dirty and enables Save. The parent keys this component
    // by `${rig.id}:${rig.model}`, so it remounts (fresh snapshot) whenever the
    // selected rig or its model changes. rigs.svelte.ts save() diffs mode_mappings
    // as one field and patches it onto the fresh catalogue.
    import { untrack } from 'svelte';
    import type { ModeMapping, RigConfig, RigDef } from '../api/rigs';

    // `disabled` mirrors the connection pickers: inputs lock while a save is in
    // flight so a mid-save edit can't be silently dropped by the post-save
    // re-baseline (rigs.svelte.ts save()).
    let {
        rig,
        rigdef,
        disabled = false,
    }: { rig: RigConfig; rigdef: RigDef | undefined; disabled?: boolean } = $props();

    interface Pair {
        mode: string;
        submode: string;
    }

    const literals = $derived(rigdef?.rig_modes ?? []);

    function defaultFor(literal: string): ModeMapping | undefined {
        return rigdef?.mode_mappings?.[literal];
    }
    function effectiveFor(literal: string): Pair {
        const m = rig.mode_mappings?.[literal] ?? defaultFor(literal);
        return { mode: m?.mode ?? '', submode: m?.submode ?? '' };
    }
    function isOverridden(literal: string): boolean {
        return rig.mode_mappings?.[literal] != null;
    }

    // Local editable snapshot (one Pair per literal), bound by the row inputs.
    // Populated once on mount from the effective values via an untracked
    // $effect.pre — untracked so reading rig.mode_mappings here doesn't make this
    // a dependency (which would re-fire and wipe edits when the reconcile effect
    // below writes mode_mappings). Parent {#key} remounts for a fresh snapshot
    // per rig/model, so a one-time capture is exactly right.
    let editing: Record<string, Pair> = $state({});
    $effect.pre(() => {
        untrack(() => {
            const fresh: Record<string, Pair> = {};
            for (const lit of rigdef?.rig_modes ?? []) fresh[lit] = effectiveFor(lit);
            editing = fresh;
        });
    });

    // Reconcile edits → rig.mode_mappings (deviations only). Skips the initial
    // run (so opening the editor never churns the draft) and guards the write by
    // value-equality (so an idempotent recompute can't raise spurious dirty).
    let primed = false;
    $effect(() => {
        JSON.stringify(editing); // register a deep dependency on the edits
        if (!primed) {
            primed = true;
            return;
        }
        const next: Record<string, ModeMapping> = {};
        for (const lit of literals) {
            const p = editing[lit];
            if (!p) continue;
            const mode = p.mode.trim();
            const submode = p.submode.trim();
            if (!mode) continue; // empty MODE → inherit the default
            const d = defaultFor(lit);
            if (d && d.mode === mode && (d.submode ?? '') === submode) continue; // == default → inherit
            next[lit] = submode ? { mode, submode } : { mode };
        }
        const nextVal = Object.keys(next).length > 0 ? next : undefined;
        if (JSON.stringify(rig.mode_mappings ?? null) !== JSON.stringify(nextVal ?? null)) {
            rig.mode_mappings = nextVal;
        }
    });

    // Small inline field styled like .input (outline edge, accent focus) but at a
    // fixed width — .input forces w-full/block, wrong for the MODE/SUBMODE pair.
    const fieldClass =
        'rounded-md bg-surface px-2 py-1 text-sm text-ink outline-1 -outline-offset-1 outline-line ' +
        'placeholder:text-muted focus:outline-2 focus:-outline-offset-2 focus:outline-focus';
</script>

<details class="rounded-md border border-line">
    <summary class="cursor-pointer px-3 py-2 text-sm font-medium text-ink select-none">
        Mode mappings
    </summary>

    <div class="border-t border-line px-3 py-3">
        {#if literals.length === 0}
            <p class="text-sm text-muted">
                This rig's model defines no mode literals, so there's nothing to map.
            </p>
        {:else}
            <p class="mb-3 text-xs text-muted">
                Maps each rig mode literal to an ADIF MODE (+ optional SUBMODE). Leave MODE empty,
                or set it to the default, to inherit the rigdef default. Unknown ADIF values are
                rejected on save.
            </p>
            <ul class="space-y-2">
                {#each literals as lit (lit)}
                    {#if editing[lit]}
                        <li class="flex items-center gap-3 text-sm">
                            <span class="w-24 shrink-0 font-mono text-ink">{lit}</span>
                            <input
                                type="text"
                                placeholder="MODE"
                                bind:value={editing[lit].mode}
                                {disabled}
                                autocomplete="off"
                                spellcheck="false"
                                class="w-28 {fieldClass} disabled:opacity-50"
                            />
                            <input
                                type="text"
                                placeholder="SUBMODE (optional)"
                                bind:value={editing[lit].submode}
                                {disabled}
                                autocomplete="off"
                                spellcheck="false"
                                class="w-28 {fieldClass} disabled:opacity-50"
                            />
                            <span class="text-xs {isOverridden(lit) ? 'text-focus' : 'text-muted'}">
                                {isOverridden(lit) ? 'override' : 'default'}
                            </span>
                        </li>
                    {/if}
                {/each}
            </ul>
        {/if}
    </div>
</details>
