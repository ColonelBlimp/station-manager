/**
 * FT8 pile-up stack — operator-curated queue of stations calling you.
 *
 * During a pile-up, stations call you (`<yourcall> <them> <grid>`). You can only
 * SEE/select them in your RX parity, and the work-now click is gated on armed+idle,
 * so callers you spot mid-QSO are gone before you can act. Ctrl+click on a calling-
 * you decode (in ANY state — it's pure capture, no TX) pushes the station here; the
 * Operate view drains the queue, working each via the work-a-caller path, while you
 * keep adding more to the bottom.
 *
 * FIFO — worked oldest-first (the OPPOSITE of the Phone/CW callsignStack, which is
 * LIFO): a pile-up is worked in the order callers were captured. The head (index 0)
 * is worked next; new captures append at the tail.
 *
 * Persistence: in-memory only — erased on refresh / tab close, matching
 * callsignStack. A pile-up queue is live operating state, not durable config.
 *
 * Dedup: push is keyed by call; ctrl-clicking a station already queued REFRESHES its
 * grid/SNR/slot in place (a later decode is the better data to work from) without
 * changing its FIFO position. Only ctrl+click enqueues — nothing auto-pushes — so
 * dedup mostly guards an accidental double-add.
 */
export interface PileupEntry {
    call: string;
    grid: string;
    snr: number;
    /** RFC3339 start of the decode's slot — fixes the TX parity (stable even if old). */
    slotUtc: string;
}

class Ft8PileupStack {
    /** Queued callers, oldest (worked next) at index 0; new captures append at the tail. */
    items: PileupEntry[] = $state([]);

    /** Queue depth — for the Operate-tab badge / drawer header. */
    count: number = $derived(this.items.length);

    /**
     * Whether the queue auto-drains. True by default, so a freshly-built stack works
     * itself off as soon as the rig is armed + idle. Abandon pauses it (keeps the
     * queue so the operator regains control without losing their captures); resume()
     * re-enables.
     */
    enabled: boolean = $state(true);

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

    /** Empty the queue — the drawer's Clear-all. */
    clear(): void {
        this.items = [];
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
