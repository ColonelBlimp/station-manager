/*
    FT8 settings state (app Settings → FT8, ADR 0044) — the port of the
    standalone config SPA's FT8 tab, and one of the last two surfaces keeping
    that SPA alive.

    FOUR BLOCKS, ONE SAVE. The subsystem master switch, the Band Activity
    display prefs, PSK Reporter and the decode log are one page to the operator
    and go out as one PUT. They differ in WHEN they take effect, which is the
    distinction `restartRequired` exists to make visible: the display prefs are
    pure SPA presentation and are applied to the running view the moment they
    save, while the other three are read at daemon startup and need a restart.
    A section that said "restart required" for every edit would make the one
    that genuinely needs it unremarkable.

    NUMBERS ARE HELD AS STRINGS (row cap, PSK port) so blank survives as blank —
    it is what asks the daemon for its default, and as a number it would collapse
    into a 0 the operator never chose. Same reasoning as the SMTP port
    (email.svelte.ts); the mirror-image case is Enrichment's TTLs, where 0 is
    MEANINGFUL and blank is not.

    NO COLOUR PICKERS — operator's ruling, 2026-08-05. The config SPA's FT8 tab
    offers three CQ highlight colours; nothing in this shell has ever read them
    (Ft8BandActivity.svelte uses a theme-aware palette, and the retired
    frontend/logging SPA was their only consumer). A hand-picked hex cannot serve
    both light and dark, so the palette stays. The three values are still LOADED
    AND SENT BACK VERBATIM: `ft8_display` is a whole-block replace daemon-side,
    so dropping them from the payload would erase a hand-set config.json colour
    on the first save from this page (pinned by W1 in ft8.svelte.test.ts).
*/
import {
    fetchFt8Settings,
    saveFt8Settings,
    type Ft8Settings,
    type Ft8DisplayEntry,
} from '../api/ft8-config';
import { toasts } from '../ui/toasts.svelte';

/** The Band Activity display prefs as the live FT8 view consumes them. */
export interface Ft8DisplayPrefs {
    feedMode: 'accumulate' | 'single';
    historyMax: number;
    cqToTop: boolean;
    hideHashedCalls: boolean;
}

/*
    Injected by main.ts (ADR 0045 DI — this module never imports the app
    bootstrap): push the just-saved display prefs into the running FT8 view so a
    changed feed mode / row cap / CQ-float / hide-hashed takes effect with no
    page reload. Without it these prefs are read once at boot, and a save would
    look like it had done nothing until F5 — which is indistinguishable from a
    save that failed silently.

    PARTIAL on purpose, mirroring setFt8DisplayPrefs' own signature: a field we
    have no value for is OMITTED rather than defaulted here. A row cap of 0 means
    "the daemon's default" on the wire, and pushing that 0 through would cap the
    live feed at zero rows. The one default belongs in the view that owns it, not
    duplicated in the settings form.
*/
let onPrefsSaved: ((p: Partial<Ft8DisplayPrefs>) => void) | null = null;
export function setFt8PrefsSaved(fn: (p: Partial<Ft8DisplayPrefs>) => void): void {
    onPrefsSaved = fn;
}

/** The form's shape. Blank row cap / port = "use the daemon's default". */
export interface Ft8Draft {
    enabled: boolean;
    historyMax: string;
    feedMode: 'accumulate' | 'single';
    cqToTop: boolean;
    hideHashedCalls: boolean;
    /* Round-tripped, never rendered — see the erasure note in the header. */
    highlightUnworked: string;
    highlightWorked: string;
    highlightCalling: string;
    pskEnabled: boolean;
    pskHost: string;
    pskPort: string;
    decodeLogEnabled: boolean;
    decodeLogPath: string;
}

const BLANK: Ft8Settings = {
    enabled: false,
    display: {
        history_max: 0,
        feed_mode: 'accumulate',
        cq_to_top: false,
        hide_hashed_calls: false,
        highlight_unworked: '',
        highlight_worked: '',
        highlight_calling: '',
    },
    psk: { enabled: false, host: '', port: 0 },
    decodeLog: { enabled: false, path: '' },
};

/** 0 on the wire means "use the default"; the box shows that as empty, so the
 *  operator is not looking at a literal 0 they never chose. */
function num(n: number): string {
    return n > 0 ? String(n) : '';
}

function draftFrom(s: Ft8Settings): Ft8Draft {
    return {
        enabled: s.enabled,
        historyMax: num(s.display.history_max),
        feedMode: s.display.feed_mode,
        cqToTop: s.display.cq_to_top,
        hideHashedCalls: s.display.hide_hashed_calls,
        highlightUnworked: s.display.highlight_unworked,
        highlightWorked: s.display.highlight_worked,
        highlightCalling: s.display.highlight_calling,
        pskEnabled: s.psk.enabled,
        pskHost: s.psk.host,
        pskPort: num(s.psk.port),
        decodeLogEnabled: s.decodeLog.enabled,
        decodeLogPath: s.decodeLog.path,
    };
}

class Ft8SettingsState {
    loading = $state(false);
    saving = $state(false);
    loaded = $state(false);
    error = $state('');
    draft = $state<Ft8Draft>(draftFrom(BLANK));

    // JSON of the last loaded/saved draft. Key order is stable (draftFrom always
    // builds it the same way), so a string compare is a valid change check.
    #pristine = $state(JSON.stringify(draftFrom(BLANK)));

    dirty = $derived(JSON.stringify(this.draft) !== this.#pristine);

    /**
     * Whether the pending edits include one the daemon only reads at startup.
     *
     * The display prefs are excluded deliberately: they are applied to the
     * running view on save (see onPrefsSaved), so telling the operator to
     * restart for them would be false — and would drain the meaning out of the
     * notice on the edits that genuinely need it.
     */
    restartRequired = $derived.by(() => {
        const base = JSON.parse(this.#pristine) as Ft8Draft;
        const d = this.draft;
        return (
            d.enabled !== base.enabled ||
            d.pskEnabled !== base.pskEnabled ||
            d.pskHost !== base.pskHost ||
            d.pskPort !== base.pskPort ||
            d.decodeLogEnabled !== base.decodeLogEnabled ||
            d.decodeLogPath !== base.decodeLogPath
        );
    });

    async load(): Promise<void> {
        if (this.loading) return;
        this.loading = true;
        // Invalidate before awaiting: while a reload is pending the retained
        // draft is not known-current and must neither render nor save. Every
        // block here is a whole-block write, so showing stale values as current
        // is one Save away from persisting them (same rule as stationState).
        this.loaded = false;
        this.error = '';
        const res = await fetchFt8Settings();
        this.loading = false;
        if (res.kind === 'error') {
            this.error = res.message;
            return;
        }
        this.#apply(res.settings);
        this.loaded = true;
    }

    async save(): Promise<void> {
        // `loaded` is a data-safety precondition, not a UI nicety: all four
        // blocks are whole-block replaces, so a PUT built from a draft we never
        // filled would wipe the operator's FT8 configuration.
        if (this.saving || !this.loaded || !this.dirty) return;
        this.saving = true;
        // Captured BEFORE the write. #apply rebaselines the draft against the
        // response, so restartRequired is false by the time the confirmation is
        // raised no matter what was saved — and the in-page banner has vanished
        // with `dirty` at the same moment, leaving nothing to say a restart is
        // still owed.
        const needsRestart = this.restartRequired;
        // The baseline as it stands BEFORE the write — what a timed-out save is
        // judged against (see #reconcileAfterTimeout). Parsed from the pristine
        // snapshot rather than aliasing `this.draft`, which the reconcile needs
        // to compare against and which stays live throughout.
        const before = JSON.parse(this.#pristine) as Ft8Draft;
        try {
            const res = await saveFt8Settings(this.buildPayload());
            if (res.kind === 'error') {
                if (res.timedOut) {
                    await this.#reconcileAfterTimeout(needsRestart, before);
                    return;
                }
                toasts.error(`Save failed: ${res.message}`);
                return;
            }
            this.#apply(res.settings);
            // Push what the daemon STORED, never what was typed: it clamps the
            // row cap (10..2000) and normalises the feed mode, so the draft can
            // differ from reality by the time this runs. Only on success —
            // applying a refused value would show the operator a live view that
            // disagrees with both their form and the daemon.
            onPrefsSaved?.(livePrefs(res.settings.display));
            toasts.info(
                needsRestart
                    ? 'FT8 settings saved — restart the daemon to apply them.'
                    : 'FT8 settings saved.'
            );
        } finally {
            this.saving = false;
        }
    }

    /**
     * Settle a write whose outcome we never learned (clean-room review
     * 569b2236 P2).
     *
     * A timed-out PUT reached the daemon with no response, so it may already
     * have committed. Saying "Save failed" asserts the one thing we do not
     * know, and the retry it invites resends all four blocks as whole-block
     * replaces — which can overwrite a change made in between.
     *
     * So: re-read, and ask whether THE FIELDS THIS SAVE CHANGED have moved.
     *
     * Two narrower questions were tried first and both were wrong:
     *
     *   "Does the stored block equal my draft?" (clean-room review e41425f1
     *   P2) — the daemon NORMALISES on the way in: a row cap of 5 is stored as
     *   10, a host is trimmed. Every normalised write then reports as missing,
     *   leaving the form dirty and the operator retrying a write that had
     *   succeeded.
     *
     *   "Does the stored block differ from my baseline?" (clean-room review
     *   f5ff2df5 P2) — that is movement by ANYONE. The standalone config SPA
     *   is still served at /config/ until its General tab is ported, so a
     *   second writer genuinely exists; its change would be read as proof of
     *   ours, replacing the operator's draft with someone else's values and
     *   announcing them as theirs.
     *
     * Asking only about the edited fields needs no knowledge of the daemon's
     * normalisation — just whether what we asked to change is now different
     * from what was there. Someone editing a different field no longer counts.
     * A second client editing THE SAME field in this window is genuinely
     * indistinguishable, and nothing available here can separate them.
     *
     * Moved → the write landed; adopt the response exactly as the unambiguous
     * success path does, so the operator ends up where they would have been
     * had the response arrived (including the daemon's clamped value).
     *
     * Otherwise → keep the draft exactly as typed, leave the baseline alone so
     * `dirty` still points at the difference, and hedge the wording. Never
     * discard typed input on an inference.
     *
     * Either way the live view is pushed from what is STORED, never from what
     * was attempted.
     */
    async #reconcileAfterTimeout(needsRestart: boolean, before: Ft8Draft): Promise<void> {
        const out = await fetchFt8Settings();
        if (out.kind === 'error') {
            toasts.error(
                'Save timed out and the daemon could not be re-read — it is unknown whether your FT8 settings were stored.'
            );
            return;
        }
        onPrefsSaved?.(livePrefs(out.settings.display));
        if (editedFieldsMoved(before, this.draft, draftFrom(out.settings))) {
            this.#apply(out.settings);
            toasts.warn(
                needsRestart
                    ? 'Save timed out, but the daemon does have your FT8 settings — restart the daemon to apply them.'
                    : 'Save timed out, but the daemon does have your FT8 settings.'
            );
            return;
        }
        toasts.warn(
            'Save timed out and the daemon does not appear to have your changes — press Save again.'
        );
    }

    /** Revert edits to the last loaded/saved snapshot. */
    reset(): void {
        this.draft = JSON.parse(this.#pristine) as Ft8Draft;
    }

    buildPayload(): Ft8Settings {
        const d = this.draft;
        return {
            enabled: d.enabled,
            display: {
                // Blank → 0, which is how the wire asks for the default.
                history_max: Number(d.historyMax) || 0,
                feed_mode: d.feedMode,
                cq_to_top: d.cqToTop,
                hide_hashed_calls: d.hideHashedCalls,
                highlight_unworked: d.highlightUnworked,
                highlight_worked: d.highlightWorked,
                highlight_calling: d.highlightCalling,
            },
            psk: {
                enabled: d.pskEnabled,
                host: d.pskHost.trim(),
                port: Number(d.pskPort) || 0,
            },
            decodeLog: { enabled: d.decodeLogEnabled, path: d.decodeLogPath.trim() },
        };
    }

    #apply(s: Ft8Settings): void {
        this.draft = draftFrom(s);
        this.#pristine = JSON.stringify(this.draft);
    }
}

/**
 * Whether every field this save asked to change is now different from what was
 * there before it — the closest thing to "my write landed" available after a
 * timeout (see #reconcileAfterTimeout).
 *
 * EVERY edited field, not any: a partial move is not this save's signature, and
 * treating it as one is how another client's edit gets adopted as ours.
 *
 * False when nothing was edited. That cannot happen through save() (it refuses
 * a clean draft), but "no request, therefore success" is the wrong default for
 * a helper that decides whether to overwrite the operator's form.
 */
function editedFieldsMoved(before: Ft8Draft, draft: Ft8Draft, stored: Ft8Draft): boolean {
    const edited = (Object.keys(before) as (keyof Ft8Draft)[]).filter(
        (k) => draft[k] !== before[k]
    );
    return edited.length > 0 && edited.every((k) => stored[k] !== before[k]);
}

/** The stored display block as the live view's setter takes it. history_max is
 *  omitted when the daemon reports 0 ("use the default") — see onPrefsSaved. */
function livePrefs(d: Ft8DisplayEntry): Partial<Ft8DisplayPrefs> {
    const p: Partial<Ft8DisplayPrefs> = {
        feedMode: d.feed_mode,
        cqToTop: d.cq_to_top,
        hideHashedCalls: d.hide_hashed_calls,
    };
    if (d.history_max > 0) p.historyMax = d.history_max;
    return p;
}

export const ft8SettingsState = new Ft8SettingsState();
