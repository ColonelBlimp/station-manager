package errors

import (
	stderr "errors"
	"fmt"
	"testing"
)

func TestRoot_SimpleChain(t *testing.T) {
	leaf := stderr.New("Contacted station not found")
	wrapped := fmt.Errorf("database failure: %w", leaf)
	top := New("facade.NewQso").Err(wrapped).Msg("Failed to create QSO")

	if got := Root(top); got != leaf {
		t.Fatalf("Root(top) = %v, want %v", got, leaf)
	}

	if got := Root(leaf); got != leaf {
		t.Fatalf("Root(leaf) = %v, want %v", got, leaf)
	}

	if got := Root(nil); got != nil {
		t.Fatalf("Root(nil) = %v, want nil", got)
	}
}

func TestRoot_DetailedErrorChain(t *testing.T) {
	root := New("database.Service.sqliteFetchContactedStationByCallsign").Msg("Contacted station not found")
	mid := New("facade.NewQso.mid").Err(root)
	top := New("facade.NewQso").Err(mid)

	got := Root(top)
	if got != root {
		t.Fatalf("Root(top) = %v, want %v", got, root)
	}

	if dErr, ok := AsDetailedError(got); !ok {
		t.Fatalf("Root(top) is not DetailedError: %T", got)
	} else if dErr.Op() != "database.Service.sqliteFetchContactedStationByCallsign" {
		t.Fatalf("root Op() = %q, want %q", dErr.Op(), "database.Service.sqliteFetchContactedStationByCallsign")
	}
}
