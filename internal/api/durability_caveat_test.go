package api

import (
	"bytes"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// PT-6. A config PUT whose atomic rename succeeded but whose parent-directory fsync
// then failed is APPLIED and live — a 200 with a caveat, not a failure. The handler
// surfaces that two ways, deliberately split: markDurabilityCaveat sets the response
// field, and logDurabilityCaveat writes the warning. The split lets the log land right
// after the commit, BEFORE the fallible buildConfigResponse, so a response-build 500
// can never erase the only record of the crash-durability risk (codex 0606e320 P2).
// The confusable both must exclude is an ordinary durable save, which sets no field
// and logs nothing.

const durabilityCaveatWarnMsg = "config saved but directory fsync failed; " +
	"change is applied and live on disk, crash durability is unconfirmed"

func TestMarkDurabilityCaveat_SetsFieldOnlyWhenUncertain(t *testing.T) {
	var srv Server

	var durable ConfigResponse
	srv.markDurabilityCaveat(&durable, config.Durable)
	if durable.Durability != "" {
		t.Errorf("a durable save must leave Durability empty (field omitted), got %q", durable.Durability)
	}

	var uncertain ConfigResponse
	srv.markDurabilityCaveat(&uncertain, config.DurabilityUncertain)
	if uncertain.Durability != durabilityUnconfirmed {
		t.Errorf("an applied-uncertain save must set Durability = %q, got %q",
			durabilityUnconfirmed, uncertain.Durability)
	}
}

func TestLogDurabilityCaveat_WarnsOnlyWhenUncertain(t *testing.T) {
	buf := &bytes.Buffer{}
	srv := testServerWithLogger(t, nil, nil, logging.NewForWriter(buf))

	// A durable save is silent: no caveat warning to mislead the operator.
	srv.logDurabilityCaveat(config.Durable)
	if recs := allMessages(t, buf, durabilityCaveatWarnMsg); len(recs) != 0 {
		t.Fatalf("a durable save must not log the durability caveat, got %d records\n%s",
			len(recs), buf.String())
	}

	// An applied-uncertain save logs exactly one warning — the durable record of the
	// risk, emitted independently of any response so a later 500 cannot lose it.
	srv.logDurabilityCaveat(config.DurabilityUncertain)
	recs := allMessages(t, buf, durabilityCaveatWarnMsg)
	if len(recs) != 1 {
		t.Fatalf("an applied-uncertain save must log exactly one caveat warning, got %d\n%s",
			len(recs), buf.String())
	}
	if lvl, _ := recs[0]["level"].(string); lvl != "warn" {
		t.Errorf("durability caveat level = %q, want warn", lvl)
	}
}
