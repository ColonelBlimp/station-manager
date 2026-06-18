package adif

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

func TestRecord_ApplyQslDefaults(t *testing.T) {
	d := types.QslDefaults{QslVia: "via M0XXX", QslMsg: "TNX QSO", QslSendVia: "B"}

	t.Run("fills empty fields", func(t *testing.T) {
		var r Record
		r.ApplyQslDefaults(d)
		if r.QslVia != "via M0XXX" || r.QslMsg != "TNX QSO" || r.QslSentVia != "B" {
			t.Fatalf("defaults not stamped: via=%q msg=%q sentvia=%q", r.QslVia, r.QslMsg, r.QslSentVia)
		}
	})

	t.Run("per-QSO value wins; other empties still fill", func(t *testing.T) {
		r := Record{}
		r.QslVia = "via G4ABC"
		r.ApplyQslDefaults(d)
		if r.QslVia != "via G4ABC" {
			t.Fatalf("per-QSO QslVia overwritten: %q", r.QslVia)
		}
		if r.QslMsg != "TNX QSO" || r.QslSentVia != "B" {
			t.Fatalf("other defaults not filled: msg=%q sentvia=%q", r.QslMsg, r.QslSentVia)
		}
	})

	t.Run("empty default never stamps", func(t *testing.T) {
		var r Record
		r.ApplyQslDefaults(types.QslDefaults{})
		if r.QslVia != "" || r.QslMsg != "" || r.QslSentVia != "" {
			t.Fatalf("empty defaults stamped something: %+v", r.QslSection)
		}
	})

	t.Run("nil receiver is a no-op", func(t *testing.T) {
		var r *Record
		r.ApplyQslDefaults(d) // must not panic
	})
}
