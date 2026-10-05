/// <reference types="vite/client" />
// O1 (ADR 0087): saved-QSO records already in a browser's IndexedDB stay
// orphaned — the application never reads, writes, migrates or deletes them.
// Behaviourally pinned by main.archiveswitch.test.ts (spies on open,
// deleteDatabase, Web Locks and BroadcastChannel through a real boot and
// rebind); this guard keeps every path closed in source: no non-test file
// under src/ names those facilities or the old store. Allowlist: empty.
import { describe, it, expect } from 'vitest';

const FILES = import.meta.glob<string>(
    ['/src/**/*.ts', '/src/**/*.svelte', '!/src/**/*.test.ts', '!/src/**/*.fixture.ts'],
    { query: '?raw', import: 'default', eager: true }
);
const FORBIDDEN = [
    /indexedDB/i,
    /navigator\s*(\.|\[\s*['"])locks/,
    /BroadcastChannel/,
    /saved-qso-drafts/,
];

describe('no browser-storage path to orphaned saved QSOs (O1)', () => {
    it('reads the application sources', () => {
        expect(Object.keys(FILES)).toContain('/src/main.ts');
        expect(Object.keys(FILES).length).toBeGreaterThan(100);
    });

    it('no application source names IndexedDB, Web Locks, BroadcastChannel or the old store', () => {
        const hits = Object.entries(FILES).flatMap(([path, text]) =>
            FORBIDDEN.filter((re) => re.test(text)).map((re) => `${path}: ${re.source}`)
        );
        expect(hits).toEqual([]);
    });
});
