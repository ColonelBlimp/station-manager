import { describe, it, expect, afterEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import BuildBadge from './BuildBadge.svelte';
import { buildIdentity, resetBuildIdentityForTests } from './buildIdentity.svelte';

afterEach(() => resetBuildIdentityForTests());

function setReady(daemon: string, env: 'dev' | 'release'): void {
    buildIdentity.info = { daemon, go: 'go1.24.0', env };
    buildIdentity.status = 'ready';
}

describe('BuildBadge', () => {
    it('shows the authoritative daemon build, not a hard-coded version (AC1)', () => {
        setReady('v2.1.0-7-gf00dc0d', 'release');
        render(BuildBadge);
        expect(screen.getByText('v2.1.0-7-gf00dc0d')).toBeInTheDocument();
        // The confusable: a daemon that isn't alpha.1 must not still read alpha.1.
        expect(screen.queryByText('v2.0.0-alpha.1')).toBeNull();
    });

    it('marks a development daemon with a DEV pill (AC2)', () => {
        setReady('v2.1.0-dev', 'dev');
        render(BuildBadge);
        expect(screen.getByText('v2.1.0-dev')).toBeInTheDocument();
        expect(screen.getByText('DEV')).toBeInTheDocument();
    });

    it('a release daemon carries no DEV pill (AC2)', () => {
        setReady('v2.1.0', 'release');
        render(BuildBadge);
        expect(screen.queryByText('DEV')).toBeNull();
    });

    it('an unavailable identity says so, with no DEV pill and no fabricated version (AC3)', () => {
        buildIdentity.status = 'unavailable';
        buildIdentity.info = null;
        render(BuildBadge);
        expect(screen.getByText(/Version unavailable/)).toBeInTheDocument();
        expect(screen.queryByText('DEV')).toBeNull();
    });
});
