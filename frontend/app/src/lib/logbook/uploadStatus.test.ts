import { describe, expect, it } from 'vitest';
import { forwarderLabel, uploadTooltip, type ForwarderInfo } from './uploadStatus';
import type { LogbookQso } from '../api/logbooks';

/*
    THE OPERATOR'S DESTINATION LABEL REACHES THE LOGBOOK.

    CRITERION (operator, 2026-08-03):

        When I've labelled a destination in config.json, the logbook calls it by
        my label — in the upload tooltip and the destination dropdown — and I can
        tell that apart from the label being ignored. A destination with no label
        still shows its name, never a blank. The daemon still receives the
        destination's NAME, not my label.

    THE LAST CLAUSE IS WHY THIS ISN'T A ONE-LINER. `name` is the durable key:
    qso_upload constrains UNIQUE (qso_id, forwarder_name, action) on it, the
    `missing_from` filter matches on it, and POST /v1/forwarder/{name}/uploads
    is addressed by it. A change that swapped the label in everywhere would make
    the dropdown read correctly and then send a destination the daemon has never
    heard of. Display and identity are different jobs — hence forwarderLabel for
    one and `name` untouched for the other, pinned by L3 in
    Logbook.svelte.test.ts.

    Same fallback chain as Settings → Forwarding, minus one step: there is no
    `display_name` here because the logbook does not fetch /v1/forwarder-types.
    A destination with no label therefore shows its config name ("smcloud")
    where Settings would show the built-in ("SM Cloud backup"). Deliberate — a
    second endpoint fetch, with its own failure mode, for a cosmetic gain on
    destinations the operator can rename themselves.
*/

const QSO = { call: 'M0ABC' } as LogbookQso;

function fwd(name: string, label = ''): ForwarderInfo {
    return { name, label, type: name, enabled: true };
}

describe('forwarderLabel', () => {
    // L1 — the operator's label wins.
    it('L1: uses the operator label when set', () => {
        expect(forwarderLabel(fwd('qrz', 'QRZ (club account)'))).toBe('QRZ (club account)');
    });

    // L2 — and falls back to the config name, never blank. A destination whose
    // row reads as empty is worse than one named after its type.
    it('L2: falls back to the config name when unlabelled', () => {
        expect(forwarderLabel(fwd('clublog'))).toBe('clublog');
    });

    // L2b — a whitespace-only label is not a label. Without this, a stray space
    // in config.json produces a blank row that looks like a rendering bug.
    it('L2b: treats a blank label as unset', () => {
        expect(forwarderLabel(fwd('clublog', '   '))).toBe('clublog');
    });
});

describe('uploadTooltip labelling', () => {
    // L4 — the tooltip names destinations the operator's way. Fixture has one
    // labelled and one not, in the SAME tooltip, so "uses labels" and "ignores
    // labels" cannot both pass.
    it('L4: names destinations by label, falling back to name', () => {
        const enabled = [fwd('qrz', 'QRZ (club account)'), fwd('clublog')];
        const stamped = { ...QSO, qrzcom_qso_upload_status: 'Y' } as LogbookQso;

        const got = uploadTooltip(stamped, enabled);

        expect(got).toContain('QRZ (club account)');
        expect(got).toContain('clublog');
        // The raw name of the LABELLED one must not also appear — showing both
        // reads as two destinations.
        expect(got).not.toMatch(/On: qrz\b/);
    });
});
