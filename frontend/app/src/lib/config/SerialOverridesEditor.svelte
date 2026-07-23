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
        // RigOverrides and RigSerial share the same key set, so no cast is needed.
        const v = rigdef?.serial?.[key];
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
        // The WHOLE trimmed value must be a positive integer. parseInt would accept
        // a numeric prefix ("9600xyz"→9600, "8.5"→8, "1e3"→1) and silently store a
        // value different from the visible text — a surprise serial setting that
        // could fail the bridge on restart (codex 55d85876 P2). A non-integer /
        // non-positive entry → undefined (inherit the rigdef default).
        const num = (s: string): number | undefined => {
            const t = s.trim();
            if (!/^\d+$/.test(t)) return undefined;
            const n = parseInt(t, 10);
            return n > 0 ? n : undefined;
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
    </summary>

    <div class="border-t border-line px-3 py-3">
        <p class="mb-3 text-xs text-muted">
            Advanced — most rigs work on the built-in defaults. Leave a field blank to keep the
            default shown. Changes apply on restart; a bad value produces an error when the rig
            reconnects.
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
