/*
    Build-environment probe — fetches the daemon's build env once (GET /v1/version)
    so the UI can flag a DEV daemon. A dev `task run:smd` and the deployed daemon
    serve identical SPAs on :8080, so without this cue you can't tell which you're
    talking to. On a "dev" daemon it prefixes the browser-tab title with "DEV · "
    and flips `isDev` so the nav can render a DEV pill. Best-effort: a failed or
    absent fetch shows no marker (treated as a normal deployed daemon).

    Duplicated per SPA (separate Vite projects) — mirror changes across the three.
*/
class BuildEnv {
    /** True once the daemon reports a non-packaged ("dev") build. */
    isDev = $state(false);
    #loaded = false;

    /** Fetch the build env once and, if dev, mark the tab title. Idempotent. */
    async load(): Promise<void> {
        if (this.#loaded) return;
        this.#loaded = true;
        try {
            const res = await fetch('/v1/version');
            if (!res.ok) return;
            const body: unknown = await res.json();
            const env = (body as { env?: unknown } | null)?.env;
            if (env === 'dev') {
                this.isDev = true;
                document.title = `DEV · ${document.title}`;
            }
        } catch {
            // best-effort; a probe failure just means "no marker"
        }
    }
}

export const buildEnv = new BuildEnv();
