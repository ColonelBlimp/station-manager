<script lang="ts">
    // Per-rig Serial overrides editor (app Settings → Rigs section, ADR 0044) —
    // ported from the config SPA's SerialOverridesEditor, restyled to the app's
    // tokens. Advanced, collapsed by default. Each row shadows one serial
    // parameter; a blank field inherits the rigdef default (zero-value-means-
    // inherit, types.RigOverrides). The bridge applies these at open
    // (Config.ActiveBridge → internal/rigserial), so a change is restart-only —
    // the section's restart note already covers it.
    //
    // Stores only the fields the operator set (rig.overrides), matching what the
    // daemon persists/returns. Numeric fields parse on reconcile: a non-positive or
    // non-numeric entry is dropped (inherits the default), so it can't be stored.
    // Other invalid values (e.g. an unknown parity string) are NOT blocked at save
    // — the config PUT doesn't validate overrides; they're caught when the bridge
    // COMPOSES the serial config at open (rigserial rejects them → a bridge error
    // on the next restart). `rig` is the RigsSection draft, so writing rig.overrides
    // flips rigsState.dirty. Parent keys this by `${rig.id}:${rig.model}` so it
    // remounts (fresh snapshot) when the rig or model changes.
    import { untrack } from 'svelte';
    import type { RigConfig, RigDef, RigOverrides } from '../api/rigs';

    // `disabled` mirrors the connection pickers: inputs lock while a save is in
    // flight so a mid-save edit can't be dropped by the post-save re-baseline.
    let {
        rig,
        rigdef,
        disabled = false,
    }: { rig: RigConfig; rigdef: RigDef | undefined; disabled?: boolean } = $props();

    // The six overridable serial fields (types.RigOverrides). `numeric` drives the
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
        const v = rigdef?.serial?.[key as keyof NonNullable<RigDef['serial']>];
        return v === undefined || v === null ? '' : String(v);
    }

    // Local editable snapshot — all strings for uniform inputs. Populated once on
    // mount via an untracked $effect.pre (reading rig.overrides here must not make
    // this a dependency, else the reconcile effect's writes would wipe edits).
    let editing: Record<string, string> = $state({});
    $effect.pre(() => {
        untrack(() => {
            const fresh: Record<string, string> = {};
            for (const f of FIELDS) {
                const v = rig.overrides?.[f.key];
                fresh[f.key] = v ? String(v) : ''; // 0 / '' / absent → inherit
            }
            editing = fresh;
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
        const next: RigOverrides = {};
        const num = (s: string): number | undefined => {
            const n = parseInt(s.trim(), 10);
            return Number.isFinite(n) && n > 0 ? n : undefined;
        };
        if (num(editing.baud_rate) !== undefined) next.baud_rate = num(editing.baud_rate);
        if (num(editing.data_bits) !== undefined) next.data_bits = num(editing.data_bits);
        if (num(editing.stop_bits) !== undefined) next.stop_bits = num(editing.stop_bits);
        if (num(editing.read_timeout_ms) !== undefined)
            next.read_timeout_ms = num(editing.read_timeout_ms);
        if (editing.parity.trim() !== '') next.parity = editing.parity.trim();
        // Delimiter is byte-exact (e.g. ";", "0xFD") — don't trim.
        if (editing.line_delimiter !== '') next.line_delimiter = editing.line_delimiter;

        const nextVal = Object.keys(next).length > 0 ? next : undefined;
        if (JSON.stringify(rig.overrides ?? null) !== JSON.stringify(nextVal ?? null)) {
            rig.overrides = nextVal;
        }
    });

    const overrideCount = $derived(rig.overrides ? Object.keys(rig.overrides).length : 0);
    function isOverridden(key: keyof RigOverrides): boolean {
        return rig.overrides?.[key] != null && rig.overrides[key] !== '';
    }

    // Small inline field styled like .input (outline edge, accent focus) at a fixed
    // width — .input forces w-full/block, wrong for a labelled row.
    const fieldClass =
        'rounded-md bg-surface px-2 py-1 text-sm text-ink outline-1 -outline-offset-1 outline-line ' +
        'placeholder:text-muted focus:outline-2 focus:-outline-offset-2 focus:outline-focus';
</script>

<details class="rounded-md border border-line">
    <summary class="cursor-pointer px-3 py-2 text-sm font-medium text-ink select-none">
        Serial overrides
        <span class="font-normal text-muted">
            {#if overrideCount > 0}
                ({overrideCount} override{overrideCount === 1 ? '' : 's'})
            {:else}
                (using rigdef defaults)
            {/if}
        </span>
    </summary>

    <div class="border-t border-line px-3 py-3">
        <p class="mb-3 text-xs text-muted">
            Advanced — most rigs work on the rigdef defaults. Leave a field blank to inherit the
            default shown. Changes apply on daemon restart; a bad value surfaces as a bridge error
            when the rig reconnects.
        </p>
        <ul class="space-y-2">
            {#each FIELDS as f (f.key)}
                <li class="flex items-center gap-3 text-sm">
                    <span class="w-36 shrink-0 text-ink">{f.label}</span>
                    <input
                        type="text"
                        inputmode={f.numeric ? 'numeric' : 'text'}
                        placeholder={defaultFor(f.key) || '—'}
                        bind:value={editing[f.key]}
                        {disabled}
                        autocomplete="off"
                        spellcheck="false"
                        class="w-32 {fieldClass} disabled:opacity-50"
                    />
                    <span class="text-xs {isOverridden(f.key) ? 'text-focus' : 'text-muted'}">
                        {isOverridden(f.key) ? 'override' : `default: ${defaultFor(f.key) || '—'}`}
                    </span>
                </li>
            {/each}
        </ul>
    </div>
</details>
