<script lang="ts">
    import Ft8PileupDrawer from '../panels/Ft8PileupDrawer.svelte';
    import { ft8PileupStack, type PileupEntry } from '../../states/ft8PileupStack.svelte';

    // Standalone playground for the FT8 pile-up drawer (the callsign stack).
    // Reached via the ?pileupdemo URL gate in app.svelte — no daemon, no live FT8
    // needed. The drawer on the right is the SAME <Ft8PileupDrawer> production
    // renders; it reads the real ft8PileupStack singleton, so seeding mock entries
    // here exercises the actual component + state. Any layout change made to the
    // drawer shows up here and on-air identically.
    //
    // The drawer hides itself when the stack is empty and shows a "paused" badge +
    // Resume control when auto-drain is paused — use the buttons to reach each state.

    const MOCK: PileupEntry[] = [
        { call: 'IK2ABC', grid: 'JN45', snr: -12, slotUtc: '2026-06-18T12:00:00Z' },
        { call: 'W1XYZ', grid: 'FN42', snr: 3, slotUtc: '2026-06-18T12:00:15Z' },
        { call: 'JA1QRP', grid: 'PM95', snr: -18, slotUtc: '2026-06-18T12:00:30Z' },
    ];

    // Extra callers for the "add" button — cycle through so each push is a new call
    // (push dedups by call, so reusing one would just refresh in place).
    const EXTRA = ['EA3XYZ', 'VK2DEF', 'PY2GHI', 'DL9JKL', 'OH2MNO', 'ZL3PQR'];
    let extraIdx = $state(0);

    function reseed() {
        ft8PileupStack.clear();
        ft8PileupStack.resume();
        for (const e of MOCK) ft8PileupStack.push({ ...e });
    }

    function addCaller() {
        const call = EXTRA[extraIdx % EXTRA.length];
        extraIdx += 1;
        ft8PileupStack.push({
            call,
            grid: 'AA00',
            snr: Math.round((extraIdx % 7) - 20),
            slotUtc: '2026-06-18T12:01:00Z',
        });
    }

    // Seed on first render so the drawer is populated when the page opens.
    reseed();
</script>

<main class="mx-auto mt-12 max-w-3xl rounded-xl border border-line-soft p-8">
    <h1 class="mb-1 text-2xl font-semibold">FT8 Pile-up Drawer — layout playground</h1>
    <p class="mb-6 text-sm text-gray-500">
        The drawer on the right is the production <code>&lt;Ft8PileupDrawer&gt;</code>, reading the
        real <code>ft8PileupStack</code>. URL: <code>?pileupdemo</code>
    </p>

    <div class="flex gap-8">
        <!-- Controls -->
        <div class="flex w-72 flex-col gap-2 text-sm">
            <button
                type="button"
                class="cursor-pointer rounded-md border border-line px-3 py-1.5 text-left hover:bg-gray-100"
                onclick={reseed}>Re-seed (3 mock callers)</button
            >
            <button
                type="button"
                class="cursor-pointer rounded-md border border-line px-3 py-1.5 text-left hover:bg-gray-100"
                onclick={addCaller}>Add a caller</button
            >
            <button
                type="button"
                class="cursor-pointer rounded-md border border-line px-3 py-1.5 text-left hover:bg-gray-100"
                onclick={() =>
                    ft8PileupStack.enabled ? ft8PileupStack.pause() : ft8PileupStack.resume()}
            >
                {ft8PileupStack.enabled ? 'Pause (Abandon)' : 'Resume'}
            </button>
            <button
                type="button"
                class="cursor-pointer rounded-md border border-line px-3 py-1.5 text-left hover:bg-gray-100"
                onclick={() => ft8PileupStack.clear()}
            >
                Clear (hides drawer)
            </button>

            <dl class="mt-4 grid grid-cols-2 gap-x-2 gap-y-1 text-xs text-gray-500">
                <dt>Queue depth</dt>
                <dd class="font-mono text-gray-800">{ft8PileupStack.count}</dd>
                <dt>Auto-drain</dt>
                <dd class="font-mono text-gray-800">
                    {ft8PileupStack.enabled ? 'enabled' : 'paused'}
                </dd>
            </dl>
        </div>

        <!-- Live preview. The drawer sets its own width (w-33, matching the
             Phone/CW Call Stack); this column just gives it room. -->
        <div class="w-44">
            <h2 class="mb-1 text-xs font-semibold text-gray-500">Live preview</h2>
            <Ft8PileupDrawer />
            {#if ft8PileupStack.count === 0}
                <p class="mt-2 text-xs italic text-gray-400">
                    (drawer hidden — stack empty; re-seed or add a caller)
                </p>
            {/if}
        </div>
    </div>
</main>
