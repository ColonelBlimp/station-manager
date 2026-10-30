// A full page reload, behind a seam: jsdom cannot reload, so tests mock this
// module. The Settings leave guard (unsaved.ts, beforeunload) still prompts when
// the operator holds unsaved edits.
//
// It navigates rather than calling location.reload() (W-0024 acceptance F6).
// In Firefox (159, reproduced headless over WebDriver BiDi, 2026-10-30), once a
// location.reload() has been refused at that prompt ("Stay on Page"), a later
// location.reload() in the same document never completes — not even after
// "Leave Page", nor with the guard removed — so a gated tab's "Reload now"
// could not recover it. A navigation to the same address does complete, and
// still honours the prompt. location.replace adds no history entry; the
// fragment is dropped because a replace that changed only the fragment would
// scroll, not reload. Every SPA response is served Cache-Control: no-cache
// (internal/api/spa.go), so the navigation refetches as a reload would.
export function reloadPage(loc: Pick<Location, 'href' | 'replace'> = window.location): void {
    loc.replace(loc.href.split('#')[0]);
}
