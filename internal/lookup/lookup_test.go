package lookup

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

func TestFilterToCallsignFields_ClearsHamnutExclusiveFields(t *testing.T) {
	in := types.ContactedStation{
		Call:       "M0CMC",
		Name:       "Marc",
		QTH:        "Lilongwe",
		Gridsquare: "KH53",
		// Per ADR 0017 #2, the orchestrator must strip these even if a
		// callsign-class provider populates them. The QRZ-records-Malawi-
		// for-an-English-call example from the ADR is exactly this case.
		Country: "Malawi",
		Cont:    "AF",
		CQZ:     "37",
		ITUZ:    "53",
		DXCC:    "440",
	}

	out := FilterToCallsignFields(in)

	if out.Country != "" {
		t.Errorf("Country = %q, want empty", out.Country)
	}
	if out.Cont != "" {
		t.Errorf("Cont = %q, want empty", out.Cont)
	}
	if out.CQZ != "" {
		t.Errorf("CQZ = %q, want empty", out.CQZ)
	}
	if out.ITUZ != "" {
		t.Errorf("ITUZ = %q, want empty", out.ITUZ)
	}
	if out.DXCC != "" {
		t.Errorf("DXCC = %q, want empty", out.DXCC)
	}

	// Non-country fields pass through unchanged.
	if out.Call != "M0CMC" {
		t.Errorf("Call = %q, want preserved", out.Call)
	}
	if out.Name != "Marc" {
		t.Errorf("Name = %q, want preserved", out.Name)
	}
	if out.QTH != "Lilongwe" {
		t.Errorf("QTH = %q, want preserved", out.QTH)
	}
	if out.Gridsquare != "KH53" {
		t.Errorf("Gridsquare = %q, want preserved", out.Gridsquare)
	}
}

func TestFilterToCallsignFields_DoesNotMutateInput(t *testing.T) {
	in := types.ContactedStation{
		Call:    "M0CMC",
		Country: "Malawi",
		CQZ:     "37",
	}

	_ = FilterToCallsignFields(in)

	// Filter takes by value and returns a copy — the caller's struct
	// should be untouched. Guards against future "optimisation" that
	// switches to pointer receiver and silently mutates.
	if in.Country != "Malawi" {
		t.Errorf("input mutated: Country = %q, want unchanged", in.Country)
	}
	if in.CQZ != "37" {
		t.Errorf("input mutated: CQZ = %q, want unchanged", in.CQZ)
	}
}
