package qrzcq_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/lookup"
	"github.com/ColonelBlimp/station-manager/internal/lookup/qrzcq"
	"github.com/ColonelBlimp/station-manager/internal/lookupdef"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// TestAcceptance_QRZCQXMLCallsignLookup is the executable feature contract.
// It stays outside package qrzcq and constructs the provider through the same
// registries and config shape used by the daemon.
func TestAcceptance_QRZCQXMLCallsignLookup(t *testing.T) {
	t.Run("the provider is discoverable and seeded disabled", func(t *testing.T) {
		descriptor, ok := lookupdef.Descriptor(qrzcq.ServiceName)
		if !ok {
			t.Fatalf("lookup descriptor %q is not registered", qrzcq.ServiceName)
		}
		if descriptor.DisplayName != "QRZCQ.com" || descriptor.Kind != lookupdef.KindCallsign {
			t.Fatalf("descriptor = %+v, want QRZCQ.com callsign provider", descriptor)
		}
		if _, ok := lookup.CallsignConstructorFor(qrzcq.ServiceName); !ok {
			t.Fatalf("callsign constructor %q is not registered", qrzcq.ServiceName)
		}

		cfg := config.DefaultConfig(t.TempDir())
		var seeded *types.LookupConfig
		for i := range cfg.Lookup.Chain {
			if cfg.Lookup.Chain[i].Name == qrzcq.ServiceName {
				seeded = &cfg.Lookup.Chain[i]
				break
			}
		}
		if seeded == nil {
			t.Fatalf("DefaultConfig did not seed %q", qrzcq.ServiceName)
		}
		if seeded.Enabled {
			t.Fatal("seeded QRZCQ XML lookup is enabled; credentials must be opt-in")
		}
		if seeded.URL != qrzcq.DefaultURL || seeded.ViewURL != qrzcq.DefaultViewURL {
			t.Fatalf("seeded URLs = %q / %q, want %q / %q",
				seeded.URL, seeded.ViewURL, qrzcq.DefaultURL, qrzcq.DefaultViewURL)
		}
		if seeded.HttpTimeoutSec != qrzcq.DefaultHTTPTimeoutSec {
			t.Fatalf("seeded timeout = %d, want %d", seeded.HttpTimeoutSec, qrzcq.DefaultHTTPTimeoutSec)
		}
	})

	t.Run("login lookup and expired-session recovery follow the QRZCQ XML protocol", func(t *testing.T) {
		var logins atomic.Int32
		var lookups atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/xml")
			q := r.URL.Query()
			if q.Get("username") != "" {
				if q.Get("username") != "7Q5MLV" || q.Get("password") != "premium-password" {
					t.Errorf("login credentials = %q / %q, want configured values",
						q.Get("username"), q.Get("password"))
				}
				if q.Get("agent") != "station-manager/acceptance" {
					t.Errorf("login agent = %q, want station-manager/acceptance", q.Get("agent"))
				}
				n := logins.Add(1)
				_, _ = fmt.Fprintf(w, `<QRZCQDatabase xmlns="http://qrzcq.com"><Session><Key>key-%d</Key></Session></QRZCQDatabase>`, n)
				return
			}

			if q.Get("password") != "" {
				t.Error("lookup request leaked the account password")
			}
			lookups.Add(1)
			switch {
			case q.Get("callsign") == "N0W" && q.Get("s") == "key-1":
				_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
					<QRZCQDatabase xmlns="http://qrzcq.com">
						<Callsign>
							<call>N0W</call><name>Al Example</name><qth>Fessenden</qth>
							<address>1 Main St</address><city>Fessenden</city><zip>58438</zip>
							<continent>NA</continent><country>United States</country><state>ND</state>
							<locator>EN07GN</locator><latitude>47.5625</latitude><longitude>-99.4583</longitude>
							<website>https://example.test/n0w</website><dxcc>291</dxcc><itu>8</itu><cq>5</cq><iota>NA-001</iota>
						</Callsign><Session><Key>key-1</Key></Session>
					</QRZCQDatabase>`)
			case q.Get("callsign") == "M0CMC" && q.Get("s") == "key-1":
				_, _ = io.WriteString(w, `<QRZCQDatabase xmlns="http://qrzcq.com"><Session><Error>Session Timeout</Error></Session></QRZCQDatabase>`)
			case q.Get("callsign") == "M0CMC" && q.Get("s") == "key-2":
				_, _ = io.WriteString(w, `<QRZCQDatabase xmlns="http://qrzcq.com"><Callsign><call>M0CMC</call><name>Marc</name></Callsign><Session><Key>key-2</Key></Session></QRZCQDatabase>`)
			default:
				t.Errorf("unexpected lookup query: callsign=%q session=%q", q.Get("callsign"), q.Get("s"))
				http.Error(w, "unexpected request", http.StatusBadRequest)
			}
		}))
		defer srv.Close()

		ctor, ok := lookup.CallsignConstructorFor(qrzcq.ServiceName)
		if !ok {
			t.Fatalf("callsign constructor %q is not registered", qrzcq.ServiceName)
		}
		providerCfg := types.LookupConfig{
			Name:           qrzcq.ServiceName,
			Enabled:        true,
			URL:            srv.URL,
			Username:       "7Q5MLV",
			Password:       "premium-password",
			HttpTimeoutSec: 5,
		}
		provider := ctor(logging.NewForWriter(io.Discard), &providerCfg, "station-manager/acceptance")
		if err := provider.Initialize(context.Background()); err != nil {
			t.Fatalf("Initialize: %v", err)
		}

		got, err := provider.LookupWithContext(context.Background(), " n0w ")
		if err != nil {
			t.Fatalf("LookupWithContext(N0W): %v", err)
		}
		want := types.ContactedStation{
			Call: "N0W", Name: "Al Example", QTH: "Fessenden",
			Address: "1 Main St, Fessenden, ND, 58438, United States",
			Cont:    "NA", Country: "United States", Gridsquare: "EN07GN",
			Lat: "47.5625", Lon: "-99.4583", Web: "https://example.test/n0w",
			DXCC: "291", ITUZ: "8", CQZ: "5", Iota: "NA-001",
		}
		if got != want {
			t.Fatalf("station = %+v, want %+v", got, want)
		}

		got, err = provider.LookupWithContext(context.Background(), "M0CMC")
		if err != nil {
			t.Fatalf("LookupWithContext(M0CMC) after expiry: %v", err)
		}
		if got.Call != "M0CMC" || got.Name != "Marc" {
			t.Fatalf("station after expiry = %+v, want M0CMC / Marc", got)
		}
		if logins.Load() != 2 || lookups.Load() != 3 {
			t.Fatalf("requests = %d logins / %d lookups, want 2 / 3", logins.Load(), lookups.Load())
		}
	})
}
