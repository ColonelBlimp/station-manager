package api

import (
	"context"
	stderrs "errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/email"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// W-0010 outcome 11: a session-email mark is not lost to delivery bookkeeping.
// Every mutation below is a real write through the real services, placed with
// the handler's seams (no sleeps): sessionEmailAfterSend runs after the send and
// before the first attempt; sessionEmailBeforeMark runs before each attempt's
// marking transaction.

type markFixture struct {
	t      *testing.T
	srv    *Server
	fake   *apiSmtpFake
	lbID   int64
	synced [][]int64 // each stamp-sync enqueue's ids
}

func newMarkFixture(t *testing.T) *markFixture {
	t.Helper()
	fake := newAPISmtpFake(t)
	t.Cleanup(fake.close)
	host, port := fake.hostPort()
	srv := serverWithForwarders(t, forwarderCfg("qrz", "qrz", true, "insert", "update", "delete"))
	srv.mailer = email.New(types.SmtpConfig{
		Enabled: true, Host: host, Port: port, From: "f@x", StartTLS: false, TimeoutSec: 5,
	}, srv.logger)
	f := &markFixture{t: t, srv: srv, fake: fake, lbID: createTestLogbook(t, srv, "My Log", "G4ABC")}
	srv.sessionStampSync = func(_ context.Context, ids []int64) (int, error) {
		f.synced = append(f.synced, slices.Clone(ids))
		return len(ids), nil
	}
	return f
}

// qso submits a distinct QSO and returns its id and UUID.
func (f *markFixture) qso(call, timeOn string) (int64, string) {
	f.t.Helper()
	rec := strings.Replace(testQsoADIF, "M0CMC", call, 1)
	rec = strings.Replace(rec, "<TIME_ON:4>0845", "<TIME_ON:4>"+timeOn, 1)
	return submitAndGetID(f.t, f.srv, f.lbID, rec)
}

func (f *markFixture) send(ctx context.Context, uuids ...string) SessionEmailResponse {
	f.t.Helper()
	body := `{"to":"manager@example.com","uuids":["` + strings.Join(uuids, `","`) + `"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/session/email", strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()
	f.srv.handleSessionEmail(w, req)
	if w.Code != http.StatusOK {
		f.t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp SessionEmailResponse
	if err := unmarshalJSON(w.Body.String(), &resp); err != nil {
		f.t.Fatalf("decode response: %v", err)
	}
	return resp
}

func (f *markFixture) row(uuid string) types.Qso {
	f.t.Helper()
	q, err := f.srv.db.FetchQsoByUUIDIncludingDeletedWithContext(context.Background(), uuid)
	if err != nil {
		f.t.Fatalf("fetch %s: %v", uuid, err)
	}
	return q
}

func (f *markFixture) marked(uuid string) bool {
	return f.row(uuid).SmFwrdByEmailStatus == adif.YesString
}

func (f *markFixture) patch(uuid, body string) {
	f.t.Helper()
	if w := patchQso(f.t, f.srv, uuid, body); w.Code != http.StatusOK {
		f.t.Fatalf("patch %s %s: status = %d; body = %s", uuid, body, w.Code, w.Body.String())
	}
}

// bookkeep makes a bookkeeping-only write that bumps the row's revision: a
// concurrent mark of the row by another session email (its fields are ignored by
// the comparison). Used where a test needs one at every attempt.
func (f *markFixture) bookkeep(uuid string) {
	f.t.Helper()
	q := f.row(uuid)
	if _, err := f.srv.db.MarkSessionEmailedAtRevisionWithContext(context.Background(),
		[]sqlite.SessionEmailTarget{{ID: q.ID, Revision: q.Revision}}, "19990101"); err != nil {
		f.t.Fatalf("bookkeeping write: %v", err)
	}
}

func wantUUIDs(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// Case 1, the observed defect: the worker's own upload stamp lands between the
// compose and the mark. The row is still marked; its revision moved for the stamp
// and again for the mark; the stamp-sync enqueue carries it.
func TestSessionEmailMark_StampOnlyChangeIsMarked(t *testing.T) {
	f := newMarkFixture(t)
	id, uuid := f.qso("M0CMA", "0845")
	composedRev := f.row(uuid).Revision
	f.srv.sessionEmailAfterSend = func() { stampUploaded(t, f.srv.db, id) }

	resp := f.send(context.Background(), uuid)

	wantUUIDs(t, "emailed", resp.Emailed, []string{uuid})
	if resp.Date == "" {
		t.Error("date must be set when a mark is confirmed")
	}
	got := f.row(uuid)
	if got.SmFwrdByEmailStatus != adif.YesString || got.QrzComUploadStatus == "" {
		t.Fatalf("row: email %q, qrz stamp %q; want both stamps", got.SmFwrdByEmailStatus, got.QrzComUploadStatus)
	}
	if got.Revision < composedRev+2 {
		t.Errorf("revision %d, want ≥ %d (the stamp and the mark each bump it)", got.Revision, composedRev+2)
	}
	if len(f.synced) != 1 || !slices.Equal(f.synced[0], []int64{id}) {
		t.Errorf("stamp sync = %v, want [[%d]]", f.synced, id)
	}
}

// Cases 2–5: a real change to emitted contact content, or a deletion, leaves the
// row unmarked (in sent, not in emailed) so it stays re-sendable.
func TestSessionEmailMark_ContentChangeOrDeletionIsNotMarked(t *testing.T) {
	for name, mutate := range map[string]func(f *markFixture, uuid string){
		"name edit":          func(f *markFixture, uuid string) { f.patch(uuid, `{"name":"Bob"}`) },
		"enrichment country": func(f *markFixture, uuid string) { f.patch(uuid, `{"country":"Gabon"}`) },
		"qsl field":          func(f *markFixture, uuid string) { f.patch(uuid, `{"qsl_sent":"Y"}`) },
		"deletion": func(f *markFixture, uuid string) {
			if w := deleteQso(f.t, f.srv, uuid); w.Code != http.StatusNoContent {
				f.t.Fatalf("delete: %d %s", w.Code, w.Body.String())
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newMarkFixture(t)
			_, uuid := f.qso("M0CMA", "0845")
			f.srv.sessionEmailAfterSend = func() { mutate(f, uuid) }

			resp := f.send(context.Background(), uuid)

			wantUUIDs(t, "sent", resp.Sent, []string{uuid})
			wantUUIDs(t, "emailed", resp.Emailed, []string{})
			if resp.Date != "" {
				t.Errorf("date = %q, want empty when nothing is confirmed", resp.Date)
			}
			if f.marked(uuid) {
				t.Error("row marked although its content changed or it was deleted")
			}
			if len(f.synced) != 0 {
				t.Errorf("stamp sync = %v, want none", f.synced)
			}
		})
	}
}

// Case 6: each row of a mixed batch is judged on its own.
func TestSessionEmailMark_MixedBatch(t *testing.T) {
	f := newMarkFixture(t)
	_, unchanged := f.qso("M0CMA", "0845")
	stampID, stamped := f.qso("M0CMB", "0846")
	_, edited := f.qso("M0CMC", "0847")
	_, deleted := f.qso("M0CMD", "0848")
	f.srv.sessionEmailAfterSend = func() {
		stampUploaded(t, f.srv.db, stampID)
		f.patch(edited, `{"comment":"changed"}`)
		if w := deleteQso(t, f.srv, deleted); w.Code != http.StatusNoContent {
			t.Fatalf("delete: %d", w.Code)
		}
	}

	resp := f.send(context.Background(), unchanged, stamped, edited, deleted)

	wantUUIDs(t, "sent", resp.Sent, []string{unchanged, stamped, edited, deleted})
	wantUUIDs(t, "emailed", resp.Emailed, []string{unchanged, stamped})
	for uuid, want := range map[string]bool{unchanged: true, stamped: true, edited: false, deleted: false} {
		if f.marked(uuid) != want {
			t.Errorf("%s marked = %v, want %v", uuid, !want, want)
		}
	}
}

// Cases 7 and 8: a write between attempt 1's verification and its mark is a lost
// race; attempt 2 re-verifies — and marks only if the change was bookkeeping.
func TestSessionEmailMark_LostRaceIsReverified(t *testing.T) {
	for name, tc := range map[string]struct {
		write  func(f *markFixture, uuid string)
		marked bool
	}{
		"bookkeeping": {write: func(f *markFixture, uuid string) { f.bookkeep(uuid) }, marked: true},
		"content":     {write: func(f *markFixture, uuid string) { f.patch(uuid, `{"name":"Bob"}`) }, marked: false},
	} {
		t.Run(name, func(t *testing.T) {
			f := newMarkFixture(t)
			_, uuid := f.qso("M0CMA", "0845")
			var attempts []int
			f.srv.sessionEmailBeforeMark = func(attempt int) {
				attempts = append(attempts, attempt)
				if attempt == 1 {
					tc.write(f, uuid)
				}
			}

			resp := f.send(context.Background(), uuid)

			if tc.marked {
				wantUUIDs(t, "emailed", resp.Emailed, []string{uuid})
				if !slices.Equal(attempts, []int{1, 2}) {
					t.Errorf("attempts = %v, want [1 2]", attempts)
				}
			} else {
				wantUUIDs(t, "emailed", resp.Emailed, []string{})
				if !slices.Equal(attempts, []int{1}) {
					t.Errorf("attempts = %v, want [1]: the content change ends the row's candidacy", attempts)
				}
			}
			if f.marked(uuid) != tc.marked {
				t.Errorf("marked = %v, want %v", f.marked(uuid), tc.marked)
			}
		})
	}
}

// Case 9: three attempts in total, not three retries. A write before every mark
// leaves the row unmarked after the third.
func TestSessionEmailMark_AttemptsExhausted(t *testing.T) {
	f := newMarkFixture(t)
	_, uuid := f.qso("M0CMA", "0845")
	var attempts []int
	f.srv.sessionEmailBeforeMark = func(attempt int) {
		attempts = append(attempts, attempt)
		f.bookkeep(uuid)
	}

	resp := f.send(context.Background(), uuid)

	if !slices.Equal(attempts, []int{1, 2, 3}) {
		t.Fatalf("attempts = %v, want exactly [1 2 3]", attempts)
	}
	wantUUIDs(t, "emailed", resp.Emailed, []string{})
	// The row carries the concurrent writer's mark (19990101), never this send's.
	if got := f.row(uuid).SmFwrdByEmailDate; got != "19990101" {
		t.Errorf("mark date = %q, want only the concurrent writer's 19990101", got)
	}
}

// failingMarkFrom wraps the real mark so attempts >= from fail with err.
func failingMarkFrom(f *markFixture, attempt *int, from int, err error) {
	f.srv.sessionMark = func(ctx context.Context, targets []sqlite.SessionEmailTarget, date string) ([]int64, error) {
		if *attempt >= from {
			return nil, err
		}
		return f.srv.db.MarkSessionEmailedAtRevisionWithContext(ctx, targets, date)
	}
}

// Case 10: attempt 1 marks A and loses B; attempts 2 and 3 fail. The confirmed
// mark of A is not discarded: emailed = [A], one stamp date, and the stamp-sync
// enqueue holds exactly [A].
func TestSessionEmailMark_EarlierConfirmedCommitSurvivesLaterFailures(t *testing.T) {
	f := newMarkFixture(t)
	idA, a := f.qso("M0CMA", "0845")
	_, b := f.qso("M0CMB", "0846")
	attempt := 0
	f.srv.sessionEmailBeforeMark = func(n int) {
		attempt = n
		if n == 1 {
			f.bookkeep(b)
		}
	}
	failingMarkFrom(f, &attempt, 2, stderrs.New("injected marking failure"))

	resp := f.send(context.Background(), a, b)

	wantUUIDs(t, "emailed", resp.Emailed, []string{a})
	if attempt != 3 {
		t.Errorf("last attempt = %d, want 3 (failures before COMMIT allow another attempt)", attempt)
	}
	if resp.Date == "" || f.row(a).SmFwrdByEmailDate != resp.Date {
		t.Errorf("date %q, A's stored date %q: want one stamp date", resp.Date, f.row(a).SmFwrdByEmailDate)
	}
	if f.row(b).SmFwrdByEmailDate == resp.Date {
		t.Error("B carries this send's mark although no attempt confirmed it")
	}
	if len(f.synced) != 1 || !slices.Equal(f.synced[0], []int64{idA}) {
		t.Errorf("stamp sync = %v, want exactly [[%d]]", f.synced, idA)
	}
}

// Case 11: the request is cancelled after attempt 1 confirmed A. No further
// marking statement is issued; A stays marked and in the accumulated result. The
// enqueue is best-effort after a cancellation, so nothing is asserted about it.
func TestSessionEmailMark_CancellationKeepsConfirmedMarks(t *testing.T) {
	f := newMarkFixture(t)
	_, a := f.qso("M0CMA", "0845")
	_, b := f.qso("M0CMB", "0846")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	marks := 0
	f.srv.sessionMark = func(ctx context.Context, targets []sqlite.SessionEmailTarget, date string) ([]int64, error) {
		marks++
		return f.srv.db.MarkSessionEmailedAtRevisionWithContext(ctx, targets, date)
	}
	f.srv.sessionEmailBeforeMark = func(n int) {
		switch n {
		case 1:
			f.bookkeep(b)
		case 2:
			cancel()
		}
	}

	resp := f.send(ctx, a, b)

	if marks != 1 {
		t.Errorf("marking statements = %d, want 1: none may be issued after the cancellation", marks)
	}
	wantUUIDs(t, "emailed", resp.Emailed, []string{a})
	if !f.marked(a) {
		t.Error("A's confirmed mark was lost")
	}
}

// Case 12: an attempt whose transaction was rolled back allows another attempt,
// which confirms the rows.
func TestSessionEmailMark_RolledBackAttemptIsRetried(t *testing.T) {
	f := newMarkFixture(t)
	_, a := f.qso("M0CMA", "0845")
	calls := 0
	f.srv.sessionMark = func(ctx context.Context, targets []sqlite.SessionEmailTarget, date string) ([]int64, error) {
		calls++
		if calls == 1 {
			return nil, stderrs.New("injected statement failure (rolled back)")
		}
		return f.srv.db.MarkSessionEmailedAtRevisionWithContext(ctx, targets, date)
	}

	resp := f.send(context.Background(), a)

	wantUUIDs(t, "emailed", resp.Emailed, []string{a})
	if calls != 2 {
		t.Errorf("marking calls = %d, want 2", calls)
	}
}

// Case 12a (handler side): a failed COMMIT is uncertain. Whether it actually
// committed or not, the next attempt's read finds the row eligible (the mark
// changes only ignored fields), and only that attempt's successful commit
// confirms it. The driver behaviour itself is pinned in the sqlite package
// (TestSessionMark_DeferredFKCommitFailureIsUncertainAndRecoverable).
func TestSessionEmailMark_UncertainCommitIsResolvedByTheNextAttempt(t *testing.T) {
	for name, committedAnyway := range map[string]bool{"rolled back": false, "actually committed": true} {
		t.Run(name, func(t *testing.T) {
			f := newMarkFixture(t)
			_, a := f.qso("M0CMA", "0845")
			calls := 0
			f.srv.sessionMark = func(ctx context.Context, targets []sqlite.SessionEmailTarget, date string) ([]int64, error) {
				calls++
				if calls == 1 {
					if committedAnyway {
						if _, err := f.srv.db.MarkSessionEmailedAtRevisionWithContext(ctx, targets, date); err != nil {
							t.Fatalf("underlying mark: %v", err)
						}
					}
					return nil, fmt.Errorf("commit: %w", sqlite.ErrSessionMarkUncertain)
				}
				return f.srv.db.MarkSessionEmailedAtRevisionWithContext(ctx, targets, date)
			}

			resp := f.send(context.Background(), a)

			if calls != 2 {
				t.Fatalf("marking calls = %d, want 2", calls)
			}
			wantUUIDs(t, "emailed", resp.Emailed, []string{a})
			if !f.marked(a) {
				t.Error("row not marked after the confirming attempt")
			}
		})
	}
}

// A rollback that itself failed leaves the connection state unknown: the loop
// stops instead of compounding it.
func TestSessionEmailMark_RollbackFailureStopsTheLoop(t *testing.T) {
	f := newMarkFixture(t)
	_, a := f.qso("M0CMA", "0845")
	calls := 0
	f.srv.sessionMark = func(context.Context, []sqlite.SessionEmailTarget, string) ([]int64, error) {
		calls++
		return nil, fmt.Errorf("stamp: %w", sqlite.ErrSessionRollbackFailed)
	}

	resp := f.send(context.Background(), a)

	if calls != 1 {
		t.Errorf("marking calls = %d, want 1", calls)
	}
	wantUUIDs(t, "emailed", resp.Emailed, []string{})
}

// Case 12b: the admission budget expires before attempt 2. Attempt 1's confirmed
// mark is reported; no further attempt is admitted. The budget is ended exactly
// after attempt 1's confirmed commit (no clock in the precondition): the marking
// context is replaced by one whose expiry the test controls, ended with the cause
// a passed deadline carries.
func TestSessionEmailMark_BudgetStopsFurtherAttempts(t *testing.T) {
	f := newMarkFixture(t)
	_, a := f.qso("M0CMA", "0845")
	_, b := f.qso("M0CMB", "0846")

	var expire context.CancelCauseFunc
	var gotDeadline time.Time
	f.srv.sessionMarkContext = func(parent context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
		gotDeadline = deadline
		ctx, cancel := context.WithCancelCause(parent)
		expire = cancel
		return ctx, func() { cancel(context.Canceled) }
	}
	marks := 0
	f.srv.sessionEmailBeforeMark = func(n int) {
		if n == 1 {
			f.bookkeep(b)
		}
	}
	f.srv.sessionMark = func(ctx context.Context, targets []sqlite.SessionEmailTarget, date string) ([]int64, error) {
		marks++
		got, err := f.srv.db.MarkSessionEmailedAtRevisionWithContext(ctx, targets, date)
		if err == nil && len(got) > 0 {
			expire(context.DeadlineExceeded) // the budget ends after this confirmed commit
		}
		return got, err
	}

	resp := f.send(context.Background(), a, b)

	if marks != 1 {
		t.Errorf("marking calls = %d, want 1: no attempt after the budget", marks)
	}
	wantUUIDs(t, "emailed", resp.Emailed, []string{a})
	if !f.marked(a) || f.row(b).SmFwrdByEmailDate == resp.Date {
		t.Error("want A marked by this send and B not")
	}
	// The production budget is the write deadline less the reserve.
	if until := time.Until(gotDeadline); until < 10*time.Second || until > 5*time.Second+sessionEmailResponseMargin {
		t.Errorf("marking deadline in %v; want write deadline (mailer timeout + margin) − %v", until, sessionEmailResponseReserve)
	}
}

// markStopReason tells the budget's expiry from the client's cancellation by the
// cause, for both a real deadline and a cause-cancelled context.
func TestMarkStopReason(t *testing.T) {
	if got := markStopReason(context.Background()); got != "" {
		t.Errorf("live context: %q, want empty", got)
	}
	past, cancelPast := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelPast()
	if got := markStopReason(past); got != "budget" {
		t.Errorf("expired deadline: %q, want budget", got)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if got := markStopReason(cancelled); got != "cancelled" {
		t.Errorf("client cancellation: %q, want cancelled", got)
	}
	caused, cancelCause := context.WithCancelCause(context.Background())
	cancelCause(context.DeadlineExceeded)
	if got := markStopReason(caused); got != "budget" {
		t.Errorf("ended with the deadline cause: %q, want budget", got)
	}
}

// A verification read whose rollback failed leaves the connection state unknown:
// the loop stops, exactly as for the marking transaction, and issues no mark.
func TestSessionEmailMark_VerificationRollbackFailureStopsTheLoop(t *testing.T) {
	f := newMarkFixture(t)
	_, a := f.qso("M0CMA", "0845")
	reads, marks := 0, 0
	f.srv.sessionVerifyRead = func(context.Context, []int64) (map[int64]types.Qso, error) {
		reads++
		return nil, fmt.Errorf("verification read: %w", sqlite.ErrSessionRollbackFailed)
	}
	f.srv.sessionMark = func(context.Context, []sqlite.SessionEmailTarget, string) ([]int64, error) {
		marks++
		return nil, nil
	}

	resp := f.send(context.Background(), a)

	if reads != 1 || marks != 0 {
		t.Errorf("reads = %d, marks = %d; want 1 and 0", reads, marks)
	}
	wantUUIDs(t, "emailed", resp.Emailed, []string{})
}

// An ordinary verification-read error allows another attempt.
func TestSessionEmailMark_VerificationReadErrorIsRetried(t *testing.T) {
	f := newMarkFixture(t)
	_, a := f.qso("M0CMA", "0845")
	reads := 0
	f.srv.sessionVerifyRead = func(ctx context.Context, ids []int64) (map[int64]types.Qso, error) {
		reads++
		if reads == 1 {
			return nil, stderrs.New("injected read failure")
		}
		return f.srv.db.FetchCurrentQsosByIDWithContext(ctx, ids)
	}

	resp := f.send(context.Background(), a)

	if reads != 2 {
		t.Errorf("reads = %d, want 2", reads)
	}
	wantUUIDs(t, "emailed", resp.Emailed, []string{a})
}

// Case 13: an edit reverted before verification leaves the content equal to what
// was sent — the mark is truthful, so the row is marked.
func TestSessionEmailMark_EditThenRevertIsMarked(t *testing.T) {
	f := newMarkFixture(t)
	_, a := f.qso("M0CMA", "0845")
	f.srv.sessionEmailAfterSend = func() {
		f.patch(a, `{"comment":"temporary"}`)
		f.patch(a, `{"comment":""}`)
	}

	resp := f.send(context.Background(), a)

	wantUUIDs(t, "emailed", resp.Emailed, []string{a})
}

// Case 14: re-sending an already-emailed row marks it again: the email-mark fields
// are ignored by the comparison.
func TestSessionEmailMark_ResendOfEmailedRow(t *testing.T) {
	f := newMarkFixture(t)
	_, a := f.qso("M0CMA", "0845")
	wantUUIDs(t, "first emailed", f.send(context.Background(), a).Emailed, []string{a})
	f.srv.sessionEmailAfterSend = func() { f.bookkeep(a) }

	resp := f.send(context.Background(), a)

	wantUUIDs(t, "second emailed", resp.Emailed, []string{a})
}
