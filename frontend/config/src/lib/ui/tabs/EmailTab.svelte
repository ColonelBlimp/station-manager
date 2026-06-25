<script lang="ts">
    // Email tab — the SMTP submission account the daemon's mailer uses (today:
    // the logging SPA's "email this session's ADIF" button; future alert/notify
    // subsystems). The password is masked-on-GET — leave it blank to keep the
    // stored secret. Provider settings are restart-only (the mailer binds at boot).
    import { configState } from '../../states/config.svelte';
    import TabFooter from '../TabFooter.svelte';

    const form = $derived(configState.emailForm);

    // Numeric inputs (port / timeout) round-trip as numbers; a non-numeric entry
    // falls back to 0 and the daemon's validateSmtp rejects it on save with a
    // clear message — the SPA doesn't duplicate the bounds rules.
    function numInput(set: (n: number) => void) {
        return (e: Event) => {
            const n = parseInt((e.currentTarget as HTMLInputElement).value.trim(), 10);
            set(Number.isFinite(n) && n >= 0 ? n : 0);
        };
    }
</script>

{#if !configState.config}
    <p class="text-sm text-gray-500">Loading…</p>
{:else}
    <div class="mx-auto max-w-xl space-y-8">
        <section>
            <h2 class="text-base font-semibold text-gray-800">Outgoing email (SMTP)</h2>
            <p class="mt-0.5 mb-3 text-sm text-gray-500">
                The submission account Station Manager sends from — used by the logging app's “email
                this session's log” button. Leave disabled if you don't send logs by email.
            </p>
            <label class="flex items-center gap-2 text-sm text-gray-700">
                <input type="checkbox" bind:checked={form.enabled} class="cursor-pointer" />
                Enabled
            </label>
        </section>

        <section class="space-y-3">
            <h3 class="text-sm font-semibold text-gray-700">Server</h3>
            <div class="flex flex-wrap gap-x-6 gap-y-3">
                <label class="flex flex-col gap-1">
                    <span class="text-sm font-medium text-gray-700">Host</span>
                    <input
                        type="text"
                        bind:value={form.host}
                        placeholder="smtp.example.org"
                        autocomplete="off"
                        spellcheck="false"
                        class="w-72 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                    />
                </label>
                <label class="flex flex-col gap-1">
                    <span class="text-sm font-medium text-gray-700">Port</span>
                    <input
                        type="text"
                        inputmode="numeric"
                        value={form.port ? String(form.port) : ''}
                        oninput={numInput((n) => (form.port = n))}
                        class="w-28 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                    />
                </label>
            </div>
            <label class="flex items-center gap-2 text-sm text-gray-700">
                <input type="checkbox" bind:checked={form.starttls} class="cursor-pointer" />
                Use STARTTLS
            </label>
            <p class="text-xs text-gray-400">
                STARTTLS upgrades the connection to TLS (port 587, the usual choice). Turn it off
                only for a cleartext local relay you trust.
            </p>
        </section>

        <section class="space-y-3">
            <h3 class="text-sm font-semibold text-gray-700">Credentials</h3>
            <label class="flex flex-col gap-1">
                <span class="text-sm font-medium text-gray-700">Username</span>
                <input
                    type="text"
                    bind:value={form.username}
                    autocomplete="off"
                    spellcheck="false"
                    class="w-72 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                />
            </label>
            <label class="flex flex-col gap-1">
                <span class="text-sm font-medium text-gray-700">Password</span>
                <input
                    type="password"
                    value={form.password}
                    oninput={(e) => (form.password = e.currentTarget.value)}
                    placeholder={form.passwordSet ? '•••••••• (set — leave blank to keep)' : ''}
                    autocomplete="off"
                    spellcheck="false"
                    class="w-72 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                />
            </label>
        </section>

        <section class="space-y-3">
            <h3 class="text-sm font-semibold text-gray-700">Addresses</h3>
            <label class="flex flex-col gap-1">
                <span class="text-sm font-medium text-gray-700">From</span>
                <input
                    type="email"
                    bind:value={form.from}
                    placeholder="you@example.org"
                    autocomplete="off"
                    spellcheck="false"
                    class="w-72 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                />
            </label>
            <label class="flex flex-col gap-1">
                <span class="text-sm font-medium text-gray-700">Default recipient</span>
                <input
                    type="email"
                    bind:value={form.defaultRecipient}
                    placeholder="qsl-manager@example.org"
                    autocomplete="off"
                    spellcheck="false"
                    class="w-72 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                />
                <span class="text-xs text-gray-400">
                    Pre-fills the “send log” recipient in the logging app. Optional.
                </span>
            </label>
        </section>

        <section>
            <h3 class="mb-3 text-sm font-semibold text-gray-700">Timeout</h3>
            <label class="flex flex-col gap-1">
                <span class="text-sm font-medium text-gray-700">Send timeout (seconds)</span>
                <input
                    type="text"
                    inputmode="numeric"
                    value={form.timeoutSec ? String(form.timeoutSec) : ''}
                    oninput={numInput((n) => (form.timeoutSec = n))}
                    class="w-28 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                />
            </label>
            <p class="mt-2 text-xs text-gray-400">
                Bounds the whole connect + send round-trip. 30s suits most servers.
            </p>
        </section>

        {#if configState.emailDirty}
            <div
                class="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800"
            >
                ⚠ Email changes apply on daemon restart.
            </div>
        {/if}

        <TabFooter
            dirty={configState.emailDirty}
            saving={configState.savingEmail}
            status={configState.emailStatus}
            onsave={() => configState.saveEmail()}
            oncancel={() => configState.cancelEmail()}
        />
    </div>
{/if}
