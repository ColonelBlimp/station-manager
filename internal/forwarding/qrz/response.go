package qrz

import (
	"net/url"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
)

// QRZ RESULT values. The documented per-action sets are:
//
//	INSERT: OK, FAIL, REPLACE
//	DELETE: OK, FAIL, PARTIAL
//
// AUTH is a global response (can accompany any ACTION) and always
// indicates api_key rejection. Any RESULT outside the documented
// per-action set is treated as a malformed response — Terminal.
const (
	resultOK      = "OK"
	resultFail    = "FAIL"
	resultReplace = "REPLACE"
	resultPartial = "PARTIAL"
	resultAuth    = "AUTH"
)

// response is the parsed QRZ Logbook API response body. Only the
// fields that drive classification are kept as typed struct members;
// anything else QRZ returns is ignored (the classifier matrix in
// classifyResponse exhaustively handles every documented RESULT
// value, so there's nothing to consume a generic-overflow map for).
type response struct {
	Result string
	Reason string
	LogID  string
	Count  string
}

// parseResponse parses a QRZ Logbook API response body. The format
// is application/x-www-form-urlencoded — key=value pairs separated by
// &, with URL-encoded values. An empty body or a body missing the
// RESULT field is an error: a real QRZ call always returns at least
// RESULT=..., even for malformed requests. Any other parsing error is
// also an error. The returned response struct may have empty fields if the
// corresponding keys are missing from the body; that's not an error
// because some RESULT types don't include all fields.
func parseResponse(body []byte) (response, error) {
	const op errors.Op = "qrz.parseResponse"

	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return response{}, errors.New(op).WithMsg("empty response body")
	}

	vals, err := url.ParseQuery(trimmed)
	if err != nil {
		return response{}, errors.New(op).WithErr(err).WithMsg("parse response body")
	}

	first := func(key string) string {
		v := vals[key]
		if len(v) == 0 {
			return ""
		}
		return v[0]
	}

	if _, ok := vals["RESULT"]; !ok {
		return response{}, errors.New(op).WithMsgf(
			"response missing RESULT field (got %d fields)", len(vals),
		)
	}

	return response{
		Result: first("RESULT"),
		Reason: first("REASON"),
		LogID:  first("LOGID"),
		Count:  first("COUNT"),
	}, nil
}

// classifyResponse maps a parsed QRZ response for the given action
// into a forwarding.Result. See the table in docs/v2-design/forwarding.md
// for the full decision matrix.
//
// Shape of the mapping (by RESULT):
//
//	AUTH                            → Terminal (global, api_key rejected)
//	OK, insert                      → Success, LOGID required
//	OK, update                      → Success, LOGID optional
//	OK, delete                      → Success
//	REPLACE, insert/update          → Success (LOGID)
//	REPLACE, delete                 → Terminal (undocumented for delete)
//	FAIL, delete                    → Success (idempotent — row gone upstream)
//	FAIL, insert/update             → Terminal
//	PARTIAL, delete                 → Terminal (single-LOGID — shouldn't occur)
//	PARTIAL, insert/update          → Terminal (undocumented for insert)
//	anything else                   → Terminal
func classifyResponse(act forwarding.Action, resp response) forwarding.Result {
	const op errors.Op = "qrz.classifyResponse"

	// AUTH short-circuits the per-action matrix.
	if resp.Result == resultAuth {
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err: errors.New(op).WithMsgf(
				"QRZ authentication rejected: %s", reasonOrDefault(resp.Reason, "no reason given"),
			),
		}
	}

	switch act {
	case action.Insert:
		return classifyInsert(resp)
	case action.Update:
		return classifyUpdate(resp)
	case action.Delete:
		return classifyDelete(resp)
	default:
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     errors.New(op).WithMsgf("unknown action %q", act),
		}
	}
}

// classifyInsert handles INSERT without OPTION=REPLACE. Documented
// RESULT set: OK, FAIL, REPLACE.
func classifyInsert(resp response) forwarding.Result {
	const op errors.Op = "qrz.classifyInsert"

	switch resp.Result {
	case resultOK:
		if resp.LogID == "" {
			return forwarding.Result{
				Outcome: forwarding.OutcomeTerminal,
				Err:     errors.New(op).WithMsg("RESULT=OK on insert without LOGID"),
			}
		}
		return forwarding.Result{
			Outcome:    forwarding.OutcomeSuccess,
			UpstreamID: resp.LogID,
		}

	case resultReplace:
		// Undocumented without OPTION=REPLACE but the end state
		// (record present upstream) matches intent. Accept if LOGID.
		if resp.LogID == "" {
			return forwarding.Result{
				Outcome: forwarding.OutcomeTerminal,
				Err:     errors.New(op).WithMsg("RESULT=REPLACE on insert without LOGID"),
			}
		}
		return forwarding.Result{
			Outcome:    forwarding.OutcomeSuccess,
			UpstreamID: resp.LogID,
		}

	case resultFail:
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err: errors.New(op).WithMsgf(
				"QRZ rejected insert: %s", reasonOrDefault(resp.Reason, "no reason given"),
			),
		}

	default:
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err: errors.New(op).WithMsgf(
				"unexpected RESULT=%q on insert (reason=%q)", resp.Result, resp.Reason,
			),
		}
	}
}

// classifyUpdate handles INSERT+OPTION=REPLACE. Documented RESULT
// set is the same as insert (OK, FAIL, REPLACE); the semantic
// difference is just that REPLACE is the expected happy path and
// OK indicates the record was newly inserted (didn't exist before).
func classifyUpdate(resp response) forwarding.Result {
	const op errors.Op = "qrz.classifyUpdate"

	switch resp.Result {
	case resultOK:
		// Updated record didn't exist — QRZ inserted it. That's a
		// logical success from the operator's perspective (the
		// record now exists upstream with our data). Require the LOGID
		// just like the insert path + the REPLACE branch below: marking
		// an update "uploaded" with no upstream id leaves a later DELETE
		// unable to identify the remote record, so it would settle as
		// forward.failed — better to fail the update terminally now with
		// a clear reason (review 2026-06-19 M2).
		if resp.LogID == "" {
			return forwarding.Result{
				Outcome: forwarding.OutcomeTerminal,
				Err:     errors.New(op).WithMsg("RESULT=OK on update without LOGID"),
			}
		}
		return forwarding.Result{
			Outcome:    forwarding.OutcomeSuccess,
			UpstreamID: resp.LogID,
		}

	case resultReplace:
		if resp.LogID == "" {
			return forwarding.Result{
				Outcome: forwarding.OutcomeTerminal,
				Err:     errors.New(op).WithMsg("RESULT=REPLACE on update without LOGID"),
			}
		}
		return forwarding.Result{
			Outcome:    forwarding.OutcomeSuccess,
			UpstreamID: resp.LogID,
		}

	case resultFail:
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err: errors.New(op).WithMsgf(
				"QRZ rejected update: %s", reasonOrDefault(resp.Reason, "no reason given"),
			),
		}

	default:
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err: errors.New(op).WithMsgf(
				"unexpected RESULT=%q on update (reason=%q)", resp.Result, resp.Reason,
			),
		}
	}
}

// classifyDelete handles DELETE. Documented RESULT set: OK, FAIL,
// PARTIAL. Single-LOGID deletes make PARTIAL impossible in practice;
// FAIL is reclassified as Success because the record's absence
// upstream matches our intent.
func classifyDelete(resp response) forwarding.Result {
	const op errors.Op = "qrz.classifyDelete"

	switch resp.Result {
	case resultOK:
		return forwarding.Result{Outcome: forwarding.OutcomeSuccess}

	case resultFail:
		// Idempotent delete: QRZ says "LOGID not found", we say "good,
		// that's what we wanted". No error, no retry, row settles.
		return forwarding.Result{Outcome: forwarding.OutcomeSuccess}

	case resultPartial:
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err: errors.New(op).WithMsgf(
				"unexpected RESULT=PARTIAL on single-LOGID delete (reason=%q)", resp.Reason,
			),
		}

	default:
		return forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err: errors.New(op).WithMsgf(
				"unexpected RESULT=%q on delete (reason=%q)", resp.Result, resp.Reason,
			),
		}
	}
}

func reasonOrDefault(reason, fallback string) string {
	if reason == "" {
		return fallback
	}
	return reason
}
