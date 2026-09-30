// Change notices for saved QSOs between this browser's tabs (operator ruling
// 2026-09-30). A tab announces only after its write COMMITTED; another tab
// hearing it re-reads the list from browser storage — the notice carries no
// data, so storage stays the one truth. A notice is not ownership: it gives no
// tab any claim on a record (Restore's exclusive ownership is separate).

export interface DraftChannel {
    post(): void;
    subscribe(fn: () => void): () => void;
}

const NAME = 'station-manager-saved-qsos';

function broadcastChannel(): DraftChannel {
    // One channel per page, opened lazily; none where the browser lacks it
    // (tabs then catch up when they become visible).
    let ch: BroadcastChannel | null | undefined;
    const open = (): BroadcastChannel | null => {
        if (ch === undefined) {
            ch = typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel(NAME);
        }
        return ch;
    };
    return {
        post() {
            open()?.postMessage('changed');
        },
        subscribe(fn) {
            const c = open();
            if (c === null) return () => {};
            const handler = (): void => fn();
            c.addEventListener('message', handler);
            return () => c.removeEventListener('message', handler);
        },
    };
}

const real = broadcastChannel();
let channel: DraftChannel = real;

/** Announce a committed save or discard to the other tabs. */
export function announceDraftsChanged(): void {
    channel.post();
}

/** Hear another tab's announcement. Returns the unsubscribe. */
export function onDraftsChanged(fn: () => void): () => void {
    return channel.subscribe(fn);
}

/** Test seam: null restores the real channel. */
export function _setDraftChannelForTests(c: DraftChannel | null): void {
    channel = c ?? real;
}
