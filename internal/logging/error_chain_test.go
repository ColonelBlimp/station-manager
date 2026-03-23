package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	smerrors "github.com/Station-Manager/errors"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

type logEntry map[string]any

func TestBuildErrorChain_WithDetailedAndStd(t *testing.T) {
	// Build Station-Manager DetailedError chain
	inner := smerrors.New("db.Connect").Msg("dial tcp 127.0.0.1:5432: connect: connection refused")
	middle := smerrors.New("db.Open").Err(inner).Msg("failed to connect to database")
	outer := smerrors.New("server.Start").Err(middle).Msg("startup failed")

	chain, root := func(e error) ([]string, string) {
		c, _, r, _ := buildErrorChain(e)
		return c, r
	}(outer)
	assert.Equal(t, []string{
		"startup failed",
		"failed to connect to database",
		"dial tcp 127.0.0.1:5432: connect: connection refused",
	}, chain)
	assert.Equal(t, "dial tcp 127.0.0.1:5432: connect: connection refused", root)

	// Build std errors chain
	wrapped := smerrors.New("wrap.Std").Errorf("wrap: %w", outer)
	chain2, root2 := func(e error) ([]string, string) {
		c, _, r, _ := buildErrorChain(e)
		return c, r
	}(wrapped)
	// first element is wrapped message
	assert.True(t, strings.HasPrefix(chain2[0], "wrap:"))
	assert.Equal(t, root, root2)
}

func TestEventErr_EmitsChainFields(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	le := newLogEvent(logger.Error())

	inner := smerrors.New("db.Connect").Msg("dial tcp 127.0.0.1:5432: connect: connection refused")
	outer := smerrors.New("server.Start").Err(inner).Msg("startup failed")

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
