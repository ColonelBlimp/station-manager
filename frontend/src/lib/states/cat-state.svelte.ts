import { tags } from '$lib/wailsjs/go/models';

export interface IsCatEnabled {
    isEnabled: boolean;
}

export const isCatEnabled: IsCatEnabled = $state({ isEnabled: false });

export interface CatState {
    identity: string;
    vfoaFreq: string;
    vfobFreq: string;
    select: string;
    split: string;
    mainMode: string;
    subMode: string;
    txPower: string;
    update(this: CatState, data: Record<string, string>): void;
}

const tagToField: Record<tags.CatStateTag, keyof Omit<CatState, 'update'>> = {
    [tags.CatStateTag.IDENTITY]: 'identity',
    [tags.CatStateTag.VFOAFREQ]: 'vfoaFreq',
    [tags.CatStateTag.VFOBFREQ]: 'vfobFreq',
    [tags.CatStateTag.SELECT]: 'select',
    [tags.CatStateTag.SPLIT]: 'split',
    [tags.CatStateTag.MAINMODE]: 'mainMode',
    [tags.CatStateTag.SUBMODE]: 'subMode',
    [tags.CatStateTag.TXPWR]: 'txPower',
};

export const catState: CatState = $state({
    identity: '',
    vfoaFreq: '',
    vfobFreq: '',
    select: '',
    split: 'OFF', // Default value
    mainMode: '',
    subMode: '',
    txPower: '',
    update(this: CatState, data: Record<string, string>): void {
        if (!data) return;
        for (const key of Object.keys(data)) {
            const tag = key as unknown as tags.CatStateTag;
            const field = tagToField[tag];
            if (!field) continue; // ignore unknown tags
            const value = (data as Record<string, string | undefined>)[key];
            if (value !== undefined) {
                this[field] = String(value);
            }
        }
        isCatEnabled.isEnabled = true;
    },
});
