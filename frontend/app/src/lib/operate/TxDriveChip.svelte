<script lang="ts">
    /*
        TX-drive (ALC) chip (ADR 0064) — the always-visible readout for "is my
        FT8 drive level right?". Renders nothing until the first ALC poll
        answer of this page-load (no METERPOLL rigdef / CAT off / no capture
        session yet: an instrument that cannot read must not paint a value);
        then one of four states, colour + text:

          good  — ALC 0, drive right (green);
          warn  — ALC deflecting below the red threshold (amber, value shown);
          red   — at/over ft8.meter.alc_red: overdrive (red, value shown);
          stale — answers stopped: NO DATA (grey dash) — deliberately distinct
                  from good, because a dead poll and a clean zero must never
                  render the same.

        Chip-only (no expandable card): the value + colour IS the whole
        instrument. Sits beside the RX AudioLevelCard chip in Ft8View's
        bottom-left anchor; this component owns no placement. data-state
        carries the state for jsdom (same contract as the audio chip).
    */
    import { txDriveState, txDriveStatus, type TxDriveStatus } from './txDrive.svelte';

    // Staleness needs a clock: re-evaluate twice per poll-cadence-ish so the
    // stale flip lands within ~500 ms of the window expiring.
    let now = $state(Date.now());
    $effect(() => {
        const id = setInterval(() => {
            now = Date.now();
        }, 500);
        return () => clearInterval(id);
    });

    const status: () => TxDriveStatus = $derived(() => txDriveStatus(now));

    const toneByState: Record<Exclude<TxDriveStatus, 'hidden'>, string> = {
        good: 'bg-emerald-500',
        warn: 'bg-amber-500',
        red: 'bg-red-600',
        stale: 'bg-zinc-500',
    };

    const label: () => string = $derived(() =>
        status() === 'stale' ? 'ALC —' : `ALC ${txDriveState.alc?.value ?? 0}`
    );
</script>

{#if status() !== 'hidden'}
    <div
        data-txdrive-chip
        data-state={status()}
        class="flex items-center gap-x-1.5 rounded-full border border-line bg-surface px-2.5 py-1.5 shadow-md"
        title="TX drive — rig ALC (0–255) polled live. Red = overdrive; grey dash = no answers. Threshold: ft8.meter.alc_red"
    >
        <span class="font-mono text-xs text-muted">{label()}</span>
        <span
            class="size-2.5 rounded-full {toneByState[
                status() as Exclude<TxDriveStatus, 'hidden'>
            ]}"
            aria-hidden="true"
        ></span>
    </div>
{/if}
