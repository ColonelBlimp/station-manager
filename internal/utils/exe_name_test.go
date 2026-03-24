package utils

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestExecName_WithExtension(t *testing.T) {
	name, err := ExecName(false)
	if err != nil {
		t.Fatalf("ExecName(false) error: %v", err)
	}
	if name == "" {
		t.Fatal("ExecName(false) returned empty string")
	}
}

func TestExecName_StripExtension(t *testing.T) {
	name, err := ExecName(true)
	if err != nil {
		t.Fatalf("ExecName(true) error: %v", err)
	}
	if name == "" {
		t.Fatal("ExecName(true) returned empty string")
	}
	if strings.Contains(name, ".") {
		// Only fail if there's an extension; a plain name with no dot is fine
		ext := filepath.Ext(name)
		if ext != "" {
			t.Fatalf("ExecName(true) = %q; expected no extension", name)
		}
	}
}

func TestExecName_NoPathSeparator(t *testing.T) {
	for _, strip := range []bool{false, true} {
		name, err := ExecName(strip)
		if err != nil {
			t.Fatalf("ExecName(%v) error: %v", strip, err)
		}
		if strings.ContainsAny(name, `/\`) {
			t.Fatalf("ExecName(%v) = %q; should not contain path separators", strip, name)
		}
	}
}
