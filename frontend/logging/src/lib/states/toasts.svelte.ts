/*
    Toast notification state singleton (ADR 0008).

    Imperative push API for fire-and-forget event signalling, plus a
    reactive `$state` array for components that want to subscribe to
    the queue (e.g. a future "recent events" status bar).

    Per ADR 0008:
      - Three levels: info / warn / error.
      - Per-level default TTL: info=4s, warn=6s, error=8s.
      - ttl=0 opts out of auto-dismiss (sticky; rare — most "must be
        acknowledged" cases want a banner instead).
      - Click-to-dismiss is always available regardless of TTL.
      - Max stack depth: 5. Pushing a sixth drops the oldest. Prevents
        runaway queues from a misbehaving event source obscuring the UI.

    The renderer (`Toasts.svelte`) reads `toastsState.items` and renders
    each item with its level-keyed Tailwind class.
*/

export type ToastLevel = 'info' | 'warn' | 'error';

export interface Toast {
    id: number;
    level: ToastLevel;
    message: string;
    createdAt: number;
    ttl: number; // milliseconds; 0 = sticky
}

const MAX_STACK = 5;

const DEFAULT_TTL: Record<ToastLevel, number> = {
    info: 4000,
    warn: 6000,
    error: 8000,
};

class ToastsState {
    items: Toast[] = $state([]);
}

export const toastsState = new ToastsState();

let nextId = 1;
const timers = new Map<number, ReturnType<typeof setTimeout>>();

/*
    pushToast — append a toast to the queue. Returns the assigned id so
    callers can `dismissToast(id)` ahead of TTL if context changes (e.g.
    a "saving…" toast superseded by "saved").

    Behaviour:
      - id assigned from a monotonic counter.
      - ttl resolves to the level's default when undefined; passing 0
        explicitly opts out of auto-dismiss.
      - When the array hits MAX_STACK, the oldest item is shifted off
        before the new one is pushed (its timer, if any, is cleared so
        we don't dismiss it post-eviction).
*/
export function pushToast(level: ToastLevel, message: string, ttl?: number): number {
    const id = nextId++;
    const resolvedTtl = ttl ?? DEFAULT_TTL[level];
    const toast: Toast = {
        id,
        level,
        message,
        createdAt: Date.now(),
        ttl: resolvedTtl,
    };

    if (toastsState.items.length >= MAX_STACK) {
        const evicted = toastsState.items.shift();
        if (evicted) clearScheduledDismiss(evicted.id);
    }
    toastsState.items.push(toast);

    if (resolvedTtl > 0) {
        const handle = setTimeout(() => {
            dismissToast(id);
        }, resolvedTtl);
        timers.set(id, handle);
    }

    return id;
}

/*
    dismissToast — remove the toast with the given id, if it's still in
    the queue. Idempotent: dismissing an already-dismissed (or
    already-evicted) id is a no-op. Cancels the pending TTL timer.
*/
export function dismissToast(id: number): void {
    const idx = toastsState.items.findIndex((t) => t.id === id);
    if (idx >= 0) {
        toastsState.items.splice(idx, 1);
    }
    clearScheduledDismiss(id);
}

function clearScheduledDismiss(id: number): void {
    const handle = timers.get(id);
    if (handle !== undefined) {
        clearTimeout(handle);
        timers.delete(id);
    }
}

/*
    Convenience wrappers — most call sites just want `toasts.error('...')`
    rather than `pushToast('error', '...')`.
*/
export const toasts = {
    info: (message: string, ttl?: number): number => pushToast('info', message, ttl),
    warn: (message: string, ttl?: number): number => pushToast('warn', message, ttl),
    error: (message: string, ttl?: number): number => pushToast('error', message, ttl),
    dismiss: dismissToast,
};

/*
    _resetForTests — clears the queue and pending timers. Test-only;
    call between cases when sharing the singleton across tests. Marked
    underscore-prefixed to keep it out of normal autocomplete usage.
*/
export function _resetForTests(): void {
    for (const handle of timers.values()) clearTimeout(handle);
    timers.clear();
    toastsState.items.length = 0;
    nextId = 1;
}
