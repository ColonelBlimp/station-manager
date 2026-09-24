package recorder

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/stationevents"
)

// The conversion table: one row shape per fact (W-0020 rulings 5 and 7). It is
// TOTAL over the sealed fact set — rowFor's switch has a case for every type
// stationevents seals — and every detail is a fixed set of typed fields per
// kind (AC7): identifiers our own code minted (alarm codes, causes, rungs) are
// re-checked against an identifier grammar on the way in, and the one piece of
// decoded input, partner_call, is normalised and bounded. Nothing here ever
// carries free text from a rig, a provider or the air.

// Severities per kind: raised alarms error; clears and recoveries info;
// automatic disarms and abnormal terminations warn (ruling 7).
var severityByKind = map[string]string{
	stationevents.KindArchiveActivated:        stationevents.SeverityInfo,
	stationevents.KindArchiveActivationFailed: stationevents.SeverityError,
	stationevents.KindTxAlarmRaised:           stationevents.SeverityError,
	stationevents.KindTxAlarmCleared:          stationevents.SeverityInfo,
	stationevents.KindDriveAlarmRaised:        stationevents.SeverityError,
	stationevents.KindDriveAlarmCleared:       stationevents.SeverityInfo,
	stationevents.KindTxDisarmed:              stationevents.SeverityWarn,
	stationevents.KindSessionTerminated:       stationevents.SeverityWarn,
}

// identMaxLen bounds every identifier field (code, cause, rung).
const identMaxLen = 32

// categoryByKind is the vocabulary's pair table inverted, so a row's category
// is looked up rather than assumed: the archive kinds are notifications, the
// rest alarms.
var categoryByKind = func() map[string]string {
	out := map[string]string{}
	for cat, kinds := range stationevents.KindsByCategory() {
		for _, k := range kinds {
			out[k] = cat
		}
	}
	return out
}()

// archiveID admits only the UUID shape our own catalogue mints (36 chars:
// lower-case hex and '-'); anything else is replaced, never truncated.
func archiveID(s string) string {
	if len(s) != 36 {
		return invalidIdent
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || c == '-'
		if !ok {
			return invalidIdent
		}
	}
	return s
}

// archiveLabel bounds operator text (the catalogue label): trimmed, printable
// runes only, at most ArchiveLabelMaxLen runes.
func archiveLabel(raw string) string {
	var b strings.Builder
	n := 0
	for _, r := range strings.TrimSpace(raw) {
		if r < 0x20 || r == 0x7f {
			continue
		}
		if n == stationevents.ArchiveLabelMaxLen {
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

// invalidIdent replaces an identifier that failed the grammar. It is a
// constant so a row can never carry the offending text — the log line (not the
// row) is where a surprise identifier would be investigated.
const invalidIdent = "invalid"

// ident admits only what our own constants look like: lower-case ASCII
// letters, digits, '_' and '.', 1..identMaxLen long. Anything else — including
// empty — is replaced, never truncated into something that reads as valid.
func ident(s string) string {
	if s == "" || len(s) > identMaxLen {
		return invalidIdent
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '.'
		if !ok {
			return invalidIdent
		}
	}
	return s
}

// partnerCall normalises decoded input (ruling 5): trimmed, upper-cased,
// bounded to stationevents.PartnerCallMaxLen runes. Still untrusted, so
// anything outside printable ASCII is dropped rather than stored.
func partnerCall(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	var b strings.Builder
	for _, r := range s {
		if r < '!' || r > '~' {
			continue
		}
		if b.Len() >= stationevents.PartnerCallMaxLen {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

type archiveDetail struct {
	ArchiveID string `json:"archive_id"`
	Label     string `json:"label"`
}

type archiveFailedDetail struct {
	ArchiveID string `json:"archive_id"`
	Label     string `json:"label"`
	Code      string `json:"code"`
}

type txAlarmDetail struct {
	Code string `json:"code"`
}

type txAlarmClearedDetail struct {
	Code string `json:"code"`
	// ActiveMs is how long the alarm stood, from the raise stamp the bridge
	// retained to the clear stamp; absent (nil) when the bridge had no raise
	// stamp, which a row must say by omission rather than by a made-up zero.
	ActiveMs *int64 `json:"active_ms,omitempty"`
}

type disarmDetail struct {
	Cause string `json:"cause"`
}

type terminatedDetail struct {
	Cause       string `json:"cause"`
	PartnerCall string `json:"partner_call"`
	Rung        string `json:"rung"`
}

// rowFor converts one fact into the row the store takes. build is the daemon's
// canonical build string (ADR 0076 §7). It cannot fail: every fact type has a
// case, and the detail structs marshal by construction.
func rowFor(f stationevents.Fact, build string) sqlite.OperatorEventInput {
	var (
		kind   string
		at     time.Time
		detail any
	)
	switch v := f.(type) {
	case stationevents.ArchiveActivated:
		kind, at, detail = stationevents.KindArchiveActivated, v.At, archiveDetail{ArchiveID: archiveID(v.ArchiveID), Label: archiveLabel(v.Label)}
	case stationevents.ArchiveActivationFailed:
		kind, at, detail = stationevents.KindArchiveActivationFailed, v.At, archiveFailedDetail{
			ArchiveID: archiveID(v.ArchiveID), Label: archiveLabel(v.Label), Code: ident(v.Code),
		}
	case stationevents.TxAlarmRaised:
		kind, at, detail = stationevents.KindTxAlarmRaised, v.At, txAlarmDetail{Code: ident(v.Code)}
	case stationevents.TxAlarmCleared:
		d := txAlarmClearedDetail{Code: ident(v.Code)}
		if !v.RaisedAt.IsZero() && !v.At.Before(v.RaisedAt) {
			ms := v.At.Sub(v.RaisedAt).Milliseconds()
			d.ActiveMs = &ms
		}
		kind, at, detail = stationevents.KindTxAlarmCleared, v.At, d
	case stationevents.DriveAlarmRaised:
		kind, at, detail = stationevents.KindDriveAlarmRaised, v.At, txAlarmDetail{Code: ident(v.Code)}
	case stationevents.DriveAlarmRecovered:
		kind, at, detail = stationevents.KindDriveAlarmCleared, v.At, txAlarmDetail{Code: ident(v.Code)}
	case stationevents.TxDisarmed:
		kind, at, detail = stationevents.KindTxDisarmed, v.At, disarmDetail{Cause: ident(v.Cause)}
	case stationevents.ExchangeTerminated:
		kind, at, detail = stationevents.KindSessionTerminated, v.At, terminatedDetail{
			Cause: ident(v.Cause), PartnerCall: partnerCall(v.PartnerCall), Rung: ident(v.Rung),
		}
	default:
		// Unreachable for a sealed set; a stationevents fact added without a
		// case here is caught by the conversion test that enumerates the set.
		panic("stationevents/recorder: no conversion for fact type")
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		panic("stationevents/recorder: detail marshal: " + err.Error()) // fixed structs: cannot happen
	}
	return sqlite.OperatorEventInput{
		Category:   categoryByKind[kind],
		Kind:       kind,
		Severity:   severityByKind[kind],
		Build:      build,
		Detail:     raw,
		OccurredAt: at,
	}
}

// kindOf names a fact's kind for log lines without building the row.
func kindOf(f stationevents.Fact) string {
	switch f.(type) {
	case stationevents.ArchiveActivated:
		return stationevents.KindArchiveActivated
	case stationevents.ArchiveActivationFailed:
		return stationevents.KindArchiveActivationFailed
	case stationevents.TxAlarmRaised:
		return stationevents.KindTxAlarmRaised
	case stationevents.TxAlarmCleared:
		return stationevents.KindTxAlarmCleared
	case stationevents.DriveAlarmRaised:
		return stationevents.KindDriveAlarmRaised
	case stationevents.DriveAlarmRecovered:
		return stationevents.KindDriveAlarmCleared
	case stationevents.TxDisarmed:
		return stationevents.KindTxDisarmed
	case stationevents.ExchangeTerminated:
		return stationevents.KindSessionTerminated
	}
	return invalidIdent
}
