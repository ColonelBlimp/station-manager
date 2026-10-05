import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import CommentField from './CommentField.svelte';

/*
    Comment field + recent-comments picker (restored from the retired logging SPA,
    W-0003). The operator-facing contract: the trigger is dead until there is
    history (never an empty popover); it opens a pick list; picking fills the field
    and closes; Escape closes without disturbing the field.
*/

describe('CommentField picker', () => {
    it('disables the trigger when there is no history (no empty popover)', () => {
        render(CommentField, { id: 'c', label: 'Comment', value: '', items: [] });
        const trigger = screen.getByTitle<HTMLButtonElement>('Insert a recent comment');
        expect(trigger.disabled).toBe(true);
    });

    it('opens a pick list and picking one fills the field and closes', async () => {
        render(CommentField, { id: 'c', label: 'Comment', value: '', items: ['Tnx 73', 'QRN'] });
        await fireEvent.click(screen.getByTitle('Insert a recent comment'));
        expect(screen.getByRole('menuitem', { name: 'QRN' })).toBeTruthy();

        await fireEvent.click(screen.getByRole('menuitem', { name: 'Tnx 73' }));
        expect((document.getElementById('c') as HTMLInputElement).value).toBe('Tnx 73');
        expect(screen.queryByRole('menuitem', { name: 'QRN' })).toBeNull(); // closed
    });

    it('Escape closes the popover', async () => {
        render(CommentField, { id: 'c', label: 'Comment', value: '', items: ['QRN'] });
        await fireEvent.click(screen.getByTitle('Insert a recent comment'));
        await fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' });
        expect(screen.queryByRole('menuitem', { name: 'QRN' })).toBeNull();
    });
});
