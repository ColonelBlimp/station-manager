// W-0023 weekend slice (operator, 2026-10-31): the contest exchange can be
// corrected after logging, in a Contest section of the edit panel — shown only
// for a contest contact, with the Phone form's checks, so a bad exchange can
// never be saved.

import { describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import EditQsoModal from './EditQsoModal.svelte';
import type { LogbookQso } from '../api/logbooks';

vi.mock('../api/enrichment', () => ({ enrichCallsign: vi.fn() }));

function props(overrides: Partial<LogbookQso>, onSave = vi.fn()) {
    const row = {
        uuid: 'u1',
        qso_date: '20261031',
        time_on: '1201',
        call: 'G4ABC',
        ...overrides,
    } as LogbookQso;
    return { row, saving: false, error: null, onSave, onClose: vi.fn() };
}
const contestRow = {
    contest_id: 'UKEIDXSSB',
    app_sm_contest_start: '20261031',
    stx: '4',
    srx: '',
    srx_string: '',
};
const saveBtn = () => screen.getByRole('button', { name: /^(Save|Saving)/i });

describe('EditQsoModal: Contest section', () => {
    it('is absent for an ordinary contact', () => {
        render(EditQsoModal, { props: props({}) });
        expect(screen.queryByRole('group', { name: 'Contest' })).toBeNull();
        expect(screen.queryByLabelText('Rcvd #')).toBeNull();
    });

    it('shows the contest and its exchange, and saves corrections', async () => {
        const onSave = vi.fn();
        render(EditQsoModal, { props: props(contestRow, onSave) });
        const section = screen.getByRole('group', { name: 'Contest' });
        expect(section).toHaveTextContent('UKEIDXSSB · 2026-10-31');
        expect(screen.getByLabelText<HTMLInputElement>('Sent #').value).toBe('4');
        await fireEvent.input(screen.getByLabelText('Rcvd #'), { target: { value: '0' } });
        await fireEvent.input(screen.getByLabelText('District'), { target: { value: 'ox' } });
        expect(screen.getByTestId('edit-district-name')).toHaveTextContent('Oxford');
        await fireEvent.click(saveBtn());
        expect(onSave).toHaveBeenCalledTimes(1);
        expect(onSave.mock.calls[0][0]).toMatchObject({ stx: '4', srx: '0', srx_string: 'OX' });
    });

    it('will not save a bogus exchange', async () => {
        const onSave = vi.fn();
        render(EditQsoModal, { props: props(contestRow, onSave) });
        for (const [field, value] of [
            ['District', 'QQ'],
            ['Rcvd #', '3a'],
            ['Sent #', ''],
        ] as const) {
            await fireEvent.input(screen.getByLabelText(field), { target: { value } });
            expect(saveBtn(), `${field}=${value}`).toBeDisabled();
            await fireEvent.click(saveBtn());
            expect(onSave).not.toHaveBeenCalled();
            // put it back
            const ok = { District: '--', 'Rcvd #': '0', 'Sent #': '4' }[field];
            await fireEvent.input(screen.getByLabelText(field), { target: { value: ok } });
        }
        expect(saveBtn()).toBeEnabled();
    });

    it('an ordinary contact sends no contest fields', async () => {
        const onSave = vi.fn();
        render(EditQsoModal, { props: props({}, onSave) });
        await fireEvent.click(saveBtn());
        const patch: unknown = onSave.mock.calls[0][0];
        expect(patch).not.toHaveProperty('stx');
        expect(patch).not.toHaveProperty('srx');
        expect(patch).not.toHaveProperty('srx_string');
    });
});
