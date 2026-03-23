import { DEFAULT_CLIPBOARD_MAX_LENGTH } from '$lib/constants/timers';

export interface ClipboardState {
    list: string[];
    maxLength: number;
    add(this: ClipboardState, item: string): void;
    setMaxLength(this: ClipboardState, len: number): void;
}

export const clipboardState: ClipboardState = $state({
    list: [],
    maxLength: DEFAULT_CLIPBOARD_MAX_LENGTH,

    add(this: ClipboardState, item: string): void {
        const value = item.trim();
        if (!value) return;

        // Use a new array to ensure reactivity triggers correctly
        let newList = this.list.filter((i) => i !== value);
        newList = [value, ...newList];

        if (newList.length > this.maxLength) {
            newList = newList.slice(0, this.maxLength);
        }
        this.list = newList;
    },

    setMaxLength(this: ClipboardState, len: number): void {
        const n = Number.isFinite(len) ? Math.floor(len) : 0;
        this.maxLength = Math.max(0, n);
        if (this.list.length > this.maxLength) {
            this.list = this.list.slice(0, this.maxLength);
        }
    },
});
