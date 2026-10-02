// Browser storage for saved drafts (ADR 0085): IndexedDB, one record per draft
// id. Recovery within this browser profile and origin, not a station backup.
// A write resolves only once its transaction has COMPLETED (strict durability),
// so the reload that follows a save cannot outrun it.

import { readSavedDraft, type SavedDraft } from './savedDraft';

export interface DraftStore {
    put(record: SavedDraft): Promise<void>;
    list(): Promise<unknown[]>;
    remove(id: string): Promise<void>;
}

const DB_NAME = 'station-manager';
const DB_VERSION = 1;
const STORE = 'saved-qso-drafts';

function openDb(): Promise<IDBDatabase> {
    return new Promise((resolve, reject) => {
        if (typeof indexedDB === 'undefined') {
            reject(new Error('browser storage (IndexedDB) is not available'));
            return;
        }
        const req = indexedDB.open(DB_NAME, DB_VERSION);
        req.onupgradeneeded = () => {
            if (!req.result.objectStoreNames.contains(STORE)) {
                req.result.createObjectStore(STORE, { keyPath: 'id' });
            }
        };
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => reject(req.error ?? new Error('browser storage could not be opened'));
        req.onblocked = () => reject(new Error('browser storage is blocked by another tab'));
    });
}

async function write(op: (store: IDBObjectStore) => void): Promise<void> {
    const db = await openDb();
    try {
        await new Promise<void>((resolve, reject) => {
            const tx = db.transaction(STORE, 'readwrite', { durability: 'strict' });
            tx.oncomplete = () => resolve();
            tx.onerror = () => reject(tx.error ?? new Error('the storage write failed'));
            tx.onabort = () => reject(tx.error ?? new Error('the storage write was aborted'));
            op(tx.objectStore(STORE));
        });
    } finally {
        db.close();
    }
}

async function readAll(): Promise<unknown[]> {
    // No IndexedDB at all: nothing can have been saved here (every save would
    // have failed and held its reload), so there is nothing to report.
    if (typeof indexedDB === 'undefined') return [];
    const db = await openDb();
    try {
        return await new Promise<unknown[]>((resolve, reject) => {
            const req = db.transaction(STORE, 'readonly').objectStore(STORE).getAll();
            req.onsuccess = () => resolve(req.result as unknown[]);
            req.onerror = () => reject(req.error ?? new Error('the storage read failed'));
        });
    } finally {
        db.close();
    }
}

const indexedDbStore: DraftStore = {
    put: (record) => write((s) => void s.put(record)),
    list: readAll,
    remove: (id) => write((s) => void s.delete(id)),
};

let store: DraftStore = indexedDbStore;

export function draftStore(): DraftStore {
    return store;
}

/** Saved drafts, oldest first, as version 2; a damaged or foreign record is skipped. */
export async function listSavedDrafts(): Promise<SavedDraft[]> {
    const rows = await store.list();
    return rows
        .map(readSavedDraft)
        .filter((r): r is SavedDraft => r !== null)
        .sort((a, b) => a.savedAt.localeCompare(b.savedAt));
}

/** Test seam: jsdom has no IndexedDB. null restores the real store. */
export function _setDraftStoreForTests(s: DraftStore | null): void {
    store = s ?? indexedDbStore;
}

/** An in-memory store for tests and for proving callers against failures. */
export function memoryDraftStore(): DraftStore & { rows: Map<string, SavedDraft> } {
    const rows = new Map<string, SavedDraft>();
    return {
        rows,
        put: (r) => {
            rows.set(r.id, structuredClone(r));
            return Promise.resolve();
        },
        list: () => Promise.resolve([...rows.values()]),
        remove: (id) => {
            rows.delete(id);
            return Promise.resolve();
        },
    };
}
