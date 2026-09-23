import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';

vi.mock('../api/qso-archives', () => ({
    fetchQsoArchives: vi.fn(),
    createQsoArchive: vi.fn(),
    activateQsoArchive: vi.fn(),
    fetchDaemonIdentity: vi.fn(),
}));
vi.mock('../api/restart', () => ({
    fetchDaemonInstance: vi.fn(),
    waitForDaemonBack: vi.fn(),
}));

import ArchiveSwitchGate from './ArchiveSwitchGate.svelte';
import {
    archivesState,
    _resetArchivesForTests,
    _setReloadForTests,
} from '../config/archives.svelte';

// The unresolved-switch gate covers every view and offers exactly one way out.
beforeEach(() => _resetArchivesForTests());
afterEach(() => vi.restoreAllMocks());

describe('ArchiveSwitchGate', () => {
    it('is absent while no switch is unresolved', () => {
        render(ArchiveSwitchGate);
        expect(screen.queryByRole('alertdialog')).toBeNull();
    });

    it('blocks with the detail and reloads on its one control', async () => {
        let reloads = 0;
        _setReloadForTests(() => reloads++);
        archivesState.switchUnresolved = true;
        archivesState.switchDetail = 'The daemon did not answer as a new instance within the wait.';
        render(ArchiveSwitchGate);
        flushSync();
        const dialog = screen.getByRole('alertdialog');
        expect(dialog).toHaveTextContent('Archive binding unproven');
        expect(dialog).toHaveTextContent('did not answer as a new instance');
        await fireEvent.click(screen.getByRole('button', { name: 'Reload now' }));
        expect(reloads).toBe(1);
    });
});
