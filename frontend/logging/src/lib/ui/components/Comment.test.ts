import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, cleanup, fireEvent } from '@testing-library/svelte';
import Comment from './Comment.svelte';

/**
 * Comment is the QSO notes textarea plus an optional paste-list
 * dropdown. The textarea/binding is the same shape as every other
 * field; the behaviour worth pinning is the dropdown:
 *
 *   - Trigger is always rendered (stable layout) but disabled while
 *     `history` is empty, so it never opens an empty popover.
 *   - Trigger is enabled + opens a menu when `history` is non-empty.
 *   - Picking an item calls `onpick` with that comment and closes.
 */
describe('Comment paste-list dropdown', () => {
    afterEach(() => cleanup());

    function setup(props: { history?: string[]; onpick?: (t: string) => void } = {}) {
        const { container } = render(Comment, {
            id: 'comment',
            label: 'Comment',
            value: '',
            ...(props.history !== undefined ? { history: props.history } : {}),
            ...(props.onpick !== undefined ? { onpick: props.onpick } : {}),
        });
        return { container };
    }

    it('renders the trigger but disables it when history is empty', () => {
        const { container } = setup({ history: [] });
        const trigger = container.querySelector('#comment-history-trigger') as HTMLButtonElement;
        expect(trigger).not.toBeNull();
        expect(trigger.disabled).toBe(true);
    });

    it('renders an enabled trigger when history is non-empty', () => {
        const { container } = setup({ history: ['Lot of QRN'] });
        const trigger = container.querySelector('#comment-history-trigger') as HTMLButtonElement;
        expect(trigger).not.toBeNull();
        expect(trigger.disabled).toBe(false);
    });

    it('opens the menu on trigger click and lists the comments', async () => {
        const { container } = setup({ history: ['Lot of QRN', 'Tnx QSO 73'] });
        // Closed initially.
        expect(container.querySelector('#comment-history-list')).toBeNull();

        const trigger = container.querySelector('#comment-history-trigger') as HTMLButtonElement;
        await fireEvent.click(trigger);

        const list = container.querySelector('#comment-history-list');
        expect(list).not.toBeNull();
        const items = list?.querySelectorAll('[role="menuitem"]') ?? [];
        expect(items).toHaveLength(2);
        expect(items[0].textContent?.trim()).toBe('Lot of QRN');
        expect(items[1].textContent?.trim()).toBe('Tnx QSO 73');
    });

    it('calls onpick with the chosen comment and closes the menu', async () => {
        const onpick = vi.fn();
        const { container } = setup({ history: ['Lot of QRN', 'Tnx QSO 73'], onpick });

        const trigger = container.querySelector('#comment-history-trigger') as HTMLButtonElement;
        await fireEvent.click(trigger);

        const items = container.querySelectorAll('#comment-history-list [role="menuitem"]');
        await fireEvent.click(items[1]);

        expect(onpick).toHaveBeenCalledTimes(1);
        expect(onpick).toHaveBeenCalledWith('Tnx QSO 73');
        // Menu closes after a pick.
        expect(container.querySelector('#comment-history-list')).toBeNull();
    });

    it('sets aria-expanded on the trigger to reflect open state', async () => {
        const { container } = setup({ history: ['Lot of QRN'] });
        const trigger = container.querySelector('#comment-history-trigger') as HTMLButtonElement;
        expect(trigger.getAttribute('aria-expanded')).toBe('false');
        await fireEvent.click(trigger);
        expect(trigger.getAttribute('aria-expanded')).toBe('true');
    });
});
