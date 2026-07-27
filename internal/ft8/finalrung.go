package ft8

// Final-rung policy — what happens when the LAST transmission of a contact does
// not make it onto the air.
//
// Every ladder ends with a rung that closes the contact, and all of them share a
// rule decided earlier: the QSO is recorded only after that rung truly transmits,
// so SM never logs a contact it did not finish putting on the air. A rung that
// fails to transmit therefore leaves the exchange in place for the next slot to
// retry. That retry had no bound, because the repeat cap and the operator's
// skip-if-silent off-ramp both sit behind `if !confirming` — they exist to give up
// on a partner who is not answering, and at the final rung nobody is being waited
// for. The result was a contact that could re-key the rig every cycle forever.
//
// Bounding it needs two different answers, because "the partner is finished" is
// true for only some of the ladders. Which rung is ours inverts by role, and
// Field Day inverts again relative to its standard counterpart:
//
// GROUP A — the partner has already rogered; our closing message is a courtesy.
//
//	answer a CQ (standard)     TX "73"    after receiving their RRR/RR73
//	answer a CQ (type-4)       TX "73"    after receiving their RR73
//	work a caller (Field Day)  TX "RR73"  after receiving their RR73
//
// They hold both halves of the exchange and their sequence is over; our message
// only tells them to stop repeating. Dropping the contact here would leave the
// other operator holding a QSO we have no record of — a one-sided log that can
// never be confirmed. So a Group A final rung is SEND ONCE: attempt it, then
// complete the contact and log it whether or not it keyed. No retry.
//
// GROUP B — the partner is still waiting on us; our message is what completes
// THEIR sequence.
//
//	work a caller (standard)   TX "RR73"  after receiving their R+report
//	Call CQ                    TX "RR73"  after receiving their R+report
//	answer a CQ (Field Day)    TX "RR73"  after receiving their R+class/section
//	work a caller (type-4)     TX "RR73"  after their bare-call answer
//
// Until it arrives they keep repeating and eventually time out, so nothing was
// completed on either side. Retrying is a real attempt to finish a real contact —
// but bounded, and at the cap the contact is dropped WITHOUT logging, because
// recording it would invent a QSO the other station does not hold.
//
// Group B reuses `maxRepeats` and the existing `s.repeats` counter, which resets
// on every advance and so gives the final rung its own budget of attempts. That
// counter only advances at the final rung when the previous attempt FAILED — a
// successful one completes the contact — so it counts transmit failures, not
// unanswered calls.

// publishCurrent pushes the sequencer's state as it is NOW, rather than a snapshot
// taken earlier in the slot handler.
//
// Every handler snapshots its status under s.mu, releases the lock, transmits, then
// publishes. But `transmit` launches its goroutine BEFORE returning, so the
// completion callback can run — clearing the contact, resuming CQ, going idle, even
// letting a replacement session start — before the handler reaches its own publish.
// Publishing the pre-transmit snapshot then overwrites the newer state, stranding
// subscribers on a finished QSO shown as live (codex 3c1ee047 P1, a301d350 P2).
//
// Re-reading is deliberately preferred over validating the snapshot against a
// version: only SOME completion paths retire the session generation (Group A does,
// via finalRungDoneLocked; the Group B callbacks clear a contact and carry the
// session on), so a generation check silently misses them — which is exactly how the
// first attempt at this guard failed. Reading the truth at publish time cannot miss
// a case, and needs no bump site to be kept in sync.
//
// Publishing under s.mu matches the completion paths, which publish while the lock
// still excludes a replacement Start*.
func (s *Sequencer) publishCurrent() {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Nothing to refresh once the session is over: its TERMINAL frame has already
	// been published and is the truth. Re-publishing a bare idle status over it
	// would strip the end_reason the hub is caching, so a reconnecting client would
	// be told the session ended but never why — the explanation exists only in that
	// one frame (codex P2 on f1a8836d).
	//
	// This covers every post-transmit publish in the package, not just the caller
	// that surfaced it: transmit() returns as soon as startTransmission launches its
	// goroutine, so an asynchronous refusal can end the session before ANY of them
	// reach here.
	if s.mode == seqIdle {
		return
	}
	s.publish(s.statusLocked())
}

// finalRungDoneLocked builds the completion callback for a GROUP A final rung: it
// records the QSO whether or not the closing message reached the air, then ends
// the session. Only the log line distinguishes the two outcomes.
//
// The caller holds s.mu (so the publish/complete hooks and the session generation
// are read consistently) and passes clear, which releases that ladder's own
// exchange field. clear runs under s.mu inside the callback and must not unlock.
//
// sentMsg / unsentMsg are the full log lines for the keyed and un-keyed cases;
// each ladder keeps its own wording so the log stays greppable per path.
func (s *Sequencer) finalRungDoneLocked(c CompletedQso, clear func(), sentMsg, unsentMsg string) func(bool) {
	gen := s.sessionGen
	prepareComplete, onComplete, publish := s.prepareComplete, s.onComplete, s.publish
	return func(ok bool) {
		// Stamp unconditionally. The stamp carries the operator's antenna
		// selection onto the QSO, and a Group A contact logs even when the
		// courtesy did not key — skipping the stamp there would silently
		// downgrade AntPath to the default.
		if prepareComplete != nil {
			prepareComplete(&c)
		}
		s.mu.Lock()
		if s.sessionGen != gen { // superseded (abandon/disarm) — stale callback
			s.mu.Unlock()
			return
		}
		// Retire the session generation on completion, exactly as Abandon does.
		// Group A completes on EITHER outcome, so unlike the old success-only path
		// this callback can be reached twice for one contact — once from the
		// transmit error branch (the goroutine never started) and once from a
		// still-running earlier transmission of the same rung. Without this bump the
		// second would log the QSO again: a duplicate row AND a duplicate upload to
		// every forwarder. The gen check above now refuses it.
		s.sessionGen++
		clear()
		s.mode = seqIdle
		s.repeats = 0
		// Carry any staged end-reason. The dial guard waits for an in-flight
		// completion before abandoning — so that a rogered contact is not lost — but
		// that lets THIS callback retire the session, and the Abandon behind it then
		// finds an idle sequencer and publishes nothing. Without consuming the
		// reason here the contact is preserved at the cost of the explanation: PTT
		// stopped, TX disarmed, and nothing on screen saying why (codex P2 on
		// 7c2e66ad).
		reason := s.pendingEndReason
		s.pendingEndReason = ""
		// Publish the terminal state while the state lock still excludes a
		// replacement Start*. Otherwise that start can publish active first and
		// this delayed completion can overwrite it with stale idle.
		publish(QsoStatus{Active: false, EndReason: reason})
		s.mu.Unlock()
		if ok {
			s.log.InfoWith().Str("their_call", c.TheirCall).Msg(sentMsg)
		} else {
			s.log.WarnWith().Str("their_call", c.TheirCall).Msg(unsentMsg)
		}
		if onComplete != nil {
			onComplete(c)
		}
	}
}
