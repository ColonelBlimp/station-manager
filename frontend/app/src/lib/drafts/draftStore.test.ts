/*
    The IndexedDB store where the browser has none (jsdom has none): a save
    FAILS — so its reload is held rather than the QSO lost — and a read holds
    nothing, since nothing can have been saved.
*/
import { describe, expect, it } from 'vitest';
import { draftStore, listSavedDrafts } from './draftStore';
import { sampleRecord } from './savedDraft.fixture';

describe('draftStore without IndexedDB', () => {
    it('a save is refused', async () => {
        expect(typeof indexedDB).toBe('undefined');
        await expect(draftStore().put(sampleRecord())).rejects.toThrow(/not available/);
    });
    it('a read holds nothing', async () => {
        expect(await listSavedDrafts()).toEqual([]);
    });
});
