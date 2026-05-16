/**
 * English baseline catalogue for the SPA's operator-facing strings.
 *
 * This is the master catalogue: every key the SPA renders MUST exist
 * here. Other locales (Chichewa `ny.ts`, Tumbuka `tum.ts` — planned)
 * may omit keys; the i18n render helper falls back to this catalogue
 * for any missing key so the SPA always produces a string.
 *
 * Keys are dot-namespaced by feature area; values are templates with
 * `{name}` placeholders that get substituted from a `details` map at
 * render time (e.g. template `"Could not open {port}: {error}"` +
 * details `{port: "/dev/ttyUSB0", error: "permission denied"}` →
 * `"Could not open /dev/ttyUSB0: permission denied"`).
 *
 * Editing wording: change the right-hand string. No daemon restart
 * needed; the SPA build/HMR picks it up. Daemon log lines stay
 * technical English independently — different audience.
 */

export const en: Record<string, string> = {
    // ─── Bridge: rig-disconnected events ──────────────────────────
    // Daemon sends EventRigDisconnected with one of these codes;
    // SPA renders via `bridge.disconnected.<code>`.

    'bridge.disconnected.rig_no_data': 'The rig has gone quiet — is it powered on?',
    'bridge.disconnected.serial_port_error': 'Lost the serial connection to the rig ({error})',

    // ─── Bridge: implicit reconnect (SPA-derived, not a daemon event)
    // Fired by bridge.svelte.ts on the first rig-state after a
    // rig-disconnected — gives the operator symmetric closure to the
    // warn-level disconnect toast. Not keyed by a daemon code because
    // the daemon doesn't emit a reconnect event; recovery is implicit
    // per ADR 0009.

    'bridge.reconnected': 'Rig reconnected.',

    // ─── Bridge: bridge-error events ──────────────────────────────
    // Daemon sends EventBridgeError with one of these codes; SPA
    // renders via `bridge.error.<code>`. All are operator-actionable
    // (something to fix in config.json or in the physical setup) —
    // hence the more specific phrasing.

    'bridge.error.unknown_driver':
        'CAT driver "{driver}" is not recognised — check bridge.cat.driver in config.json',
    'bridge.error.serial_config_invalid': 'Serial port configuration is invalid: {error}',
    'bridge.error.missing_init_command':
        'Driver "{driver}" has no startup command — cannot enable rig push-state',
    'bridge.error.missing_read_command':
        'Driver "{driver}" has no read command — cannot fetch initial rig state',
    'bridge.error.serial_open_failed': 'Could not open serial port "{port}": {error}',
    'bridge.error.init_write_failed': 'Could not enable push-state on driver "{driver}": {error}',
    'bridge.error.identity_unrecognised':
        'The connected rig\'s ID is not recognised by driver "{driver}" — check bridge.cat.driver matches your rig',
    'bridge.error.identity_mismatch':
        'Configured driver is "{driver}" ({expected}), but the rig identifies as "{actual}"',

    // ─── Form-input validators ────────────────────────────────────
    // Returned by SPA-side validators in `lib/validators/*` when an
    // input fails the local format check. Operator sees this inline
    // under the input (rendered by `ValidatedInput` and `Callsign`)
    // with `aria-describedby` wiring so screen readers announce it
    // too. Keep wording short — the field's own red border carries
    // the "something's wrong" cue, the message answers "what shape
    // does this expect."

    'validators.callsign':
        'Callsign must be 3–32 characters with letters and digits (slash allowed)',
    'validators.maidenhead': 'Grid square must be 4, 6, or 8 characters (e.g. IO91vl)',
    'validators.cq_zone': 'CQ Zone must be between 1 and 40',
    'validators.itu_zone': 'ITU Zone must be between 1 and 90',
    'validators.dxcc': 'DXCC entity must be between 0 and 522',
    'validators.rst': 'RST must be 2 or 3 digits',
    'validators.frequency': 'Frequency must be 100 kHz – 30 GHz (e.g. 14.250.000 or 14.250)',

    // ─── Worked panel ─────────────────────────────────────────────
    // Shown in the WorkedPanel tab when the contact-history fetch
    // returned an empty list — i.e. "I checked and there's no prior
    // QSO with this station." Distinct from the pre-Tab state, which
    // renders nothing rather than this message.

    'worked.empty': 'No prior contacts with this station.',
};
