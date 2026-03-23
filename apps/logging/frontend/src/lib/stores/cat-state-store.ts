import { writable, type Writable } from 'svelte/store';
import { FetchCatStateValues } from '$lib/wailsjs/go/facade/Service';
import { handleAsyncError } from '../utils/error-handler';
import { tags } from '$lib/wailsjs/go/models';

export class StateValuesClass {
    private readonly data: Record<string, Record<string, string>>;

    constructor(data: Record<string, Record<string, string>>) {
        this.data = data;
    }

    public getMainModes(): { key: string; value: string }[] {
        const modes = this.data[tags.CatStateTag.MAINMODE] ?? {};
        return Object.entries(modes).map(([key, value]) => ({ key, value }));
    }
}

export const catStateValues: Writable<StateValuesClass> = writable(new StateValuesClass({}));

export const loadCatStateValues = async (): Promise<void> => {
    try {
        const data: Record<string, Record<string, string>> = await FetchCatStateValues();
        catStateValues.set(new StateValuesClass(data));
    } catch (e: unknown) {
        handleAsyncError(e, 'loadCatStateValues()');
    }
};
