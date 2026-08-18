/*
    Operator-facing wording for the daemon's bridge event codes — the
    `rig-disconnected` and `bridge-error` payloads (ADR 0010 rev 6). Ported from
    the retired logging SPA's i18n catalogue (`frontend/logging/src/lib/i18n/en.ts`)
    so the app surfaces friendly, actionable text instead of raw internal codes
    (W-0003 acceptance criterion 3).

    An UNKNOWN code falls back to the raw code (+ any details) — odd beats
    invisible, and it preserves exactly the behaviour the raw-code rendering had
    for codes we have no wording for. `{token}` placeholders are filled from the
    event's `details` map (e.g. `{port}` → `/dev/ttyUSB0`); a token with no
    matching detail is left verbatim so the gap is visible rather than silently
    dropped.

    The stuck-TX alarm wording (`bridge.txalarm.*`) is deliberately NOT here: it
    lives in `ui/TxAlarmBanner.svelte`, which owns that safety banner end to end.
*/

const DISCONNECTED: Record<string, string> = {
    rig_no_data: 'The rig has gone quiet — is it powered on?',
    serial_port_error:
        'Lost the connection to the rig — check it is powered on and the cable is connected.',
};

const BRIDGE_ERROR: Record<string, string> = {
    unknown_driver:
        'CAT driver "{driver}" is not recognised — check bridge.cat.driver in config.json',
    serial_config_invalid: 'Serial port configuration is invalid: {error}',
    missing_init_command: 'Driver "{driver}" has no startup command — cannot enable rig push-state',
    missing_read_command: 'Driver "{driver}" has no read command — cannot fetch initial rig state',
    serial_open_failed:
        'Could not open the rig serial port {port} — check the cable and that the rig is on.',
    serial_permission_denied:
        'Permission denied opening the rig serial port {port}. The daemon user needs access to the device — add it to the "dialout" group: run "sudo usermod -aG dialout $USER", then log out and back in (or reboot).',
    serial_port_busy:
        'The rig serial port {port} is already in use by another program (WSJT-X, another logger, or a second Station Manager). Close it, then retry.',
    serial_port_not_found:
        'The rig serial port {port} was not found — is the rig powered on and the USB cable connected?',
    init_write_failed: 'Could not enable push-state on driver "{driver}": {error}',
    identity_unrecognised:
        'The connected rig\'s ID is not recognised by driver "{driver}" — check bridge.cat.driver matches your rig',
    identity_mismatch:
        'Configured driver is "{driver}" ({expected}), but the rig identifies as "{actual}"',
};

function fill(template: string, details?: Record<string, string>): string {
    if (!details) return template;
    return template.replace(/\{(\w+)\}/g, (_m, key: string) =>
        key in details ? details[key] : `{${key}}`
    );
}

/** Raw code (+ joined details), matching the pre-port rendering — the fallback
 *  for any code without ported wording. */
function rawFallback(code: string, details?: Record<string, string>): string {
    const suffix = details ? ` (${Object.values(details).join(', ')})` : '';
    return `${code}${suffix}`;
}

/** Friendly text for a `rig-disconnected` code; raw-code fallback when unknown. */
export function disconnectMessage(code: string, details?: Record<string, string>): string {
    const template = DISCONNECTED[code];
    return template ? fill(template, details) : rawFallback(code, details);
}

/** Friendly text for a `bridge-error` code; raw-code fallback when unknown. */
export function bridgeErrorMessage(code: string, details?: Record<string, string>): string {
    const template = BRIDGE_ERROR[code];
    return template ? fill(template, details) : rawFallback(code, details);
}
