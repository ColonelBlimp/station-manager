package iocdi

import (
	"reflect"
	"testing"
)

type testIface interface{ A() int }

type concreteDep struct{}

func (c *concreteDep) A() int { return 42 }

type receiver struct {
	Dep testIface `di.inject:"dep"`
}

func TestInterfaceInjection(t *testing.T) {
	c := New()

	// Register the receiver and the concrete dependency
	if err := c.Register("receiver", reflect.TypeOf((*receiver)(nil))); err != nil {
		t.Fatalf("register receiver: %v", err)
	}
	if err := c.Register("dep", reflect.TypeOf((*concreteDep)(nil))); err != nil {
		t.Fatalf("register dep: %v", err)
	}

	if err := c.Build(); err != nil {
		t.Fatalf("build: %v", err)
	}

	v, err := c.ResolveSafe("receiver")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	r := v.(*receiver)
	if r.Dep == nil {
		t.Fatalf("expected interface dependency to be injected")
	}
	if got := r.Dep.A(); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestInterfaceMismatchFailsBuild(t *testing.T) {
	type other struct{}

	c := New()
	if err := c.Register("receiver", reflect.TypeOf((*receiver)(nil))); err != nil {
		t.Fatalf("register receiver: %v", err)
	}
	if err := c.Register("dep", reflect.TypeOf((*other)(nil))); err != nil {
		t.Fatalf("register dep: %v", err)
	}

	if err := c.Build(); err == nil {
		t.Fatalf("expected build to fail due to interface mismatch")
	}
}
