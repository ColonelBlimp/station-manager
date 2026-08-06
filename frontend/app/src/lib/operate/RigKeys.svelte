<script lang="ts">
    // Operate-wide rig-control keyboard shortcuts (ADR 0026). Mounted once in
    // Operate.svelte — NOT the Phone/CW-only LoggingCard — so the SAME keystroke
    // drives the rig in both Phone/CW and FT8. That consistency is free because
    // the shortcuts bind to the shared rig ACTIONS (rig.svelte.ts), so there's
    // one behaviour, not a per-mode keymap.
    //
    // The whole family is Ctrl+Shift+<key>, dispatched on the PHYSICAL key
    // (e.code) because Shift mutates the character (Shift+] = }, Shift+1 = !).
    // That keeps rig control in its own modifier namespace, distinct from the
    // logging shortcuts (Esc / Ctrl+Enter / Tab) in LoggingCard — the two
    // <svelte:window> handlers never touch the same key. Page keys are avoided:
    // Firefox's Ctrl+Shift+PageUp/Down move-tab won't yield to preventDefault.
    import {
        swapVfo,
        bandUp,
        bandDown,
        selectBand,
        bandForDigit,
        nudgeFreqCoarse,
        nudgeFreqFine,
        nudgeFreqJump,
        type RigWriteResult,
    } from './rig.svelte';
    import { operate } from './state.svelte';
    import { submitState } from './qso.svelte';
    import { sessionEdit } from './sessionEdit.svelte';
    import { toasts } from '../ui/toasts.svelte';

    // A text field is focused → Ctrl+Shift+Arrow is native word-select, so the
    // freq-step arrows stand down (matches the shipping SPA). Swap / band / digit
    // aren't native editing combos, so they fire regardless of focus.
    function isTextEntry(t: EventTarget | null): boolean {
        if (!(t instanceof HTMLElement)) return false;
        const tag = t.tagName;
        return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || t.isContentEditable;
    }

    // Surface a genuine command rejection; silent no-ops (off-CAT, not-exposed)
    // return ok:true with no message, so key-repeat never spams toasts.
    function run(p: Promise<RigWriteResult>): void {
        void p.then((r) => {
            if (!r.ok && r.message !== '') toasts.error(r.message);
        });
    }

    function onKeydown(e: KeyboardEvent): void {
        // A modal overlay owns the keyboard while open — don't tune underneath it.
        // The session-edit modal is the one that bit: its own window handler
        // does Escape/Ctrl+Enter, but window listeners don't shadow each
        // other, so the rig family stayed live and could detune a live rig
        // under the editor (keyboard audit 2026-08-06, A27 in
        // pileupKeys.svelte.test.ts). (The pile-up drawer is docked, not
        // modal, so shortcuts stay live there.)
        if (operate.exportOpen || submitState.duplicate || sessionEdit.row !== null) return;
        if (!e.ctrlKey || !e.shiftKey) return; // the rig family is Ctrl+Shift only

        if (e.code === 'Backslash') {
            e.preventDefault();
            run(swapVfo());
            return;
        }
        if (e.code === 'BracketRight') {
            e.preventDefault();
            run(bandUp());
            return;
        }
        if (e.code === 'BracketLeft') {
            e.preventDefault();
            run(bandDown());
            return;
        }
        const band = bandForDigit(e.code);
        if (band !== undefined) {
            e.preventDefault();
            run(selectBand(band));
            return;
        }

        // Freq step on the arrow cluster, gated on !typing so word-select still
        // works in a field: ↑/↓ = coarse (±100 Hz), →/← = fine (±10 Hz),
        // Alt+↑/↓ = a ±5 kHz hop. Up/right = higher.
        if (isTextEntry(e.target)) return;
        if (e.code === 'ArrowUp') {
            e.preventDefault();
            run(e.altKey ? nudgeFreqJump(1) : nudgeFreqCoarse(1));
            return;
        }
        if (e.code === 'ArrowDown') {
            e.preventDefault();
            run(e.altKey ? nudgeFreqJump(-1) : nudgeFreqCoarse(-1));
            return;
        }
        if (e.code === 'ArrowRight') {
            e.preventDefault();
            run(nudgeFreqFine(1));
            return;
        }
        if (e.code === 'ArrowLeft') {
            e.preventDefault();
            run(nudgeFreqFine(-1));
        }
    }
</script>

<svelte:window onkeydown={onKeydown} />
