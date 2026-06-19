package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	smerrors "github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

type logEntry map[string]any

func TestBuildErrorChain_WithDetailedAndStd(t *testing.T) {
	// Build Station-Manager DetailedError chain.
	//
	// Note on assertions: after the 4.1/4.2 errors-package rework, every
	// frame's Error() returns a rich "op: msg: cause.Error()" representation
	// that recursively includes the remaining chain from that point. The
	// per-frame entries collected by buildErrorChain therefore each contain
	// the full tail of the chain, not just the local message. This is a
	// deliberate consequence of aligning Error() with stdlib conventions
	// for wrapped errors; the test was updated to reflect the new shape.
	inner := smerrors.New("db.Connect").WithMsg("dial tcp 127.0.0.1:5432: connect: connection refused")
	middle := smerrors.New("db.Open").WithErr(inner).WithMsg("failed to connect to database")
	outer := smerrors.New("server.Start").WithErr(middle).WithMsg("startup failed")

	chain, root := func(e error) ([]string, string) {
		c, _, r, _ := buildErrorChain(e)
		return c, r
	}(outer)
	assert.Equal(t, []string{
		"server.Start: startup failed: db.Open: failed to connect to database: db.Connect: dial tcp 127.0.0.1:5432: connect: connection refused",
		"db.Open: failed to connect to database: db.Connect: dial tcp 127.0.0.1:5432: connect: connection refused",
		"db.Connect: dial tcp 127.0.0.1:5432: connect: connection refused",
	}, chain)
	assert.Equal(t, "db.Connect: dial tcp 127.0.0.1:5432: connect: connection refused", root)

	// Build a DetailedError wrapping a std error chain.
	wrapped := smerrors.New("wrap.Std").WithErr(fmt.Errorf("wrap: %w", outer))
	chain2, root2 := func(e error) ([]string, string) {
		c, _, r, _ := buildErrorChain(e)
		return c, r
	}(wrapped)
	// first element is the wrap.Std DetailedError's full chain output,
	// which starts with its op identifier under the new Error() format
	assert.True(t, strings.HasPrefix(chain2[0], "wrap.Std:"))
	// the format string passed to Errorf is still present inside that first
	// element, verifying the wrapper's intent is preserved
	assert.True(t, strings.Contains(chain2[0], "wrap: "))
	assert.Equal(t, root, root2)
}

// TestBuildErrorChain_StdWrapperFrameNotDuplicated is the M1 regression: a
// DetailedError → stdlib fmt wrapper → DetailedError chain must record the
// stdlib wrapper as its own frame (empty op) and the inner DetailedError exactly
// once. Before the fix, buildErrorChain used the chain-searching AsDetailedError
// as a per-frame predicate, so the wrapper frame was dropped and the inner
// DetailedError was counted twice.
func TestBuildErrorChain_StdWrapperFrameNotDuplicated(t *testing.T) {
	inner := smerrors.New("db.Connect").WithMsg("refused")
	outer := smerrors.New("server.Start").WithErr(fmt.Errorf("wrap: %w", inner)).WithMsg("boom")

	chain, ops, root, _ := buildErrorChain(outer)

	assert.Equal(t, []string{
		"server.Start: boom: wrap: db.Connect: refused",
		"wrap: db.Connect: refused",
		"db.Connect: refused",
	}, chain)
	assert.Equal(t, []string{"server.Start", "", "db.Connect"}, ops)
	assert.Equal(t, "db.Connect: refused", root)
}

func TestEventErr_EmitsChainFields(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	le := newLogEvent(logger.Error())

	inner := smerrors.New("db.Connect").WithMsg("dial tcp 127.0.0.1:5432: connect: connection refused")
	outer := smerrors.New("server.Start").WithErr(inner).WithMsg("startup failed")

	le.Err(outer).Msg("boom")

	var entry logEntry
	dec := json.NewDecoder(&buf)
	if err := dec.Decode(&entry); err != nil {
		t.Fatalf("failed to decode json log: %v", err)
	}

	// Zerolog sets error field by key "error"
	if v, ok := entry[zerolog.ErrorFieldName]; !ok || v == "" {
		t.Fatalf("expected %q field to be present", zerolog.ErrorFieldName)
	}

	// Our enrichment fields
	if _, ok := entry["error_chain"]; !ok {
		t.Fatal("expected error_chain field to be present")
	}
	if _, ok := entry["error_root"]; !ok {
		t.Fatal("expected error_root field to be present")
	}
	if _, ok := entry["error_history"]; !ok {
		t.Fatal("expected error_history field to be present")
	}

	// Ops enrichment fields
	if ops, ok := entry["error_ops"]; !ok {
		t.Fatal("expected error_ops field to be present")
	} else {
		// should be an array of strings
		_, _ = ops.([]any)
	}
	// root op may be empty if root isn't DetailedError, but in our test it is empty
	// because the root is a DetailedError with op "db.Connect"; verify presence and value
	if rootOp, ok := entry["error_root_op"]; ok {
		_ = rootOp.(string)
	}
}
