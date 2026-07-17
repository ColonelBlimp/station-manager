<script lang="ts">
    // First-run setup — the only surface a fresh install shows (App.svelte
    // gates the whole shell, map tab included, until setup completes).
    // Ported in shape from the shipping logging SPA's setup/setup_done
    // snippets, restyled onto the app shell's theme tokens.
    import { setup, saveSetup, dismissSetupDone } from '../setup.svelte';
    import { navigate } from '../router.svelte';
    import { toasts } from './toasts.svelte';
    import { isValidCallsign } from '../validators/callsign';
    import Logo from './Logo.svelte';

    // Local form state: what the operator is TYPING, distinct from what the
    // daemon has persisted (which lives behind the setup/station seams).
    let callsign: string = $state('');
    let saving: boolean = $state(false);

    // Focus the callsign input the moment it enters the DOM. A node action
    // (not onMount) because the welcome branch renders behind reactive gates,
    // so the input may not exist at mount time.
    function autofocus(node: HTMLElement) {
        node.focus();
    }

    const save = async (): Promise<void> => {
        // Normalise the same way the daemon does (TrimSpace + ToUpper) so a
        // validation failure shows the operator exactly what was sent.
        const normalised = callsign.trim().toUpperCase();
        if (normalised === '' || saving) return;
        if (isValidCallsign(normalised) !== null) {
            toasts.error('Invalid callsign format');
            return;
        }
        saving = true;
        const out = await saveSetup(normalised);
        saving = false;
        if (!out.ok) toasts.error(out.message);
    };
</script>

<div class="flex min-h-screen items-center justify-center bg-canvas p-6">
    <main class="w-full max-w-2xl rounded-xl border border-line bg-surface p-8 shadow-sm">
        {#if setup.justCompleted}
            <h1 class="text-center text-2xl font-semibold text-ink">
                <span class="text-green-500">✓</span> Setup complete
            </h1>
            <div class="space-y-3 py-4 text-center text-ink">
                <p>Your default logbook is ready and you can start logging right away.</p>
                <p>
                    Want to finish setting up your station first? <i>Settings</i> is where you configure
                    your rig (CAT), QSO forwarding (QRZ, ClubLog…), session email, FT8, and the rest of
                    your station details.
                </p>
            </div>
            <div class="flex flex-col items-center space-y-3 pt-2">
                <button
                    type="button"
                    class="btn btn-primary"
                    use:autofocus
                    onclick={() => {
                        dismissSetupDone();
                        navigate('config');
                    }}>Open Settings →</button
                >
                <button
                    type="button"
                    class="cursor-pointer text-sm text-muted hover:text-ink"
                    onclick={dismissSetupDone}>Start logging →</button
                >
            </div>
        {:else}
            <div class="flex items-center justify-center gap-2 pb-2">
                <Logo class="size-8 shrink-0 text-focus" />
                <h1 class="text-2xl font-semibold text-ink">Welcome to Station Manager</h1>
            </div>
            <div class="space-y-3 py-4 text-ink">
                <p>
                    Before you can use Station Manager, the <i>default logbook</i> needs to be initialised.
                    All this requires is a callsign — generally your personal callsign.
                </p>
                <p>
                    If you use QRZ.com and plan to forward QSOs to it, enter the callsign of your
                    target QRZ.com logbook. Not sure? Just use your personal callsign — it can
                    easily be changed later.
                </p>
            </div>
            <!-- A <form> so Enter in the input submits via the same path as
                 clicking Save; preventDefault stops the browser's synthetic GET. -->
            <form
                class="flex flex-row items-center justify-center gap-x-4"
                onsubmit={(e) => {
                    e.preventDefault();
                    void save();
                }}
            >
                <label class="text-sm font-medium text-ink" for="setup-callsign">Callsign</label>
                <input
                    id="setup-callsign"
                    type="text"
                    placeholder="Callsign"
                    class="input w-40 uppercase"
                    title="The default logbook's callsign."
                    bind:value={callsign}
                    disabled={saving}
                    autocomplete="off"
                    spellcheck="false"
                    use:autofocus
                />
                <button
                    type="submit"
                    class="btn btn-primary"
                    disabled={callsign.trim() === '' || saving}
                    >{saving ? 'Saving…' : 'Save'}</button
                >
            </form>
        {/if}
    </main>
</div>
