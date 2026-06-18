package types

import "testing"

func TestQslDefaults_ApplyTo(t *testing.T) {
	d := QslDefaults{QslVia: "via M0XXX", QslMsg: "TNX QSO", QslSendVia: "B"}

	t.Run("fills empty fields", func(t *testing.T) {
		var q Qso
		d.ApplyTo(&q)
		if q.QslVia != "via M0XXX" || q.QslMsg != "TNX QSO" || q.QslSendVia != "B" {
			t.Fatalf("defaults not stamped: %+v", q.Qsl)
		}
	})

	t.Run("does not overwrite a per-QSO value", func(t *testing.T) {
		q := Qso{}
		q.QslVia = "via G4ABC" // per-QSO wins
		d.ApplyTo(&q)
		if q.QslVia != "via G4ABC" {
			t.Fatalf("per-QSO QslVia overwritten: %q", q.QslVia)
		}
		// The other empty fields still fill from defaults.
		if q.QslMsg != "TNX QSO" || q.QslSendVia != "B" {
			t.Fatalf("other defaults not filled: %+v", q.Qsl)
		}
	})

	t.Run("empty default never stamps (stays empty -> ADIF omits)", func(t *testing.T) {
		var q Qso
		(QslDefaults{}).ApplyTo(&q)
		if q.QslVia != "" || q.QslMsg != "" || q.QslSendVia != "" {
			t.Fatalf("empty defaults stamped something: %+v", q.Qsl)
		}
	})

	t.Run("nil receiver target is a no-op", func(t *testing.T) {
		d.ApplyTo(nil) // must not panic
	})
}
