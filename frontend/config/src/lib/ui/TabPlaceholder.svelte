<script lang="ts">
    // Empty-shell placeholder for a not-yet-built config tab. Each tab gets its
    // own panel component as the workstream fills them in; until then the shell
    // renders this so the layout (nav strip + body + footer) can be reviewed in
    // isolation. The `title` echoes the active tab so it's obvious which panel
    // will live here.
    let { title }: { title: string } = $props();
</script>

<div class="flex h-full min-h-64 flex-col items-center justify-center text-center">
    <p class="text-lg font-medium text-gray-400">{title}</p>
    <p class="mt-1 text-sm text-gray-400">This panel is not built yet.</p>
</div>
