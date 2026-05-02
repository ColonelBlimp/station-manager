<!--
    SessionTimer — operating-session length, displayed as HH:MM:SS,
    ticking once per second.

    Persistence model (settled session 28): the session start moment
    is stored in `sessionStorage` under `sm.session.startedAt` (epoch
    ms). sessionStorage's natural lifecycle maps cleanly to "operator
    session":
      - Page refresh (deliberate or accidental F5) → start time
        survives, session continues. Important because we've started
        using F-keys for log-app actions (F2 lookup-only, future F-key
        bindings) and accidental F5 mid-contest would otherwise reset
        a 3-hour session to 00:00:00 — a real papercut.
      - Tab close / browser close → sessionStorage cleared, next open
        is a fresh session. No Reset button needed in v1.
      - New tab → fresh session per tab (sessionStorage is per-tab).

    This is correctly DIFFERENT from manualState's localStorage. Both
    persist, but for different time scales:
      - localStorage: operator preferences that span devices and
        sessions ("I want USB on 14.250 by default").
      - sessionStorage: state that spans page reloads but not the
        operator's intent to start a fresh session ("how long have I
        been operating right now?").

    Format: HH:MM:SS, hours zero-padded but allowed to grow past 99
    for long sessions (Field Day overnight, contest weekends). The
    `font-mono` class can be added at the use site if the layout
    needs it; SessionTimer renders bare text so the parent owns the
    visual treatment.

    The `setInterval` runs at 1 Hz to match the seconds-precision
    display. `formatDurationHms` is reactive (`$derived`), so the
    bound DOM updates on every `now` change.
-->
<script lang="ts">
    import { onDestroy } from 'svelte';
    import { formatDurationHms } from '../../utils/time';

    const SESSION_KEY = 'sm.session.startedAt';

    function loadOrInitStart(): number {
        try {
            const raw = sessionStorage.getItem(SESSION_KEY);
            if (raw !== null) {
                const n = Number(raw);
                if (Number.isFinite(n)) return n;
            }
        } catch {
            // sessionStorage unavailable (private browsing edge cases,
            // disabled storage) — fall through to in-memory fallback.
        }
        const fresh = Date.now();
        try {
            sessionStorage.setItem(SESSION_KEY, String(fresh));
        } catch {
            // Storage write failed — in-memory state is still correct
            // for this page life; refresh-survival is what we lose.
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

<span class="">{elapsed}</span>
