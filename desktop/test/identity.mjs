// Synthetic TLS identities for the C1 proof (W-0027, ADR 0103). Generated per run with
// openssl into a temporary directory, never committed, never the station's identity.
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

/** Create a self-signed P-256 certificate for 127.0.0.1. Returns { key, cert } (PEM). */
export function makeIdentity(name) {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), `sm-c1-${name}-`));
    const keyPath = path.join(dir, 'key.pem');
    const certPath = path.join(dir, 'cert.pem');
    execFileSync(
        'openssl',
        [
            'req',
            '-x509',
            '-newkey',
            'ec',
            '-pkeyopt',
            'ec_paramgen_curve:P-256',
            '-nodes',
            '-keyout',
            keyPath,
            '-out',
            certPath,
            '-days',
            '3650',
            '-subj',
            `/CN=127.0.0.1 ${name}`,
            '-addext',
            'subjectAltName=IP:127.0.0.1',
        ],
        { stdio: 'ignore' }
    );
    const id = { key: fs.readFileSync(keyPath, 'utf8'), cert: fs.readFileSync(certPath, 'utf8') };
    fs.rmSync(dir, { recursive: true, force: true });
    return id;
}
