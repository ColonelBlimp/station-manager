/**
 * FT8 pile-up stack — operator-curated queue of stations calling you.
 *
 * During a pile-up, stations call you (`<yourcall> <them> <grid>`). You can only
 * SEE/select them in your RX parity, and the work-now click is gated on armed+idle,
 * so callers you spot mid-QSO are gone before you can act. Ctrl/Cmd+click on a
 * calling-you decode (in ANY state — it's pure capture, no TX) pushes the station
 * here; the Operate view drains the queue, working each via the work-a-caller path,
 * while you keep adding more to the bottom.
 *
 * FIFO — worked oldest-first: a pile-up is worked in the order callers were captured.
 * The head (index 0) is worked next; new captures append at the tail.
 *
 * Persistence: in-memory only — erased on refresh / tab close. A pile-up queue is
 * live operating state, not durable config.
 *
 * Dedup: push is keyed by call; ctrl-clicking a station already queued REFRESHES its
 * grid/SNR/slot in place (a later decode is the better data to work from) without
 * changing its FIFO position. Only ctrl+click enqueues — nothing auto-pushes — so
 * dedup mostly guards an accidental double-add.
 *
 * Single-parity run: the queue carries `lockedParity` — the slot parity of the run,
 * fixed by the FIRST station added and held until the run ends (the operator clears,
 * or starts a fresh run when the queue is empty + no contact active — the enqueue
 * path in the panel enforces it). The enforcement (reject a wrong-parity add) lives
 * in the panel so it can explain itself; the stack just owns the locked value so it
 * survives the drain emptying the queue between adds (otherwise the lock evaporates
 * as the drain clears the head).
 */
import { slotParity, type SlotParity } from '../utils/ft8Parity';

export interface PileupEntry {
    call: string;
    grid: string;
    snr: number;
    /** RFC3339 start of the decode's slot — fixes the TX parity (stable even if old). */
    slotUtc: string;
    /**
     * The operator queued this station KNOWING they were already worked this session
     * (a repair, a sked, a second report). The drain drops an already-worked head as
     * stale queue hygiene — an entry queued before the station got worked some other
     * way — but that must not silently discard a deliberate repeat: the panel accepts
     * the add and says so, and dropping it anyway would make the accepted action
     * impossible to carry out (codex 0f9aa672 P1).
     */
    repeat?: boolean;
}

class Ft8PileupStack {
    /** Queued callers, oldest (worked next) at index 0; new captures append at the tail. */
    items: PileupEntry[] = $state([]);

    /** Queue depth — for the drawer header / a badge. */
    count: number = $derived(this.items.length);

    /**
     * Whether the queue auto-drains. True by default, so a freshly-built stack works
     * itself off as soon as the rig is armed + idle. Abandon pauses it (keeps the
     * queue so the operator regains control without losing their captures); resume()
     * re-enables.
     */
    enabled: boolean = $state(true);

    /**
     * The run's locked slot parity ('' = no run / unlocked). Set by the first push of a
     * run; the panel rejects any later add whose parity differs, and clears it (clearLock)
     * when a fresh run starts (queue empty + no contact) or on clear()/Clear-all. Held
     * across the drain emptying the queue mid-run so the lock can't evaporate between adds.
     */
    lockedParity: SlotParity | '' = $state('');

    /**
     * Enqueue a caller at the tail. Normalises the call (trim + upper); silent no-op
     * on empty. Dedup by call: an already-queued station's grid/SNR/slot refresh in
     * place (newer decode = better data) without reordering.
     *
     * Returns true when a NEW entry was appended, false when an existing one was
     * refreshed (or the call was empty). Callers use this to distinguish "a genuinely
     * new caller arrived" from "re-clicked one already queued" — e.g. only a new
     * caller resumes a paused drain.
     */
    push(entry: PileupEntry): boolean {
        const call = entry.call.trim().toUpperCase();
        if (call === '') return false;
        const e: PileupEntry = { ...entry, call };
        const i = this.items.findIndex((x) => x.call === call);
        if (i >= 0) {
            this.items = this.items.map((x, idx) => (idx === i ? e : x));
            return false;
        }
        this.items = [...this.items, e];
        // First station of a run fixes the run's parity (if not already locked). The
        // panel enforces the lock on subsequent adds; this just records it.
        if (this.lockedParity === '') {
            const p = slotParity(e.slotUtc);
            if (p !== '') this.lockedParity = p;
        }
        return true;
    }

    /** The head (worked next), or undefined when empty. */
    peek(): PileupEntry | undefined {
        return this.items[0];
    }

    /** Remove and return the head (FIFO). */
    dequeue(): PileupEntry | undefined {
        if (this.items.length === 0) return undefined;
        const head = this.items[0];
        this.items = this.items.slice(1);
        return head;
    }

    /** Remove the entry at index — the drawer's per-entry remove. Out-of-bounds no-op. */
    remove(index: number): void {
        if (index < 0 || index >= this.items.length) return;
        this.items = [...this.items.slice(0, index), ...this.items.slice(index + 1)];
    }

    /**
     * Move the entry at index one place toward the head (worked sooner) — the drawer's
     * per-entry up-arrow, so the operator can prioritise a particular caller without
     * clearing the queue. No-op at the head (index 0) or out-of-bounds.
     */
    moveUp(index: number): void {
        if (index <= 0 || index >= this.items.length) return;
        const next = [...this.items];
        [next[index - 1], next[index]] = [next[index], next[index - 1]];
        this.items = next;
    }

    /** Empty the queue — the drawer's Clear-all. Also unlocks the run parity. */
    clear(): void {
        this.items = [];
        this.lockedParity = '';
    }

    /** Unlock the run parity without touching the queue — used when a fresh run starts
     *  (queue empty + no contact active), so the next add can set a new parity. */
    clearLock(): void {
        this.lockedParity = '';
    }

    /** Pause auto-drain (Abandon) — the queue is kept. */
    pause(): void {
        this.enabled = false;
    }

    /** Resume auto-drain. */
    resume(): void {
        this.enabled = true;
    }
}

export const ft8PileupStack = new Ft8PileupStack();

/** Test seam — restore the singleton between cases. */
export function _resetPileupForTests(): void {
    ft8PileupStack.items = [];
    ft8PileupStack.enabled = true;
    ft8PileupStack.lockedParity = '';
}
