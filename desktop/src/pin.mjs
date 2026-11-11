// The TLS pin (ADR 0103 point 3; W-0027 C1).
//
// The shell trusts exactly one certificate: smd's, identified by the SHA-256 of its DER
// encoding. Every server-certificate verification in the session is decided here: accept
// (0) only for the pinned host with the pinned fingerprint, reject (-2) everything else,
// including certificates the system would trust. It never delegates to the default
// verifier (-3), and nothing else may accept a certificate error. The pin is fixed for the
// app's lifetime and is never learned from a handshake.

/** A pin set exactly once (W-0027 C1d). Before it is set, `get()` returns null, so a verifier
 *  installed with it rejects everything; a second `set` throws, so nothing can re-pin. */
export function oncePin() {
    let value = null;
    return {
        get: () => value,
        set(fp) {
            if (value !== null) throw new Error('the pin is already fixed for this app lifetime');
            if (typeof fp !== 'string' || fp === '') throw new Error('an empty pin');
            value = fp;
        },
    };
}
import { X509Certificate } from 'node:crypto';

export const PIN_HOST = '127.0.0.1';

/** SHA-256 over the certificate's DER encoding, as colon-separated upper-case hex. */
export function fingerprintOf(pem) {
    return new X509Certificate(pem).fingerprint256;
}

/** Install the pin on a session before its first request. `fingerprint` is the pinned
 *  value, or a function read at each verification (W-0027 C1d: the pin is fixed once, at
 *  the first valid identity; until then it returns null and everything is rejected).
 *  `onDecision` sees each verification (for tests); the decision never depends on it. */
export function installPin(ses, { fingerprint, onDecision }) {
    const pinned = typeof fingerprint === 'function' ? fingerprint : () => fingerprint;
    ses.setCertificateVerifyProc((request, callback) => {
        let presented;
        try {
            presented = fingerprintOf(request.certificate.data);
        } catch {
            presented = null; // an unparseable certificate is never the pinned one
        }
        const pin = pinned();
        const ok =
            request.hostname === PIN_HOST &&
            pin !== null &&
            presented !== null &&
            presented === pin;
        onDecision?.({ hostname: request.hostname, presented, ok });
        callback(ok ? 0 : -2);
    });
}
