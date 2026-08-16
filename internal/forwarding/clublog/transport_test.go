package clublog

// ST-4a — ClubLog carries the account password + shared app key to BOTH its realtime and
// delete endpoints, so BOTH must be https (http only for loopback). A6 (each endpoint
// checked independently), A1/A8/A9. allow_insecure_http is SM-Cloud-only.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

func TestNew_TransportPolicy(t *testing.T) {
	creds, err := json.Marshal(map[string]string{
		"email": "op@example.org", "password": "pw", "callsign": "M0ABC",
	})
	if err != nil {
		t.Fatal(err)
	}
	const leak = "clublog.leaky.example"
	remote := "http://" + leak + "/x.php"
	cases := []struct {
		name      string
		endpoints map[string]string
		wantOK    bool
	}{
		{"both default (https)", nil, true},
		{"realtime loopback http", map[string]string{"insert": "http://127.0.0.1:9/r.php"}, true},
		// A6: realtime stays the https default; ONLY the delete endpoint is remote http.
		{"delete endpoint remote http — refused", map[string]string{"delete": remote}, false},
		// And the mirror: only realtime is remote http.
		{"realtime endpoint remote http — refused", map[string]string{"insert": remote}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fc := types.ForwarderConfig{Name: "cl", Type: Type, Credentials: creds, Endpoints: tc.endpoints}
			_, err := New(fc)
			if (err == nil) != tc.wantOK {
				t.Fatalf("New(endpoints=%v) err=%v, wantOK=%v", tc.endpoints, err, tc.wantOK)
			}
			if err != nil && strings.Contains(err.Error(), leak) {
				t.Errorf("A9: error leaked the URL host: %q", err.Error())
			}
		})
	}
}
