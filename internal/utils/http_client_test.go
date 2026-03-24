package utils

import (
	"testing"
	"time"
)

func TestNewHTTPClient_DefaultTimeout(t *testing.T) {
	client := NewHTTPClient(0)
	if client == nil {
		t.Fatal("NewHTTPClient(0) returned nil")
	}
	if client.Timeout != 15*time.Second {
		t.Fatalf("expected default timeout 15s, got %v", client.Timeout)
	}
}

func TestNewHTTPClient_CustomTimeout(t *testing.T) {
	want := 30 * time.Second
	client := NewHTTPClient(want)
	if client == nil {
		t.Fatal("NewHTTPClient(30s) returned nil")
	}
	if client.Timeout != want {
		t.Fatalf("expected timeout %v, got %v", want, client.Timeout)
	}
}

func TestNewHTTPClient_HasTransport(t *testing.T) {
	client := NewHTTPClient(0)
	if client.Transport == nil {
		t.Fatal("expected non-nil Transport")
	}
}
