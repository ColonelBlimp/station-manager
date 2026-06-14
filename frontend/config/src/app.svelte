<script lang="ts">
    // Scaffold placeholder. The config-editing UX (station, rigs, forwarding,
    // FT8, bridge, SMTP) is a separate design pass — deliberately not built
    // here until that UX is discussed. The config-system redesign
    // (docs/v2-design/config.md §9-15) is decided and partly shipped; it routes
    // §10 2e (audio-device naming) and §11 (hot-reload) to this workstream.
    const version = '0.0.0';
</script>

<main class="min-h-screen bg-gray-50 p-8 font-sans">
    <h1 class="text-2xl font-semibold text-gray-900">Station Manager — Config</h1>
    <p class="mt-2 text-gray-600">
        Scaffold ({version}). The configuration UI lives here. Served at
        <code class="rounded bg-gray-200 px-1">/config/</code>.
    </p>
</main>
