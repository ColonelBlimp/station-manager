<script lang="ts">
    // The recovery question, shown in place of the workspace (ADR 0100 §6;
    // W-0029 part 4, E). After a start that did not follow a confirmed exit,
    // an earlier session with contacts can still be open: the operator says
    // whether to continue it or start a new one, and the workspace opens after
    // the answer. There is no "not now".
    //
    // The same card shows when the session's state could not be read at the
    // page's load (an unreadable state is never a clean start): it says so and
    // offers to try again.
    //
    // The words of the question are the operator's ruling of 2026-11-23.
    // Nothing else is on this screen, by the same ruling: no rail, no Settings.
    import { recovery, answerRecovery, readSessionState } from '../recovery.svelte';
    import Logo from './Logo.svelte';

    // The session's start, in UTC, as contacts are logged.
    function startedAt(iso: string): string {
        const d = new Date(iso);
        if (Number.isNaN(d.getTime())) return iso;
        return `${d.toISOString().slice(0, 10)} ${d.toISOString().slice(11, 16)} UTC`;
    }

    function contacts(n: number): string {
        return n === 1 ? '1 contact' : `${n} contacts`;
    }

    let retrying: boolean = $state(false);
    const retry = async (): Promise<void> => {
        if (retrying) return;
        retrying = true;
        await readSessionState();
        retrying = false;
    };
</script>

<div class="flex min-h-screen items-center justify-center bg-canvas p-6">
    <main class="w-full max-w-2xl rounded-xl border border-line bg-surface p-8 shadow-sm">
        {#if recovery.status === 'question' && recovery.session !== null}
            <div class="flex items-center justify-center gap-2 pb-2">
                <Logo class="size-8 shrink-0 text-focus" />
                <h1 class="text-2xl font-semibold text-ink">The last session is still open</h1>
            </div>
            <div class="space-y-3 py-4 text-ink">
                <!-- Said of the session as it is now, and of nothing in its past (operator,
                     2026-11-24; W-0029 part 5, question 3). Two earlier wordings are false
                     in some case: "Station Manager did not close normally last time." for a
                     session left open by a contact logged while no app was running; "and
                     was never closed" for a session that was closed, continued, and then
                     left open (the start shown is the session's first). -->
                <p>
                    The session that started
                    {startedAt(recovery.session.openedAt)} has {contacts(
                        recovery.session.contactCount
                    )}.
                </p>
            </div>
            <!-- What a control pressed in the WORKSPACE came to, when this card is
                 what the operator is then looking at (a stale refusal can carry a
                 state that asks this question; codex-290). -->
            {#if recovery.controlError !== ''}
                <p role="alert" class="pb-3 text-sm text-warn">{recovery.controlError}</p>
            {/if}
            {#if recovery.error !== ''}
                <p role="alert" class="pb-3 text-sm text-warn">{recovery.error}</p>
            {/if}
            <div class="flex flex-row items-center justify-center gap-x-4 pt-2">
                <button
                    type="button"
                    class="btn btn-primary"
                    disabled={recovery.answering}
                    onclick={() => void answerRecovery('continue')}>Continue this session</button
                >
                <button
                    type="button"
                    class="btn"
                    disabled={recovery.answering}
                    onclick={() => void answerRecovery('new')}>Start a new session</button
                >
            </div>
            <p class="pt-4 text-center text-sm text-muted">
                If you start a new one, the earlier session is closed and kept, with its contacts.
            </p>
        {:else}
            <div class="flex items-center justify-center gap-2 pb-2">
                <Logo class="size-8 shrink-0 text-focus" />
                <h1 class="text-2xl font-semibold text-ink">The session could not be read</h1>
            </div>
            <div class="space-y-3 py-4 text-ink">
                <p>
                    Station Manager could not read the operating session from the daemon, so the
                    workspace is not shown yet.
                </p>
            </div>
            <!-- What a control pressed in the WORKSPACE came to, when this card is
                 what the operator is then looking at (a stale refusal can carry a
                 state that asks this question; codex-290). -->
            {#if recovery.controlError !== ''}
                <p role="alert" class="pb-3 text-sm text-warn">{recovery.controlError}</p>
            {/if}
            {#if recovery.error !== ''}
                <p role="alert" class="pb-3 text-sm text-warn">{recovery.error}</p>
            {/if}
            <div class="flex flex-row items-center justify-center pt-2">
                <button
                    type="button"
                    class="btn btn-primary"
                    disabled={retrying}
                    onclick={() => void retry()}>Try again</button
                >
            </div>
        {/if}
    </main>
</div>
