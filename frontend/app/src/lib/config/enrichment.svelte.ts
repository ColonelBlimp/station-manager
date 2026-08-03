/*
    Enrichment settings state (app Settings → Enrichment, ADR 0044 / ADR 0017) —
    the port of the standalone config SPA's Enrichment tab.

    THE SECTION IS PROVIDER-SPECIFIC; THE PAYLOAD IS NOT. Only two providers are
    implemented (hamnut for country, QRZ for callsign) and there is no
    /v1/lookup-types descriptor endpoint the way Forwarding has, so a "generic"
    chain editor would hardcode the same field knowledge with more machinery —
    CLAUDE.md's build-specific rule. But `mergeLookup` replaces the chain WHOLE,
    so the payload must still carry every provider, including any this build
    cannot render. That asymmetry is the whole risk in this module: the UI knows
    about QRZ, and the save has to know about everything.

    Hence #entry — the last loaded daemon view, kept verbatim and used as the
    base for every payload. The form edits a few named fields on top of it; it
    never becomes the source of the payload.

    Passwords follow the same three-state contract as Email (see
    email.svelte.ts): omitted = keep, typed = replace, `password_clear` = remove.
    TTLs are the mirror-image case and easy to get backwards: here 0 is a
    MEANINGFUL value ("never goes stale") and BLANK is the one that means
    "default", which is why they are held as strings — as numbers the two
    collapse into the same 0.
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

/** The named fields the form edits. Everything else rides via #entry. */
export interface EnrichmentDraft {
    hamnutEnabled: boolean;
    qrzEnabled: boolean;
    qrzUsername: string;
    /** What the operator typed. Blank = not retyped, never "erase it". */
    qrzPassword: string;
    /** Whether the daemon holds a QRZ password. Read-only; from the masked GET. */
    qrzPasswordSet: boolean;
    /** Operator pressed Remove — the stored password goes on the next save. */
    qrzPasswordCleared: boolean;
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

function draftFrom(e: LookupEntry): EnrichmentDraft {
    const qrz = e.chain.find((p) => p.name === QRZ_PROVIDER);
    return {
        hamnutEnabled: e.hamnut.enabled,
        qrzEnabled: qrz?.enabled ?? false,
        qrzUsername: qrz?.username ?? '',
        qrzPassword: '',
        qrzPasswordSet: qrz?.password_set ?? false,
        qrzPasswordCleared: false,
        // A stored 0 renders as "0", NOT as blank — unlike the SMTP port, where
        // 0 meant "unset". Here 0 is the operator's "never goes stale" and
        // blanking the box would misreport it as "using the default".
        countryTtlDays: String(e.country_ttl_days),
        stationTtlDays: String(e.station_ttl_days),
        refreshMaxInFlight: String(e.refresh_max_in_flight),
    };
}

/** Round-trip a provider unchanged: every field the daemon gave us goes back. */
function preserve(p: LookupProvider): LookupProviderPayload {
    return {
        name: p.name,
        enabled: p.enabled,
        url: p.url,
        username: p.username,
        timeout_sec: p.timeout_sec,
        view_url: p.view_url,
    };
}

class EnrichmentState {
    loading = $state(false);
    saving = $state(false);
    loaded = $state(false);
    error = $state('');
    draft = $state<EnrichmentDraft>(draftFrom(BLANK));

    // The daemon's last view, verbatim. The base of every payload — see the
    // module header. NOT reactive state the form binds to.
    #entry: LookupEntry = BLANK;

    #pristine = $state(JSON.stringify(draftFrom(BLANK)));

    dirty = $derived(JSON.stringify(this.draft) !== this.#pristine);

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
        // `loaded` is a precondition: a whole-chain write built from an entry we
        // never successfully fetched would delete every provider.
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
    setQrzPassword(v: string): void {
        this.draft.qrzPassword = v;
        if (v !== '') this.draft.qrzPasswordCleared = false;
    }

    /** Mark the stored QRZ password for removal, discarding any half-typed value. */
    clearQrzPassword(): void {
        this.draft.qrzPassword = '';
        this.draft.qrzPasswordCleared = true;
    }

    /** Undo a pending removal. */
    keepQrzPassword(): void {
        this.draft.qrzPasswordCleared = false;
    }

    reset(): void {
        this.draft = JSON.parse(this.#pristine) as EnrichmentDraft;
    }

    /**
     * Build the PUT payload from the LOADED ENTRY, with the form's edits applied
     * on top — never from the form alone. The chain is a whole-list replace
     * daemon-side, so a provider missing here is a provider deleted.
     */
    buildPayload(): LookupPayload {
        const d = this.draft;

        const chain = this.#entry.chain.map(preserve);
        let qrz = chain.find((p) => p.name === QRZ_PROVIDER);
        if (!qrz) {
            // First run: the daemon seeds a QRZ template, but a config that
            // predates it (or had the entry removed by hand) still needs one.
            qrz = { name: QRZ_PROVIDER, enabled: false };
            chain.push(qrz);
        }
        qrz.enabled = d.qrzEnabled;
        qrz.username = d.qrzUsername.trim();
        if (d.qrzPasswordCleared) qrz.password_clear = true;
        else if (d.qrzPassword !== '') qrz.password = d.qrzPassword;

        const payload: LookupPayload = {
            hamnut: { ...preserve(this.#entry.hamnut), enabled: d.hamnutEnabled },
            chain,
            refresh_max_in_flight: Number(d.refreshMaxInFlight) || 0,
        };
        // Omit a blank TTL — that is how the wire says "use the default". An
        // explicit "0" is a different instruction and must be sent as 0.
        if (d.countryTtlDays !== '') payload.country_ttl_days = Number(d.countryTtlDays);
        if (d.stationTtlDays !== '') payload.station_ttl_days = Number(d.stationTtlDays);
        return payload;
    }

    #apply(e: LookupEntry): void {
        this.#entry = e;
        this.draft = draftFrom(e);
        this.#pristine = JSON.stringify(this.draft);
    }
}

export const enrichmentState = new EnrichmentState();
