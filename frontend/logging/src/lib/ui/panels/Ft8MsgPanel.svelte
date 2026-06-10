<script lang="ts">
    // FT8 message ladder (the CQ→73 sequencer UI). Placeholder until ADR 0029
    // step e3 — the daemon-side resolver (e2) is shipped and the TX path (e1) is
    // wired; this tab becomes the manual sequencer (click a highlighted CQ row →
    // pre-filled next message → enqueue on the next slot) once that lands.
</script>

<div class="px-2 py-6 text-sm text-gray-500">
    <p class="font-semibold text-gray-700">Message ladder</p>
    <p class="mt-1">
        The FT8 contact sequencer (CQ → call → report → R-report → RR73 → 73) lands here
        with the manual-sequencing step. Not yet wired.
    </p>
</div>
