/**
 * Callsign stack — operator's pile-up scratchpad.
 *
 * During a pile-up the operator hears multiple callsigns at once.
 * Shift+Enter on the Callsign field pushes the typed call onto this
 * stack so the operator can capture-and-clear, then work them one at
 * a time. Shift+Up pops the most recent (top); Shift+Down pops the
 * oldest (bottom); clicking an entry in StackingDrawer pops at that
 * index. Pop = load: the call goes into qsoDraft.callsign and leaves
 * the stack (single action, one effect — no third "in both" state).
 *
 * Stack discipline: LIFO with newest-at-top. Stored as a string[] with
 * newest at index 0, so the drawer renders top-to-bottom in stack
 * order without a reverse.
 *
 * Persistence tier: in-memory only. A refresh during a pile-up is a
 * "user just made a mistake" situation; reaching for sessionStorage
 * here would couple the stack to the wrong recovery model and add
 * a state-shape migration concern for zero operator-visible benefit.
 * Aligns with qsoDraft / enrichmentState / contactHistoryState.
 *
 * Dedupe rule: push is a no-op when the normalised call is already on
 * the stack. Operators retype the same call by accident more often
 * than they intentionally stack a duplicate.
 *
 * No max-depth cap. The drawer's CSS handles overflow if a pile-up
 * actually exceeds the visible area; we'll add a cap only if runaway
 * pushing surfaces as a real problem.
 */

class CallsignStack {
    /**
     * Stacked callsigns, newest at index 0. Plain string[] because the
     * drawer iterates it directly and there's no per-entry metadata
     * worth modelling (no timestamps, no enrichment snapshot — pop
     * loads the call and the operator re-runs Tab / F2 from there).
     */
    items: string[] = $state([]);

    /**
     * Stack depth. Mirrors contactHistoryState.count so any future
     * surface that wants a badge can read this without recomputing.
     */
    count: number = $derived(this.items.length);

    /**
     * Push a callsign onto the top of the stack. Normalises (trim +
     * upper); silent no-op on empty or already-present values. The
     * caller is expected to have validated the call already — push
     * doesn't run isValidCallsign because the keyboard path
     * (QsoPanel.handleStack) already gates on it and the icon path
     * shares the same gate.
     */
    push(call: string): void {
        const normalised = call.trim().toUpperCase();
        if (normalised === '') return;
        if (this.items.includes(normalised)) return;
        this.items = [normalised, ...this.items];
    }

    /**
     * Remove and return the most recently stacked call. Returns
     * undefined when the stack is empty so callers can branch
     * without first reading length.
     */
    popTop(): string | undefined {
        if (this.items.length === 0) return undefined;
        const top = this.items[0];
        this.items = this.items.slice(1);
        return top;
    }

    /**
     * Remove and return the oldest stacked call. Symmetric to popTop;
     * Shift+Down's handler.
     */
    popBottom(): string | undefined {
        if (this.items.length === 0) return undefined;
        const last = this.items[this.items.length - 1];
        this.items = this.items.slice(0, -1);
        return last;
    }

    /**
     * Remove and return the call at the given index. Out-of-bounds
     * returns undefined. Used by the drawer's click-an-entry path.
     */
    popAt(index: number): string | undefined {
        if (index < 0 || index >= this.items.length) return undefined;
        const call = this.items[index];
        this.items = [...this.items.slice(0, index), ...this.items.slice(index + 1)];
        return call;
    }

    /**
     * Empty the stack. Provided for the drawer's "discard all" affordance
     * and for test setup.
     */
    clear(): void {
        this.items = [];
    }
}

export const callsignStack = new CallsignStack();
