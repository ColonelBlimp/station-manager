// The read-only binding marker at the PRODUCTION BOUNDARY (ADR 0099 W1-2): the
// real main.ts and App publish it on <html>; after boot it names the bound
// archive, the established default and the bracket's instance; after the
// daemon restarts and the shell stream reconnects, a successful verification
// replaces the instance — without any request made on the marker's behalf.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { installDaemon, shellStream, type Daemon } from './test/mainHarness';
import { nextSocket } from './test/fakeSocket';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';

const ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58d11';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';
const versionAs = (instance: string) => ({
    daemon: 'test',
    instance,
    archive: { id: ARCHIVE, label: 'Home', ownership: 'legacy', default_logbook_uuid: A },
});
const daemon: Daemon = {
    version: versionAs('i1'),
    config: {
        setup_complete: true,
        logging_station: { station_callsign: '7Q5MLV' },
        default_logbook: { id: 1, uuid: A, name: 'Home', callsign: '7Q5MLV' },
        bridge: { enabled: false },
    },
    routes: {},
    sent: [],
};
const marker = () => {
    const el = document.documentElement;
    return {
        archive: el.getAttribute('data-sm-archive'),
        default: el.getAttribute('data-sm-default'),
        binding: el.getAttribute('data-sm-binding'),
        instance: el.getAttribute('data-sm-verified-instance'),
    };
};

describe('main.ts: the binding marker on <html>', () => {
    beforeAll(async () => {
        installDaemon(daemon);
        _setReloadForTests(() => {});
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
        await vi.waitFor(() => expect(archivesState.verifying).toBe(false));
    });
    afterAll(() => {
        vi.unstubAllGlobals();
    });

    it('names the booted binding, then the new instance after a reconnect check succeeds', async () => {
        await vi.waitFor(() =>
            expect(marker()).toEqual({
                archive: ARCHIVE,
                default: A,
                binding: 'verified',
                instance: 'i1',
            })
        );
        daemon.version = versionAs('i2');
        const src = shellStream();
        src.emit('open');
        src.emit('error');
        (await nextSocket(src, (fn) => vi.waitFor(fn))).emit('open');
        await vi.waitFor(() =>
            expect(marker()).toEqual({
                archive: ARCHIVE,
                default: A,
                binding: 'verified',
                instance: 'i2',
            })
        );
    });
});
