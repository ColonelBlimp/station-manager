/*
    Discovered-hardware reader for the app Settings → Rigs connection editor
    (ADR 0028 "enumerate, don't recommend" — the operator picks a device, never
    types a path). GET /v1/hardware returns the serial ports + audio devices the
    Port / Audio pickers offer. Audio enumeration is CGO-gated daemon-side: a
    static build reports audio.available=false with empty lists, and the picker
    degrades to showing the stored device name as read-only text. Mirrors the
    daemon DTOs (internal/api/handler_hardware.go) and the config SPA's hardware.ts.
*/
import { isPlainObject, isShape, readJsonBody, safeFetch } from './_helpers';

/** A discovered serial port. `id` is the device path stored in RigConfig.port. */
export interface SerialPort {
    id: string;
    label: string;
}

/** A discovered audio device. `name` is what RigConfig.audio.{rx,tx} stores. */
export interface AudioDevice {
    name: string;
}

export interface Hardware {
    serialPorts: SerialPort[];
    audioAvailable: boolean;
    capture: AudioDevice[];
    playback: AudioDevice[];
}

export type HardwareOutcome =
    { kind: 'ok'; hardware: Hardware } | { kind: 'error'; message: string };

function ports(v: unknown): SerialPort[] {
    if (!Array.isArray(v)) return [];
    return v
        .filter(isPlainObject)
        .filter((p): p is { id: string; label?: unknown } => typeof p.id === 'string')
        .map((p) => ({ id: p.id, label: typeof p.label === 'string' ? p.label : p.id }));
}

function devices(v: unknown): AudioDevice[] {
    if (!Array.isArray(v)) return [];
    return v
        .filter(isPlainObject)
        .filter((d): d is { name: string } => typeof d.name === 'string')
        .map((d) => ({ name: d.name }));
}

export async function fetchHardware(signal?: AbortSignal): Promise<HardwareOutcome> {
    const fetched = await safeFetch('/v1/hardware', { signal });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    if (!fetched.response.ok) return { kind: 'error', message: `HTTP ${fetched.response.status}` };
    const body = await readJsonBody(fetched.response);
    if (!isShape<{ serial_ports: unknown; audio: unknown }>(body, ['serial_ports', 'audio'])) {
        return { kind: 'error', message: 'malformed /v1/hardware response' };
    }
    const audio = isPlainObject(body.audio) ? body.audio : {};
    return {
        kind: 'ok',
        hardware: {
            serialPorts: ports(body.serial_ports),
            audioAvailable: audio.available === true,
            capture: devices(audio.capture),
            playback: devices(audio.playback),
        },
    };
}
