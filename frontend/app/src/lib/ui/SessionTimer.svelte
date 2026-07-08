<!--
    SessionTimer — operating-session length as HH:MM:SS, ticking once per
    second. Ported verbatim from the logging SPA (session-28 persistence model).

    Session start is stored in sessionStorage under `sm.session.startedAt`
    (epoch ms), whose lifecycle maps cleanly to "operator session":
      - Page refresh (deliberate or accidental F5) → start survives, session
        continues (an accidental reload mid-contest must not reset the timer).
      - Tab / browser close → sessionStorage clears; next open is fresh.
      - New tab → fresh session per tab (sessionStorage is per-tab).

    Distinct from any localStorage the app uses: localStorage is cross-session
    operator preference; sessionStorage spans reloads but not the intent to
    start fresh. Renders bare text so the parent (the header) owns the visual
    treatment. Hours grow past 99 for long sessions (Field Day / contest).
-->
<script lang="ts">
    import { onDestroy } from 'svelte';
    import { formatDurationHms } from '../utils/time';

    const SESSION_KEY = 'sm.session.startedAt';

    function loadOrInitStart(): number {
        try {
            const raw = sessionStorage.getItem(SESSION_KEY);
            if (raw !== null) {
                const n = Number(raw);
                if (Number.isFinite(n)) return n;
            }
        } catch {
            // sessionStorage unavailable (private-mode edge cases) — fall
            // through to an in-memory start (refresh-survival is what we lose).
        }
        const fresh = Date.now();
        try {
            sessionStorage.setItem(SESSION_KEY, String(fresh));
        } catch {
            // Write failed — in-memory state is still correct for this page life.
        }
        return fresh;
    }

    const startedAt = loadOrInitStart();
    let now = $state(startedAt);

    const tickerId = setInterval(() => {
        now = Date.now();
    }, 1000);

    onDestroy(() => clearInterval(tickerId));

    const elapsed = $derived(formatDurationHms(now - startedAt));
</script>

<span class="tabular-nums">{elapsed}</span>
