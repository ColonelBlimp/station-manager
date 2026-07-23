// Serial-overrides editor — the two things that have now gone wrong twice.
//
// 1. Fields the editor does NOT render (rts/dtr: tri-state PTT-line controls set
//    by hand in config.json) must SURVIVE an edit to a field it does render.
//    Rebuilding `overrides` from only the visible fields dropped them, and the
//    daemon then fell back to the rigdef default on restart — on a rig that
//    genuinely needs a line asserted, that stops it working (review 60a8e7ae).
// 2. Preserving them must not reorder the object. Dirty-tracking is
//    JSON.stringify equality, so re-emitting identical settings in a different
//    key order reads as an edit: the override cannot be cleanly reverted and the
//    spurious diff reaches the save merge (review 0e8cec2e).

import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import SerialOverridesEditor from './SerialOverridesEditor.svelte';
import type { RigConfig } from '../api/rigs';

function rigWith(overrides: RigConfig['overrides']): RigConfig {
    return { id: 1, model: 'yaesu-ftdx10', overrides } as RigConfig;
}

/** Mount and return a typist for the FIRST field (baud rate). The inputs carry
 *  no accessible name — the labels are plain spans — so they are addressed
 *  positionally rather than by label text. */
function mount(rig: RigConfig): (value: string) => void {
    const { container } = render(SerialOverridesEditor, { rig, rigdef: undefined });
    const baud = container.querySelectorAll('input')[0];
    return (value: string) => {
        baud.value = value;
        baud.dispatchEvent(new Event('input', { bubbles: true }));
        flushSync();
    };
}

describe('SerialOverridesEditor', () => {
    it('keeps unmanaged fields (rts/dtr) when a visible field is edited', () => {
        const rig = rigWith({ baud_rate: 4800, rts: false });
        const typeBaud = mount(rig);

        typeBaud('9600');

        expect(rig.overrides?.baud_rate).toBe(9600);
        expect(rig.overrides?.rts).toBe(false); // must not be dropped
    });

    it('reverting an edit restores the ORIGINAL object byte-for-byte', () => {
        const rig = rigWith({ baud_rate: 4800, rts: false });
        const typeBaud = mount(rig);
        const before = JSON.stringify(rig.overrides);

        typeBaud('9600');
        typeBaud('4800'); // back to where we started

        // Key ORDER matters: the panel compares with JSON.stringify, so a
        // reordered-but-equivalent object still reads as dirty.
        expect(JSON.stringify(rig.overrides)).toBe(before);
    });

    it('clearing a visible field removes it without disturbing the rest', () => {
        const rig = rigWith({ baud_rate: 4800, rts: true });
        const typeBaud = mount(rig);

        typeBaud('');

        expect(rig.overrides?.baud_rate).toBeUndefined();
        expect(rig.overrides?.rts).toBe(true);
    });
});
