<script lang="ts">
    // Notification history (W-0001 / ADR 0076) — the durable counterpart to the
    // transient Toasts. A global right slide-over, mounted once beside <Toasts/>,
    // overlaying the shell with its own backdrop + focus handling and BELOW the
    // toast layer in z-order. It deliberately does NOT reuse the Operate util-rail
    // / pile-up docking or content-push mechanics — it is a pure overlay.
    //
    // A fresh GET runs on every open (the re-fetch is the reload path this feature
    // must survive); unknown/future detail shapes degrade to "Details unavailable"
    // rather than stringifying raw content.
    import { fade, fly } from 'svelte/transition';
    import { ui, closeNotifications } from './state.svelte';
    import { fetchNotifications, type NotificationEvent } from '../api/notifications';

    let loading = $state(false);
    let error = $state('');
    let items = $state<NotificationEvent[]>([]);
    let closeButton = $state<HTMLButtonElement | null>(null);

    async function load(): Promise<void> {
        loading = true;
        error = '';
        const outcome = await fetchNotifications(50);
        if (outcome.kind === 'ok') {
            items = outcome.items;
        } else {
            error = outcome.message;
        }
        loading = false;
    }

    // Fetch fresh on each open; move focus into the dialog.
    $effect(() => {
        if (ui.notificationsOpen) {
            void load();
            closeButton?.focus();
        }
    });

    function onKeydown(e: KeyboardEvent): void {
        if (e.key === 'Escape') closeNotifications();
    }

    function kindLabel(kind: string): string {
        switch (kind) {
            case 'export.adif_failed':
                return 'ADIF export failed';
            case 'forward.failed':
                return 'Upload failed';
            default:
                return kind;
        }
    }

    // Compact, typed summary. NEVER stringifies the raw detail — an unknown or
    // future shape degrades to a fixed placeholder.
    function detailSummary(ev: NotificationEvent): string {
        const d = ev.detail;
        if (d && typeof d === 'object') {
            const o = d as Record<string, unknown>;
            if (
                ev.kind === 'export.adif_failed' &&
                typeof o.count === 'number' &&
                typeof o.outcome === 'string'
            ) {
                return `${o.count} QSO${o.count === 1 ? '' : 's'} · ${o.outcome}`;
            }
            if (
                ev.kind === 'forward.failed' &&
                typeof o.forwarder === 'string' &&
                typeof o.action === 'string' &&
                typeof o.attempts === 'number'
            ) {
                return `${o.forwarder} · ${o.action} · ${o.attempts} attempt${o.attempts === 1 ? '' : 's'}`;
            }
        }
        return 'Details unavailable';
    }

    function severityDot(sev: string): string {
        switch (sev) {
            case 'error':
                return 'bg-red-500';
            case 'warn':
                return 'bg-amber-500';
            default:
                return 'bg-sky-500';
        }
    }

    function fmtTime(iso: string): string {
        const t = new Date(iso);
        return isNaN(t.getTime()) ? iso : t.toLocaleString();
    }
</script>

<svelte:window onkeydown={ui.notificationsOpen ? onKeydown : undefined} />

{#if ui.notificationsOpen}
    <div class="fixed inset-0 z-50" role="dialog" aria-modal="true" aria-labelledby="notif-title">
        <button
            type="button"
            class="absolute inset-0 cursor-default bg-gray-500/75 dark:bg-gray-900/50"
            aria-label="Close notification history"
            onclick={closeNotifications}
            transition:fade={{ duration: 150 }}
        ></button>

        <div
            class="absolute inset-y-0 right-0 flex w-full max-w-md flex-col bg-surface shadow-xl outline-1 outline-line"
            transition:fly={{ x: 400, duration: 200 }}
        >
            <div class="flex items-center justify-between border-b border-line px-4 py-3">
                <h2 id="notif-title" class="text-base font-semibold text-ink">Notifications</h2>
                <button
                    bind:this={closeButton}
                    class="cursor-pointer rounded-md text-muted hover:text-ink"
                    aria-label="Close"
                    onclick={closeNotifications}
                >
                    <svg
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.5"
                        aria-hidden="true"
                        class="size-5"
                    >
                        <path
                            d="M6 18 18 6M6 6l12 12"
                            stroke-linecap="round"
                            stroke-linejoin="round"
                        />
                    </svg>
                </button>
            </div>

            <div class="flex-1 overflow-y-auto">
                {#if loading}
                    <p class="p-4 text-sm text-muted">Loading…</p>
                {:else if error}
                    <div class="p-4">
                        <p class="text-sm text-red-600 dark:text-red-400">{error}</p>
                        <button class="btn mt-3" onclick={() => void load()}>Retry</button>
                    </div>
                {:else if items.length === 0}
                    <p class="p-4 text-sm text-muted">No notifications yet.</p>
                {:else}
                    <ul class="divide-y divide-line">
                        {#each items as ev (ev.id)}
                            <li class="flex gap-x-3 px-4 py-3">
                                <span
                                    class="mt-1.5 size-2 shrink-0 rounded-full {severityDot(
                                        ev.severity
                                    )}"
                                    aria-hidden="true"
                                ></span>
                                <span class="sr-only">Severity: {ev.severity}</span>
                                <div class="min-w-0 flex-1">
                                    <p class="text-sm font-medium text-ink">{kindLabel(ev.kind)}</p>
                                    <p class="truncate text-xs text-muted">{detailSummary(ev)}</p>
                                    <p class="mt-0.5 text-xs text-muted">
                                        <span>{fmtTime(ev.occurred_at)}</span>
                                        <span class="text-muted/70"> · {ev.build}</span>
                                    </p>
                                </div>
                            </li>
                        {/each}
                    </ul>
                {/if}
            </div>
        </div>
    </div>
{/if}
