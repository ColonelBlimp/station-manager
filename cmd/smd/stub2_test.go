package main

import (
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
)

// stub2Type and stub3Type are further registered types backed by the stub's
// constructor, so a test config can carry the station's several names with
// ONE entry per destination type (ADR 0082 part 3; bindings are unique per
// logbook × type).
const (
	stub2Type = "stub2"
	stub3Type = "stub3"
	// stub4Type registers NO default retry: an enabled entry of it builds and
	// seeds, then fails in spawnForwarderWorkers — the workers-node failure the
	// rollback test injects now that an unbuildable entry is refused earlier.
	stub4Type = "stub4"
)

func init() {
	for _, tt := range []string{stub2Type, stub3Type, stub4Type} {
		forwarding.Register(tt, stub.New)
		if tt != stub4Type {
			forwarding.RegisterDefaultRetry(tt, stub.DefaultRetry)
		}
		forwarding.RegisterForwarderType(tt, "Stub "+tt+" (testing)",
			[]forwarding.Action{action.Insert, action.Update, action.Delete},
			[]forwarding.CredentialField{{Key: "mode", Label: "Mode", Kind: "text", Clearable: true, Scope: forwarding.ScopeStation}})
	}
}
