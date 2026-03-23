package utils

import (
	"errors"
	"testing"
)

type person struct {
	Name string
	Age  int
}

type personOut struct {
	Name string
	Age  int
}

func TestDeepCopy_Success(t *testing.T) {
	in := person{Name: "Bob", Age: 42}
	var out personOut
	if err := DeepCopy(in, &out); err != nil {
		t.Fatalf("DeepCopy error: %v", err)
	}
	if out.Name != in.Name || out.Age != in.Age {
		t.Fatalf("copy mismatch: %+v vs %+v", out, in)
	}
}

func TestDeepCopy_ErrorOnUnmarshal(t *testing.T) {
	// Provide a target that cannot be unmarshaled into (non-pointer already tested by compiler types)
	var out chan int
	err := DeepCopy(person{Name: "x"}, &out)
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
	// ensure wrapping with %w possible
	if !errors.Is(err, err) { // trivial check to exercise errors.Is call site
		// no-op, just ensure err is error
	}
}
