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
// Group B reuses `maxRepeats` and the existing `s.contact.repeats` counter, which resets
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

// finalRungDoneLocked builds the completion for a GROUP A final rung: it records
// the QSO whether or not the closing message reached the air, then ends the
// session. Only the log line distinguishes the two outcomes. What it returns is
// the completion in its two steps (earnedClaim); a transmission is given the two
// together (earnedClaim.complete).
//
// The caller holds s.mu (so the publish/complete hooks and the session generation
// are read consistently) and passes clear, which releases that ladder's own
// exchange field. clear runs under s.mu inside the callback and must not unlock.
//
// sentMsg / unsentMsg are the full log lines for the keyed and un-keyed cases;
// each ladder keeps its own wording so the log stays greppable per path.
// retireSessionLocked performs the ONE session-identity transition every completion
// that ends a session must make. Caller holds s.mu; clear releases that ladder's own
// exchange field and must not unlock.
//
// It exists because the four ending completions each did their own version and had
// already drifted: only the Group A path retired the generation and consumed the
// staged reason, so the other three left every stale-callback guard in the package
// unable to tell a finished session from a live one, and silently dropped the dial
// guard's explanation when they won the race with a teardown (package review of
// internal/ft8, finding 2). Call-CQ is deliberately NOT a caller: its completion
// RESUMES CQ rather than ending, which is a different transition.
//
// The publish happens INSIDE the lock so a replacement Start* cannot commit and
// publish ACTIVE first only to be overwritten by this idle (see publishCurrent).
func (s *Sequencer) retireSessionLocked(clear func()) {
	s.sessionGen++
	clear()
	s.mode = seqIdle
	// Per-contact operator flags die with the session, in ONE assignment — what
	// this function exists to be. It used to name each flag, with a comment hoping
	// "a caller should never need to remember a fifth thing to reset by hand"; the
	// zero value now enforces that rather than asking for it.
	s.contact = contactFlags{}
	reason := s.pendingEndReason
	s.pendingEndReason = ""
	s.publish(s.terminalStatusLocked(reason))
}

func (s *Sequencer) finalRungDoneLocked(c CompletedQso, clear func(), sentMsg, unsentMsg string) earnedClaim {
	gen := s.sessionGen
	prepareComplete, onComplete := s.prepareComplete, s.onComplete
	return func(ok bool, endReason string) (handOver func()) {
		// A copy of its own for THIS invocation. The completion can be invoked by
		// more than one caller at once (a stop, the slot whose rung was refused, a
		// transmission's goroutine) and each stamps before the guard below decides
		// which of them logs. With one contact shared between them the caller that
		// logs handed over whatever the other had stamped meanwhile (codex-204).
		c := c
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
			return nil
		}
		// The caller's reason for the session's end rides the terminal frame the
		// retirement publishes. Set in the SAME lock hold that consumes it, so no
		// other teardown can pick it up; a reason already staged wins, as it does
		// in every abandon.
		if endReason != "" && s.pendingEndReason == "" {
			s.pendingEndReason = endReason
		}
		// Retire the session generation on completion, exactly as Abandon does.
		// Group A completes on EITHER outcome, so unlike the old success-only path
		// this callback can be reached twice for one contact — once from the
		// transmit error branch (the goroutine never started) and once from a
		// still-running earlier transmission of the same rung. Without this bump the
		// second would log the QSO again: a duplicate row AND a duplicate upload to
		// every forwarder. The gen check above now refuses it.
		s.retireSessionLocked(clear)
		s.mu.Unlock()
		// Everything from here can be held up by someone else, a log writer or
		// whoever takes the contact, so it is the hand-over's and not the claim's.
		return func() {
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
}

// earnedClaim is a Group A contact's completion, in its two steps (codex-206).
//
// Calling it is the CLAIM: it stamps the contact and, if the contact's session is
// still the live one, retires it. The generation moves, so a rung of that session
// still queued can no longer be admitted, and the terminal frame is published
// (carrying endReason when the caller gives one and none was staged). A claim does
// nothing that someone else can hold up: it takes txMu for the stamp and the
// sequencer's lock, and publishes to the hub. The hub does not wait on a
// subscriber, but it does WRITE A RECORD for one it evicts, inside the publish: a
// caller with a transmission to cut holds those records until the cut is made
// (hub.holdEvictionReports, in the Service's Abandon; codex-208). Itself the claim
// writes no log line and calls no consumer. It returns the HAND-OVER it then owes: the
// completion's log line, and the call that gives the contact to the logging sink.
// A claim that finds its session already over returns nil, and nothing is owed:
// that is the exactly-once guard.
//
// The two are apart because a log writer, or whoever takes the contact, can hold
// the hand-over up, and stopping RF must never wait on that (codex-206, codex-207).
// A caller with a transmission to cut claims, cuts, and only then hands over
// (abandonNamedThen). A caller with nothing to cut runs both at once (complete).
// Add nothing to the claim that writes, waits or calls out.
type earnedClaim func(ok bool, endReason string) (handOver func())

// complete is the claim and its hand-over together: the completion callback a
// transmission is given.
func (c earnedClaim) complete(ok bool) {
	if handOver := c(ok, ""); handOver != nil {
		handOver()
	}
}

// earnLocked keeps the completion of a Group A contact from the slot that takes the
// partner's roger in, and returns it. Caller holds s.mu. build makes the completion
// (finalRungDoneLocked with the ladder's own snapshot and wording) and runs once per
// contact: the handler calls this as soon as the ladder is on its closing rung,
// BEFORE any return that defers that rung (decoded too late into our slot, or our
// slot already carried a rung), and again where the rung is transmitted, which gets
// the same completion. Earning is the protocol's fact, not the courtesy's: a contact
// whose courtesy has not yet been attempted is owed to the log all the same.
//
// Only the three Group A ladders call it. A Group B final rung earns nothing until
// it has been on the air.
//
// That is also what keeps a kept completion with its own contact. Each of the three
// zeroes contactFlags and advances sessionGen where its session starts (startAnswer,
// StartQsoT4, StartWorkCallerFd) and where it ends (retireSessionLocked,
// abandonLocked). Call-CQ begins a contact inside a run without doing either
// (commitCqContactLocked), so a ladder that begins its contacts that way must not
// earn through here (codex-205).
func (s *Sequencer) earnLocked(build func() earnedClaim) func(ok bool) {
	if s.contact.earned == nil {
		s.contact.earned = build()
	}
	return s.contact.earned.complete
}

// claimEarnedLocked claims a Group A contact that is earned and not yet completed,
// for an abandon about to end its session (invariant 1: a rogered contact is logged
// whatever stops it). The caller holds s.mu.
//
//   - Nothing earned: it returns (nil, false) and the lock is STILL HELD. The caller
//     tears the session down in that same hold, so no slot can take a roger in
//     between the look and the teardown.
//   - A contact earned: it RELEASES the lock (the claim takes txMu and then s.mu,
//     in that order), claims, and returns the hand-over owed, or nil if another
//     caller claimed first, and true.
//
// The look and the teardown used to be two holds of the lock. A slot that took the
// partner's roger in between them earned a contact the abandon then cleared,
// unlogged (the clean-room reviews of d49b0d4e and 8f6f8cb4).
//
// Where it finds something. The Service's disarm abandons AFTER it has waited for
// the transmission in flight, so a closing rung that reached the air has already
// completed through its own callback; what is found there is a closing rung that
// never reached the air: deferred to a later slot, or decided and never admitted.
// The operator's Abandon abandons FIRST and cuts the transmission short afterwards
// (the order the rung's commit gate relies on), so it may find the closing rung on
// the air: the contact is claimed here, and the cut transmission's own callback,
// arriving later, is refused.
//
// Either way it is logged as not transmitted. Exactly once is the claim's own
// generation guard: whichever of this call, the slot's own failure branch and a
// transmission's goroutine claims first retires the session, and the others get nil.
func (s *Sequencer) claimEarnedLocked(endReason string) (handOver func(), released bool) {
	claim := s.contact.earned
	if claim == nil {
		return nil, false
	}
	s.mu.Unlock()
	return claim(false, endReason), true
}

// endEarnedLocked is claimEarnedLocked for an abandon scoped to ONE generation, which
// has just checked, under s.mu, that its generation is the live one. The caller holds
// s.mu. With nothing earned it returns false and the lock is still held. With a
// contact earned it RELEASES the lock, claims and hands over, and returns true: that
// generation's session is over and the caller has nothing left to abandon.
//
// The kept claim read here belongs to the generation the caller checked: both are
// read in one lock hold, and contactFlags die with their session. The claim guards
// itself after the lock is let go, so a session that replaces this one in the gap
// is not touched. A claim BEFORE the generation check would let a stale rung
// complete the replacement's contact (codex-206).
//
// Why it is needed: a rung is accepted a slot before it is keyed, and its refusal
// can arrive after a later slot of the SAME session has taken the partner's roger
// in. That earlier rung carries no completion of its own, so its scoped abandon
// was the only thing that ran, and it discarded the contact.
func (s *Sequencer) endEarnedLocked(endReason string) (ended bool) {
	handOver, released := s.claimEarnedLocked(endReason)
	if !released {
		return false
	}
	if handOver != nil {
		handOver()
	}
	return true
}
