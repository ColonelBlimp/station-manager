// Spies on the browser facilities ADR 0085's saved-QSO recovery used — IndexedDB
// (open AND deleteDatabase), Web Locks and BroadcastChannel — so a test can prove
// ADR 0087's "orphaned records stay untouched": nothing opens, deletes, locks or
// announces. An open fails asynchronously, as a blocked or broken store does; a
// page that still read the store would surface that failure (the old Unlogged
// QSOs control showed "(?)"), so the fixture makes old and new behaviour differ.
import { vi } from 'vitest';

export interface BrowserStorageSpies {
    opens: string[];
    deletes: string[];
    locks: string[];
    channels: string[];
    restore(): void;
}

export function installBrowserStorageSpies(): BrowserStorageSpies {
    const spies: BrowserStorageSpies = {
        opens: [],
        deletes: [],
        locks: [],
        channels: [],
        restore: () => {
            vi.unstubAllGlobals();
            delete (navigator as { locks?: unknown }).locks;
        },
    };
    const failing = () => {
        const req: Record<string, unknown> = {};
        queueMicrotask(() => {
            req.error = new Error('blocked by the test');
            (req.onerror as (() => void) | undefined)?.();
        });
        return req;
    };
    vi.stubGlobal('indexedDB', {
        open: (name: string) => {
            spies.opens.push(name);
            return failing();
        },
        deleteDatabase: (name: string) => {
            spies.deletes.push(name);
            return failing();
        },
    });
    vi.stubGlobal(
        'BroadcastChannel',
        class {
            constructor(name: string) {
                spies.channels.push(name);
            }
            postMessage(): void {}
            addEventListener(): void {}
            removeEventListener(): void {}
            close(): void {}
        }
    );
    Object.defineProperty(navigator, 'locks', {
        configurable: true,
        value: {
            request: (name: string) => {
                spies.locks.push(name);
                return new Promise(() => {});
            },
            query: () => Promise.resolve({ held: [], pending: [] }),
        },
    });
    return spies;
}
