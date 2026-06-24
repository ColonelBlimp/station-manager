/*
    Daemon-side wrapper for `GET /v1/hardware` — the discovered serial ports +
    audio devices the Rigs tab's Port / Audio pickers offer (ADR 0028 "enumerate,
    don't recommend"; the operator never types a device path). Same discriminated-
    union + envelope-guard conventions as rigs.ts / config.ts.

    Always 200 (enumeration, no validation errors), so any non-2xx collapses to
    'server'. Audio enumeration is CGO-gated daemon-side: a static build reports
    `audio.available: false` with empty lists — the Rigs tab degrades to showing
    the stored device name as read-only text rather than a picker.

    Types mirror the daemon DTOs (internal/api/handler_hardware.go +
    internal/hardware).
*/

import { isPlainObject, isShape, readJsonBody, safeFetch } from './_helpers';

/** A discovered serial port (mirrors hardware.SerialPort). `id` is the device
 *  path stored in RigConfig.port; `label` is the friendly display string. */
export interface SerialPort {
    id: string;
    label: string;
    usb: boolean;
    vid?: string;
    pid?: string;
    serial_number?: string;
    product?: string;
}

/** A discovered audio device (mirrors hardware.AudioDevice). `name` is what
 *  RigConfig.audio.{rx,tx} stores. */
export interface AudioDevice {
    name: string;
    is_default: boolean;
}

export interface HardwareResponse {
    serial_ports: SerialPort[];
    audio: {
        available: boolean;
        capture: AudioDevice[];
        playback: AudioDevice[];
    };
}

export type HardwareOutcome =
    | { kind: 'ok'; hardware: HardwareResponse }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    | { kind: 'network'; message: string };

export async function fetchHardware(signal?: AbortSignal): Promise<HardwareOutcome> {
    const fetched = await safeFetch('/v1/hardware', { signal });
    if (!fetched.ok) {
        return { kind: fetched.kind, message: fetched.message };
    }
    const body = await readJsonBody(fetched.response);
    if (fetched.response.ok) {
        if (!isShape<HardwareResponse>(body, ['serial_ports', 'audio'])) {
            return {
                kind: 'server',
                code: 'malformed_response',
                message: 'daemon returned /v1/hardware without the serial_ports / audio blocks',
            };
        }
        return { kind: 'ok', hardware: body };
    }
    const err = isPlainObject(body) ? (body as { code?: string; message?: string }) : null;
    return {
        kind: 'server',
        code: err?.code ?? 'unknown_error',
        message: err?.message ?? `HTTP ${fetched.response.status}`,
    };
}
