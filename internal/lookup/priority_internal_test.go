package lookup

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

type cancellingCallsignProvider struct {
	name   string
	lookup func(context.Context) (types.ContactedStation, error)
	calls  int
}

func (p *cancellingCallsignProvider) Name() string { return p.name }

func (p *cancellingCallsignProvider) Initialize(context.Context) error { return nil }

func (p *cancellingCallsignProvider) Lookup(callsign string) (types.ContactedStation, error) {
	return p.LookupWithContext(context.Background(), callsign)
}

func (p *cancellingCallsignProvider) LookupWithContext(ctx context.Context, _ string) (types.ContactedStation, error) {
	p.calls++
	return p.lookup(ctx)
}

func TestAcceptance_CancellationDuringProviderStopsRemainingChain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	first := &cancellingCallsignProvider{
		name: "first",
		lookup: func(context.Context) (types.ContactedStation, error) {
			cancel()
			return types.ContactedStation{}, context.Canceled
		},
	}
	second := &cancellingCallsignProvider{
		name: "second",
		lookup: func(context.Context) (types.ContactedStation, error) {
			return types.ContactedStation{Name: "must not run"}, nil
		},
	}
	o := &Orchestrator{
		Chain:           []CallsignProvider{first, second},
		ContinueIfBlank: []string{"name", "gridsquare"},
	}

	_, _ = o.runChain(ctx, "7Q7EB")
	if first.calls != 1 || second.calls != 0 {
		t.Fatalf("provider calls = first:%d second:%d, want 1/0", first.calls, second.calls)
	}
}

func TestFillCallsignBlanks_CoversEveryCallsignOwnedFieldOnly(t *testing.T) {
	preferred := types.ContactedStation{
		Call: "7Q5MLV", Name: "Preferred name", QTH: "Preferred QTH",
	}
	fallback := types.ContactedStation{
		CSID: 99, Address: "Address", Age: "40", Altitude: "1200", Call: "WRONG",
		Cont: "AF", ContactedOp: "Op", Country: "Country", CQZ: "37", DXCC: "444",
		Email: "mail@example.test", EqCall: "7Q5MLV/P", Gridsquare: "KH67RT",
		Iota: "AF-000", IotaIslandId: "1", ITUZ: "53", Lat: "-15.0", Lon: "35.0",
		Name: "Fallback name", QTH: "Fallback QTH", Sig: "SIG", SigInfo: "Info",
		Web: "https://example.test", WwffRef: "7QFF-0001",
		LastRefreshedAt: time.Unix(1, 0),
	}
	want := types.ContactedStation{
		Address: "Address", Age: "40", Altitude: "1200", Call: "7Q5MLV",
		ContactedOp: "Op", Email: "mail@example.test", EqCall: "7Q5MLV/P",
		Gridsquare: "KH67RT", Iota: "AF-000", IotaIslandId: "1", Lat: "-15.0",
		Lon: "35.0", Name: "Preferred name", QTH: "Preferred QTH", Sig: "SIG",
		SigInfo: "Info", Web: "https://example.test", WwffRef: "7QFF-0001",
	}

	if got := fillCallsignBlanks(preferred, fallback); !reflect.DeepEqual(got, want) {
		t.Fatalf("fillCallsignBlanks() =\n%+v\nwant\n%+v", got, want)
	}
}
