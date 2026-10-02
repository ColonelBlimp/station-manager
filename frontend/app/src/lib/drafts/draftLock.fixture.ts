// Deterministic Web Locks scheduling for application tests, not browser evidence.
import { vi } from 'vitest';

export function fakeDraftLocks() {
    const held = new Set<string>();
    const request = vi.fn(
        async (name: string, options: LockOptions, callback: LockGrantedCallback<unknown>) => {
            await Promise.resolve(); // two callers can request before either callback runs
            if (held.has(name)) {
                if (!options.ifAvailable) throw new Error('Test requires a non-queued request');
                return callback(null);
            }
            held.add(name);
            try {
                return await callback({ name, mode: 'exclusive' });
            } finally {
                held.delete(name);
            }
        }
    );
    return { held, request, manager: { request } as unknown as LockManager };
}
