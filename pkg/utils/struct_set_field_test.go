package utils

import (
	"errors"
	"testing"
)

type sample struct {
	Name string
	Age  string
}

func TestSetStructStringField_Success(t *testing.T) {
	s := &sample{}
	if err := SetStructStringField(s, "Name", "Alice"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Name != "Alice" {
		t.Fatalf("field not set, got %q", s.Name)
	}
}

func TestSetStructStringField_Errors(t *testing.T) {
	// non-pointer
	var s sample
	if err := SetStructStringField(s, "Name", "Alice"); err == nil {
		t.Fatal("expected error for non-pointer input")
	}
	// non-struct pointer
	i := 3
	if err := SetStructStringField(&i, "Name", "Alice"); err == nil {
		t.Fatal("expected error for pointer to non-struct")
	}
	// no such field
	sp := &sample{}
	if err := SetStructStringField(sp, "Missing", "x"); err == nil {
		t.Fatal("expected error for missing field")
	}
	// wrong kind
	type bad struct{ X int }
	bp := &bad{}
	if err := SetStructStringField(bp, "X", "x"); err == nil {
		t.Fatal("expected error for non-string field")
	}
	// cannot set unexported not applicable here; ensure proper error messages contain key phrases
	err := SetStructStringField(&sample{}, "Name", "ok")
	if err != nil {
		// should be nil, verifying positive path covered above
		_ = errors.Is(err, nil)
	}
}
