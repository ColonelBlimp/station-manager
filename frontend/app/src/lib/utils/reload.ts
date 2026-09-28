// A full page reload, behind a seam: jsdom cannot reload, so tests mock this
// module. The Settings leave guard (unsaved.ts, beforeunload) still prompts when
// the operator holds unsaved edits.
export function reloadPage(): void {
    window.location.reload();
}
