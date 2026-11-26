// Rows on the Session's screen, for tests of what READS them (the panel, the
// rail, the Export dialog, the edit, Band Activity). In the app a row is only
// ever drawn from the daemon's answer (session.svelte.ts); a test of a reader
// does not need the daemon, so it draws rows as an answer would have.

import { session, _seedSessionForTests, type SessionQso } from '../lib/operate/session.svelte';

/** Draw one more row, newest first, as a read that now included it would. */
export function addSessionRow(q: Omit<SessionQso, 'id'>): void {
    _seedSessionForTests([q, ...session.qsos]);
}
