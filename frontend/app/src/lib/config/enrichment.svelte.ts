/*
    Enrichment settings state (app Settings → Enrichment, ADR 0044 / ADR 0017) —
    the port of the standalone config SPA's Enrichment tab.

    EVERY SOURCE THE DAEMON REPORTS IS A DRAFT, including ones this build has no
    specific knowledge of. That is not generality for its own sake: `mergeLookup`
    replaces the chain WHOLE, with no merge-by-name for absent entries, so a
    payload that omits a provider DELETES it. Holding every provider as a draft
    makes the safe payload the natural one to build, rather than something the
    save has to remember to reconstruct.

    Per-provider PRESENTATION is still specific (PROVIDERS below): only hamnut
    and QRZ are implemented, and unlike Forwarding there is no descriptor
    endpoint to drive a data-driven form from. An unrecognised provider gets the
    generic treatment — the wire shape (LookupProviderInfo) is uniform, so its
    username/password are editable; only the friendly name and blurb are missing.

    Passwords follow the same three-state contract as Email (email.svelte.ts):
    omitted = keep, typed = replace, `password_clear` = remove. TTLs are the
    mirror-image case and easy to get backwards — here 0 is MEANINGFUL ("never
    goes stale") and BLANK means "use the default", which is why they are held as
    strings: as numbers the two collapse into the same 0.
*/
import {
    fetchLookup,
    saveLookup,
    QRZ_PROVIDER,
    HAMNUT_PROVIDER,
    type LookupEntry,
    type LookupPayload,
    type LookupProvider,
    type LookupProviderPayload,
} from '../api/lookup';
import { toasts } from '../ui/toasts.svelte';

/** What this build knows about a provider, beyond the uniform wire shape. */
export interface ProviderMeta {
    label: string;
    blurb: string;
    /** False only for a provider that is anonymous by design (hamnut). */
    credentialed: boolean;
}

const PROVIDERS: Record<string, ProviderMeta> = {
    [HAMNUT_PROVIDER]: {
        label: 'Hamnut',
        blurb: 'Resolves DXCC / CQ / ITU zones from the callsign prefix. Free and anonymous — no credentials needed.',
        credentialed: false,
    },
    [QRZ_PROVIDER]: {
        label: 'QRZ.com',
        blurb: 'Fills name, grid and address from QRZ. Needs a QRZ subscription with XML/API access.',
        credentialed: true,
    },
};

/** One lookup source as the form holds it: the masked entry plus local edits. */
export interface ProviderDraft {
    name: string;
    /** hamnut is the single country provider; the rest are the callsign chain. */
    country: boolean;
    enabled: boolean;
    username: string;
    /** What the operator typed. Blank = not retyped, never "erase it". */
    password: string;
    /** Whether the daemon holds one. Read-only; from the masked GET. */
    passwordSet: boolean;
    /** Operator pressed Remove — the stored password goes on the next save. */
    passwordCleared: boolean;
    /** Round-tripped untouched: the daemon takes these AS SENT. */
    url: string;
    timeoutSec: number;
    viewUrl: string;
}

export interface EnrichmentDraft {
    providers: ProviderDraft[];
    /** Blank = use the daemon default. "0" = never goes stale. Not the same. */
    countryTtlDays: string;
    stationTtlDays: string;
    refreshMaxInFlight: string;
}

const BLANK: LookupEntry = {
    hamnut: {
        name: HAMNUT_PROVIDER,
        enabled: false,
        url: '',
        username: '',
        password_set: false,
        timeout_sec: 0,
        view_url: '',
    },
    chain: [],
    country_ttl_days: 0,
    station_ttl_days: 0,
    refresh_max_in_flight: 0,
};

function providerDraft(p: LookupProvider, country: boolean): ProviderDraft {
    return {
        name: p.name,
        country,
        enabled: p.enabled,
        username: p.username,
        password: '',
        passwordSet: p.password_set,
        passwordCleared: false,
        url: p.url,
        timeoutSec: p.timeout_sec,
        viewUrl: p.view_url,
    };
}

function draftFrom(e: LookupEntry): EnrichmentDraft {
    return {
        // hamnut first: it is the country source and the only non-chain entry.
        providers: [providerDraft(e.hamnut, true), ...e.chain.map((p) => providerDraft(p, false))],
        // A stored 0 renders as "0", NOT as blank — unlike the SMTP port, where
        // 0 meant "unset". Here 0 is the operator's "never goes stale" and
        // blanking the box would misreport it as "using the default".
        countryTtlDays: String(e.country_ttl_days),
        stationTtlDays: String(e.station_ttl_days),
        refreshMaxInFlight: String(e.refresh_max_in_flight),
    };
}

class EnrichmentState {
    loading = $state(false);
    saving = $state(false);
    loaded = $state(false);
    error = $state('');
    draft = $state<EnrichmentDraft>(draftFrom(BLANK));

    #pristine = $state(JSON.stringify(draftFrom(BLANK)));

    dirty = $derived(JSON.stringify(this.draft) !== this.#pristine);

    /** What this build knows about a provider, or undefined if it doesn't. */
    metaFor(name: string): ProviderMeta | undefined {
        return PROVIDERS[name];
    }

    /** A provider's display name, never blank — falls back to its wire name. */
    labelFor(name: string): string {
        return PROVIDERS[name]?.label ?? name;
    }

    /**
     * True when THIS source has unsaved edits.
     *
     * Exists because each source collapses into a disclosure: the footer can say
     * "unsaved changes" but not where, so a collapsed card has to mark itself.
     * Compared against the pristine snapshot rather than tracked as a flag, so
     * an edit that is typed and then undone stops counting.
     */
    hasEdits(name: string): boolean {
        const base = (JSON.parse(this.#pristine) as EnrichmentDraft).providers.find(
            (p) => p.name === name
        );
        const now = this.draft.providers.find((p) => p.name === name);
        if (!now) return false;
        if (!base) return true; // unknown to the snapshot — treat as changed
        return JSON.stringify(now) !== JSON.stringify(base);
    }

    async load(): Promise<void> {
        if (this.loading) return;
        this.loading = true;
        // Invalidate before awaiting: while a reload is pending the retained
        // draft is not known-current and must neither render nor save.
        this.loaded = false;
        this.error = '';
        const out = await fetchLookup();
        this.loading = false;
        if (out.kind === 'error') {
            this.error = out.message;
            return;
        }
        this.#apply(out.lookup);
        this.loaded = true;
    }

    async save(): Promise<void> {
        // `loaded` is a precondition: a whole-chain write built from a draft we
        // never successfully filled would delete every provider.
        if (this.saving || !this.loaded || !this.dirty) return;
        this.saving = true;
        try {
            const res = await saveLookup(this.buildPayload());
            if (res.kind === 'error') {
                toasts.error(`Save failed: ${res.message}`);
                return;
            }
            this.#apply(res.lookup);
            toasts.info('Enrichment settings saved. Restart the daemon to apply.');
        } finally {
            this.saving = false;
        }
    }

    /** Record a typed password; supersedes a pending removal. */
    setPassword(name: string, v: string): void {
        const p = this.draft.providers.find((x) => x.name === name);
        if (!p) return;
        p.password = v;
        if (v !== '') p.passwordCleared = false;
    }

    /**
     * Mark a stored password for removal, discarding any half-typed value.
     *
     * ALSO SWITCHES THE SOURCE OFF when it is one that cannot work without
     * credentials. The daemon refuses an enabled QRZ with no password (it used
     * to accept it and then fail to START at the next restart — clean-room
     * review 9732ab7914af), so leaving the toggle on would build a payload
     * guaranteed to 400. Turning it off is also what removing a login MEANS:
     * rotating one is served by typing a new value, not by removing the old.
     */
    clearPassword(name: string): void {
        const p = this.draft.providers.find((x) => x.name === name);
        if (!p) return;
        p.password = '';
        p.passwordCleared = true;
        if (this.metaFor(p.name)?.credentialed) p.enabled = false;
    }

    /** Undo a pending removal. */
    keepPassword(name: string): void {
        const p = this.draft.providers.find((x) => x.name === name);
        if (p) p.passwordCleared = false;
    }

    reset(): void {
        this.draft = JSON.parse(this.#pristine) as EnrichmentDraft;
    }

    /**
     * Build the PUT payload. EVERY provider rides, because the chain is a
     * whole-list replace daemon-side — a provider missing here is a provider
     * deleted, along with its url and timeout.
     */
    buildPayload(): LookupPayload {
        const d = this.draft;
        const hamnut = d.providers.find((p) => p.country);
        const chain = d.providers.filter((p) => !p.country);

        const payload: LookupPayload = {
            hamnut: toPayload(hamnut ?? providerDraft(BLANK.hamnut, true)),
            chain: chain.map(toPayload),
            refresh_max_in_flight: Number(d.refreshMaxInFlight) || 0,
        };
        // Omit a blank TTL — that is how the wire says "use the default". An
        // explicit "0" is a different instruction and must be sent as 0.
        if (d.countryTtlDays !== '') payload.country_ttl_days = Number(d.countryTtlDays);
        if (d.stationTtlDays !== '') payload.station_ttl_days = Number(d.stationTtlDays);
        return payload;
    }

    #apply(e: LookupEntry): void {
        this.draft = draftFrom(e);
        this.#pristine = JSON.stringify(this.draft);
    }
}

/**
 * One provider on the wire. url / timeout_sec / view_url are round-tripped
 * verbatim: the daemon takes them AS SENT and re-stamps defaults only for the
 * two names Normalize knows, so anything else is silently emptied if dropped.
 */
function toPayload(p: ProviderDraft): LookupProviderPayload {
    const out: LookupProviderPayload = {
        name: p.name,
        enabled: p.enabled,
        url: p.url,
        username: p.username.trim(),
        timeout_sec: p.timeoutSec,
        view_url: p.viewUrl,
    };
    if (p.passwordCleared) out.password_clear = true;
    else if (p.password !== '') out.password = p.password;
    return out;
}

export const enrichmentState = new EnrichmentState();
