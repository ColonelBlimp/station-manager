// W-0027 D1: an installed (packaged) app ignores every development selector before anything else
// is decided (the profile, any switch, the instance lock). Pure logic, no Electron.
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { startupMode } from '../src/mode.mjs';

const hostile = (scenario) => ({
    SM_DESKTOP_SCENARIO: scenario,
    SM_DESKTOP_USERDATA: '/tmp/hostile-profile',
});

for (const scenario of ['a1', 'c1', 'c1d', 'c1d-shell', 'second', 'anything']) {
    test(`packaged: SM_DESKTOP_SCENARIO=${scenario} and SM_DESKTOP_USERDATA are ignored`, () => {
        const m = startupMode({ isPackaged: true, env: hostile(scenario) });
        assert.deepEqual(m, { scenario: '', automated: false, userData: null, fakeMedia: false });
    });
}

test('development: the selector, the profile and fake media apply as before', () => {
    assert.deepEqual(startupMode({ isPackaged: false, env: hostile('a1') }), {
        scenario: 'a1',
        automated: true,
        userData: '/tmp/hostile-profile',
        fakeMedia: true,
    });
});

test('development, interactive: no selector, the default profile, no fake media', () => {
    assert.deepEqual(startupMode({ isPackaged: false, env: {} }), {
        scenario: '',
        automated: false,
        userData: null,
        fakeMedia: false,
    });
});

test('development: a shared profile without a selector is still honoured', () => {
    const m = startupMode({ isPackaged: false, env: { SM_DESKTOP_USERDATA: '/tmp/p' } });
    assert.equal(m.userData, '/tmp/p');
    assert.equal(m.automated, false);
});

test('an unknown packaged state is treated as packaged (only a strict false is development)', () => {
    for (const isPackaged of [undefined, null, 1, 'false']) {
        assert.equal(
            startupMode({ isPackaged, env: hostile('a1') }).scenario,
            '',
            String(isPackaged)
        );
    }
});
