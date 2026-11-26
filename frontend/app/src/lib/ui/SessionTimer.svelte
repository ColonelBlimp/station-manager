<!--
    SessionTimer — how long the current sitting of the operating session has
    run, as HH:MM:SS, ticking once a second (W-0029 part 6, criteria D).

    The start is the daemon's: `sitting_started_at` of the open session, as the
    page last read it (session.svelte.ts). So a reload and a daemon restart do
    not change what it shows, a second window would show the same, and New and
    Continue start it again at zero. Nothing is kept in the browser's storage.

    The time since that start is counted on the DAEMON's clock too (daemonNowMs,
    calibrated from the `Date` of the daemon's answers): a start stamped by one
    clock and a "now" read from another would show the difference between the
    two clocks as session time. Before any answer has calibrated it, that is the
    window's own clock, the best there is.

    With no session open it shows no time (operator ruling 2026-11-25): not
    zeros, and never a count from when the window loaded. The header keeps its
    place for it. Renders bare text so the header owns the visual treatment.
    Hours grow past 99 for long sittings (Field Day / contest).
-->
<script lang="ts">
    import { onDestroy } from 'svelte';
    import { formatDurationHms } from '../utils/time';
    import { session } from '../operate/session.svelte';
    import { daemonNowMs } from '../api/daemonClock.svelte';

    let now = $state(daemonNowMs());

    const tickerId = setInterval(() => {
        now = daemonNowMs();
    }, 1000);

    onDestroy(() => clearInterval(tickerId));

    const elapsed = $derived.by(() => {
        if (session.sittingStartedAt === null) return '';
        const started = Date.parse(session.sittingStartedAt);
        if (Number.isNaN(started)) return '';
        // A start a moment ahead of the clock's last reading never shows a negative time.
        return formatDurationHms(Math.max(0, now - started));
    });
</script>

<span class="tabular-nums text-base" data-testid="session-timer">{elapsed}</span>
