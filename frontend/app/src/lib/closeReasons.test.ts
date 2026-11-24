// What a close of the app would stop or lose, as the page reports it (W-0027 A3, section A).
// The desktop app reads these attributes when a close is asked for and puts them in its one
// question. They decide only whether to ask and which lines are shown.
import { afterEach, describe, expect, it, vi } from 'vitest';

const sections = vi.hoisted(() => ({ unsaved: [] as string[], saving: [] as string[] }));
vi.mock('./config/unsaved', () => ({
    unsavedSections: () => sections.unsaved,
    savingSections: () => sections.saving,
}));

import { computeCloseReasons, publishCloseReasons } from './closeReasons';
import { draft, resetDraft, submitState } from './operate/qso.svelte';

afterEach(() => {
    sections.unsaved = [];
    sections.saving = [];
    resetDraft();
    submitState.busy = false;
    submitState.uncertain = false;
});

describe('computeCloseReasons', () => {
    it('reports nothing when nothing would be lost', () => {
        expect(computeCloseReasons()).toEqual({ entry: '', unsaved: [], saving: [] });
    });

    it('reports a contact being entered', () => {
        draft.callsign = 'K1ABC';
        expect(computeCloseReasons().entry).toBe('draft');
    });

    it('reports a log attempt whose outcome is not known, in flight or uncertain', () => {
        submitState.busy = true;
        expect(computeCloseReasons().entry).toBe('unknown');
        submitState.busy = false;
        submitState.uncertain = true;
        expect(computeCloseReasons().entry).toBe('unknown');
    });

    it('names the contact once: an unknown outcome is not also a draft to discard', () => {
        // While a log attempt is out, what was typed is still in the form. The question
        // must not say that contact "will be discarded": it may already be stored.
        draft.callsign = 'K1ABC';
        submitState.busy = true;
        expect(computeCloseReasons().entry).toBe('unknown');
    });

    it('reports unsaved Settings sections, in the order given', () => {
        sections.unsaved = ['Station', 'Email'];
        expect(computeCloseReasons()).toEqual({
            entry: '',
            unsaved: ['Station', 'Email'],
            saving: [],
        });
    });

    it('names a section once: one being saved is not also unsaved', () => {
        // A section that is being saved is usually still unsaved until its answer comes.
        sections.unsaved = ['Station', 'General'];
        sections.saving = ['General'];
        expect(computeCloseReasons()).toEqual({
            entry: '',
            unsaved: ['Station'],
            saving: ['General'],
        });
    });

    it('reports every reason together', () => {
        draft.callsign = 'K1ABC';
        sections.unsaved = ['Rigs'];
        sections.saving = ['Email'];
        expect(computeCloseReasons()).toEqual({
            entry: 'draft',
            unsaved: ['Rigs'],
            saving: ['Email'],
        });
    });
});

describe('publishCloseReasons', () => {
    const read = (el: Element): Array<string | null> => [
        el.getAttribute('data-sm-close-entry'),
        el.getAttribute('data-sm-close-unsaved'),
        el.getAttribute('data-sm-close-saving'),
    ];

    it('writes three attributes of its own, empty when there is nothing', () => {
        const el = document.createElement('div');
        publishCloseReasons(el);
        expect(read(el)).toEqual(['', '', '']);
    });

    it('writes the reasons, sections separated by a bar', () => {
        const el = document.createElement('div');
        submitState.uncertain = true;
        sections.unsaved = ['Station', 'FT8 / FT4'];
        sections.saving = ['General'];
        publishCloseReasons(el);
        expect(read(el)).toEqual(['unknown', 'Station|FT8 / FT4', 'General']);
    });

    it('clears a reason that has gone', () => {
        const el = document.createElement('div');
        draft.callsign = 'K1ABC';
        publishCloseReasons(el);
        expect(el.getAttribute('data-sm-close-entry')).toBe('draft');
        resetDraft();
        publishCloseReasons(el);
        expect(el.getAttribute('data-sm-close-entry')).toBe('');
    });

    it('leaves the binding marker alone', () => {
        const el = document.createElement('div');
        el.setAttribute('data-sm-binding', 'verified');
        publishCloseReasons(el);
        expect(el.getAttribute('data-sm-binding')).toBe('verified');
        expect(el.getAttributeNames().sort()).toEqual([
            'data-sm-binding',
            'data-sm-close-entry',
            'data-sm-close-saving',
            'data-sm-close-unsaved',
        ]);
    });
});
