<script lang="ts">
    // Per-rig Serial overrides editor (Rigs tab, Step 3) — advanced, collapsed by
    // default. Each row shadows one serial parameter; a blank field inherits the
    // rigdef default (zero-value-means-inherit, types.RigOverrides). The bridge
    // applies these at open (Config.ActiveBridge → buildSerialConfig), so a change
    // is restart-only — the Rigs-tab restart banner already covers it.
    //
    // Stores only the fields the operator set (rig.overrides), matching what the
    // daemon persists/returns. Numeric fields parse on reconcile; a bad value is
    // caught daemon-side on save. Parent keys this by `${rig.id}:${rig.model}` so
    // it remounts (fresh snapshot) when the rig or model changes.
    import { untrack } from 'svelte';
    import type { RigConfig, RigDefSummary, RigOverrides } from '../api/rigs';

    let { rig, rigdef }: { rig: RigConfig; rigdef: RigDefSummary | undefined } = $props();

    // The six overridable serial fields (types.RigOverrides). `numeric` drives
    // input mode + parse; `def` reads the rigdef default for the placeholder.
    const FIELDS = [
        { key: 'baud_rate', label: 'Baud rate', numeric: true },
        { key: 'data_bits', label: 'Data bits', numeric: true },
        { key: 'stop_bits', label: 'Stop bits', numeric: true },
        { key: 'parity', label: 'Parity', numeric: false },
        { key: 'line_delimiter', label: 'Line delimiter', numeric: false },
        { key: 'read_timeout_ms', label: 'Read timeout (ms)', numeric: true },
    ] as const satisfies ReadonlyArray<{
        key: keyof RigOverrides;
        label: string;
        numeric: boolean;
    }>;

    function defaultFor(key: keyof RigOverrides): string {
        const v = rigdef?.serial?.[key as keyof NonNullable<RigDefSummary['serial']>];
        return v === undefined || v === null ? '' : String(v);
    }

    // Local editable snapshot — all strings for uniform inputs. Populated once on
    // mount via an untracked $effect.pre (reading rig.overrides here must not make
    // this a dependency, else the reconcile effect's writes would wipe edits).
    let editing: Record<string, string> = $state({});
    let keyOrder: string[] = [];
    $effect.pre(() => {
        untrack(() => {
            const fresh: Record<string, string> = {};
            for (const f of FIELDS) {
                const v = rig.overrides?.[f.key];
                fresh[f.key] = v ? String(v) : ''; // 0 / '' / absent → inherit
            }
            editing = fresh;
            // Remember the ORDER the value arrived in. Emitting in this order
            // below is what lets clear-then-restore (and edit-then-revert)
            // reproduce the original object byte-for-byte: `delete` drops a
            // key's position, so re-setting it would otherwise append and the
            // JSON.stringify dirty check would read a reverted edit as a real
            // one (reviews of 0e8cec2e and 3335fdab).
            keyOrder = Object.keys(rig.overrides ?? {});
        });
    });

    // Reconcile edits → rig.overrides (only the set fields). Skip the initial run
    // (no churn on open) + guard the write by value-equality (no spurious dirty).
    let primed = false;
    $effect(() => {
        JSON.stringify(editing); // deep dependency on the edits
        if (!primed) {
            primed = true;
            return;
        }
        // Fields this editor does NOT manage must SURVIVE a save. rts/dtr are
        // tri-state serial-line controls set by hand in config.json (no UI), and
        // rebuilding `overrides` from only the visible fields silently dropped
        // them: an operator who set "rts": true and later touched baud lost the
        // line override, and the daemon fell back to the rigdef default on
        // restart — on hardware that genuinely needs the line asserted, that
        // stops the rig working (review of 60a8e7ae). Carry anything unknown
        // through untouched rather than enumerating it here, so a future field
        // added daemon-side is preserved without a matching UI change.
        //
        // Start from a COPY of the current value and edit in place, rather than
        // rebuilding from scratch: that preserves the server's key ORDER. The
        // dirty check below (and the panel's baseline comparison) are
        // JSON.stringify equality, so re-emitting the same settings in a
        // different order reads as an edit — an override could not be cleanly
        // reverted, and the spurious diff reaches the save merge (review of
        // 0e8cec2e). Managed keys are set or deleted below; anything else rides
        // along untouched, in its original position.
        const values: Record<string, unknown> = { ...(rig.overrides ?? {}) };
        const setOrDrop = (key: keyof RigOverrides, v: string | number | undefined): void => {
            if (v === undefined) delete values[key];
            else values[key] = v;
        };
        const num = (s: string): number | undefined => {
            const n = parseInt(s.trim(), 10);
            return Number.isFinite(n) && n > 0 ? n : undefined;
        };
        setOrDrop('baud_rate', num(editing.baud_rate));
        setOrDrop('data_bits', num(editing.data_bits));
        setOrDrop('stop_bits', num(editing.stop_bits));
        setOrDrop('read_timeout_ms', num(editing.read_timeout_ms));
        setOrDrop('parity', editing.parity.trim() !== '' ? editing.parity.trim() : undefined);
        // Delimiter is byte-exact (e.g. ";", "0xFD") — don't trim.
        setOrDrop(
            'line_delimiter',
            editing.line_delimiter !== '' ? editing.line_delimiter : undefined
        );

        // Emit in the remembered order first, then anything new, so an object
        // that is semantically back to where it started is also byte-identical.
        const next: RigOverrides = {};
        for (const k of keyOrder) {
            if (values[k] !== undefined) (next as Record<string, unknown>)[k] = values[k];
        }
        for (const k of Object.keys(values)) {
            if (!keyOrder.includes(k)) (next as Record<string, unknown>)[k] = values[k];
        }

        const nextVal = Object.keys(next).length > 0 ? next : undefined;
        if (JSON.stringify(rig.overrides ?? null) !== JSON.stringify(nextVal ?? null)) {
            rig.overrides = nextVal;
        }
    });

    const overrideCount = $derived(rig.overrides ? Object.keys(rig.overrides).length : 0);
    function isOverridden(key: keyof RigOverrides): boolean {
        return rig.overrides?.[key] != null && rig.overrides[key] !== '';
    }
</script>

<details class="rounded-md border border-gray-200">
    <summary class="cursor-pointer px-3 py-2 text-sm font-medium text-gray-700 select-none">
        Serial overrides
        <span class="font-normal text-gray-400">
            {#if overrideCount > 0}
                ({overrideCount} override{overrideCount === 1 ? '' : 's'})
            {:else}
                (using rigdef defaults)
            {/if}
        </span>
    </summary>

    <div class="border-t border-gray-200 px-3 py-3">
        <p class="mb-3 text-xs text-gray-400">
            Advanced — most rigs work on the rigdef defaults. Leave a field blank to inherit the
            default shown. Changes apply on daemon restart; bad values are rejected on save.
        </p>
        <ul class="space-y-2">
            {#each FIELDS as f (f.key)}
                <li class="flex items-center gap-3 text-sm">
                    <span class="w-36 shrink-0 text-gray-700">{f.label}</span>
                    <input
                        type="text"
                        inputmode={f.numeric ? 'numeric' : 'text'}
                        placeholder={defaultFor(f.key) || '—'}
                        bind:value={editing[f.key]}
                        autocomplete="off"
                        spellcheck="false"
                        class="w-32 rounded-md border border-gray-300 px-2 py-1 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                    />
                    <span
                        class="text-xs {isOverridden(f.key) ? 'text-indigo-600' : 'text-gray-400'}"
                    >
                        {isOverridden(f.key) ? 'override' : `default: ${defaultFor(f.key) || '—'}`}
                    </span>
                </li>
            {/each}
        </ul>
    </div>
</details>
