import { describe, expect, it, vi } from 'vitest';
import { reloadPage } from './reload';

// W-0024 acceptance F6. In Firefox (159, reproduced headless over WebDriver
// BiDi, 2026-10-30), once a location.reload() has been refused at the
// beforeunload prompt ("Stay on Page"), a later location.reload() in the same
// document never completes — not even after "Leave Page", nor with the guard
// removed — while a navigation to the same address does. The reload seam
// therefore navigates (location.replace, no history entry) to the current
// address without its fragment: a replace that only changed the fragment
// would scroll, not reload.
function fakeLocation(href: string) {
    return { href, replace: vi.fn(), reload: vi.fn() };
}

describe('reloadPage', () => {
    it.each([
        ['http://127.0.0.1:8080/config', 'http://127.0.0.1:8080/config'],
        [
            'http://127.0.0.1:8080/logbook?missing_from=qrz',
            'http://127.0.0.1:8080/logbook?missing_from=qrz',
        ],
        ['http://127.0.0.1:8080/operate/phone#top', 'http://127.0.0.1:8080/operate/phone'],
    ])('navigates to %s by location.replace, never location.reload', (href, target) => {
        const loc = fakeLocation(href);
        reloadPage(loc);
        expect(loc.replace).toHaveBeenCalledTimes(1);
        expect(loc.replace).toHaveBeenCalledWith(target);
        expect(loc.reload).not.toHaveBeenCalled();
    });
});
