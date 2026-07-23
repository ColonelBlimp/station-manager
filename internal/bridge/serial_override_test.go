package bridge

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// A per-rig override must be able to flip a modem line in BOTH directions —
// tri-state, so an explicit false is a real setting and not "unset".
func TestBuildSerialConfig_RTSDTROverride(t *testing.T) {
	rigdef := cat.RigSerial{BaudRate: 38400, DataBits: 8, StopBits: 1, Parity: "none", LineDelimiter: ";"}
	tru, fls := true, false

	base, err := buildSerialConfig(types.BridgeSerialConfig{Port: "/dev/null"}, rigdef)
	if err != nil {
		t.Fatalf("base: %v", err)
	}
	if base.RTS != nil || base.DTR != nil {
		t.Errorf("no override should inherit the rigdef (nil here), got RTS=%v DTR=%v", base.RTS, base.DTR)
	}

	rigdef.RTS, rigdef.DTR = &fls, &fls
	on, err := buildSerialConfig(types.BridgeSerialConfig{
		Port:      "/dev/null",
		Overrides: types.RigOverrides{RTS: &tru, DTR: &tru},
	}, rigdef)
	if err != nil {
		t.Fatalf("override-on: %v", err)
	}
	if on.RTS == nil || !*on.RTS || on.DTR == nil || !*on.DTR {
		t.Errorf("override true must assert both lines, got RTS=%v DTR=%v", on.RTS, on.DTR)
	}

	rigdef.RTS, rigdef.DTR = &tru, &tru
	off, err := buildSerialConfig(types.BridgeSerialConfig{
		Port:      "/dev/null",
		Overrides: types.RigOverrides{RTS: &fls, DTR: &fls},
	}, rigdef)
	if err != nil {
		t.Fatalf("override-off: %v", err)
	}
	if off.RTS == nil || *off.RTS || off.DTR == nil || *off.DTR {
		t.Errorf("override FALSE must de-assert (not read as unset), got RTS=%v DTR=%v", off.RTS, off.DTR)
	}
}
