package qrzcq

import (
	"context"
	"encoding/json"
	stderr "errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func responseClient(status int, body string, calls *atomic.Int64) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		if calls != nil {
			calls.Add(1)
		}
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}
}

func testForwarder(client *http.Client, interval time.Duration) *Forwarder {
	return newWithEndpoint("7Q5MLV", "secret-key", "https://example.invalid/logupload", client, interval)
}

func unitSampleQSO() types.Qso {
	return types.Qso{
		ContactedStation: types.ContactedStation{Call: "M0TEST"},
		QsoDetails: types.QsoDetails{
			QsoDate: "20260814",
			TimeOn:  "0322",
			Band:    "20m",
			Mode:    "SSB",
		},
		LoggingStation: types.LoggingStation{StationCallsign: "7Q5MLV"},
	}
}

func TestInit_RegistersQRZCQContract(t *testing.T) {
	if !forwarding.IsRegistered(Type) {
		t.Fatalf("type %q is not registered", Type)
	}
	if got, ok := forwarding.DefaultRetryFor(Type); !ok || got != DefaultRetry {
		t.Fatalf("DefaultRetryFor(%q) = %+v,%v; want %+v,true", Type, got, ok, DefaultRetry)
	}
	if got, ok := forwarding.WorkerDefaultsFor(Type); !ok ||
		got.TickIntervalSec != DefaultTickIntervalSec || got.BatchSize != DefaultBatchSize {
		t.Fatalf("WorkerDefaultsFor(%q) = %+v,%v; want 90-second/one-row defaults", Type, got, ok)
	}
	actions, ok := forwarding.SupportedActionsFor(Type)
	if !ok || len(actions) != 1 || actions[0] != action.Insert {
		t.Fatalf("SupportedActionsFor(%q) = %v,%v; want [insert],true", Type, actions, ok)
	}
}

func TestNew_ValidCredentialsAreTrimmed(t *testing.T) {
	fwd, err := New(types.ForwarderConfig{
		Type:        Type,
		Credentials: json.RawMessage(`{"call":" 7Q5MLV ","key":" secret "}`),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := fwd.(*Forwarder)
	if got.call != "7Q5MLV" || got.key != "secret" {
		t.Fatalf("credentials = call:%q key:%q; want trimmed values", got.call, got.key)
	}
	if got.endpoint != DefaultEndpoint {
		t.Fatalf("endpoint = %q, want %q", got.endpoint, DefaultEndpoint)
	}
	if got.Type() != Type || got.AdifPrefix() != "" {
		t.Fatalf("identity = type:%q prefix:%q; want %q and empty", got.Type(), got.AdifPrefix(), Type)
	}
}

func TestNew_RejectsMissingOrMalformedCredentials(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{name: "absent", want: "credentials required"},
		{name: "malformed", raw: json.RawMessage(`{nope`), want: "parse credentials"},
		{name: "call", raw: json.RawMessage(`{"key":"k"}`), want: "credentials.call"},
		{name: "key", raw: json.RawMessage(`{"call":"7Q5MLV"}`), want: "credentials.key"},
		{name: "blank call", raw: json.RawMessage(`{"call":"  ","key":"k"}`), want: "credentials.call"},
		{name: "blank key", raw: json.RawMessage(`{"call":"7Q5MLV","key":"  "}`), want: "credentials.key"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(types.ForwarderConfig{Type: Type, Credentials: tc.raw})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("New error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestSubmit_RejectsUnsupportedActionsWithoutNetwork(t *testing.T) {
	var calls atomic.Int64
	fwd := testForwarder(responseClient(http.StatusOK, `{"status":"OK"}`, &calls), 0)
	for _, act := range []forwarding.Action{action.Update, action.Delete, "unknown"} {
		res := fwd.Submit(context.Background(), unitSampleQSO(), act, "")
		if res.Outcome != forwarding.OutcomeTerminal || res.Err == nil {
			t.Errorf("action %q result = %+v, want terminal error", act, res)
		}
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("transport calls = %d, want 0 for unsupported actions", got)
	}
}

func TestSubmit_ClassifiesHTTPResponses(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		outcome forwarding.Outcome
	}{
		{name: "request timeout", status: http.StatusRequestTimeout, outcome: forwarding.OutcomeTransient},
		{name: "rate limit", status: http.StatusTooManyRequests, outcome: forwarding.OutcomeTransient},
		{name: "server error", status: http.StatusBadGateway, outcome: forwarding.OutcomeTransient},
		{name: "bad request", status: http.StatusBadRequest, outcome: forwarding.OutcomeTerminal},
		{name: "unauthorized", status: http.StatusUnauthorized, outcome: forwarding.OutcomeTerminal},
		{name: "malformed success", status: http.StatusOK, body: `not json`, outcome: forwarding.OutcomeTerminal},
		{name: "API rejection", status: http.StatusOK, body: `{"status":"ERROR","message":"bad credentials"}`, outcome: forwarding.OutcomeTerminal},
		{name: "queued", status: http.StatusOK, body: `{"status":"OK","message":"DATA_QUEUED"}`, outcome: forwarding.OutcomeSuccess},
		{name: "case insensitive OK", status: http.StatusAccepted, body: `{"status":"ok"}`, outcome: forwarding.OutcomeSuccess},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fwd := testForwarder(responseClient(tc.status, tc.body, nil), 0)
			res := fwd.Submit(context.Background(), unitSampleQSO(), action.Insert, "")
			if res.Outcome != tc.outcome {
				t.Fatalf("outcome = %q, want %q (err=%v)", res.Outcome, tc.outcome, res.Err)
			}
			if tc.outcome == forwarding.OutcomeSuccess && res.Err != nil {
				t.Fatalf("success Err = %v, want nil", res.Err)
			}
			if tc.outcome != forwarding.OutcomeSuccess && res.Err == nil {
				t.Fatal("non-success result has nil Err")
			}
		})
	}
}

func TestSubmit_NoHTTPResponseIsUnreachable(t *testing.T) {
	sentinel := stderr.New("dial failed")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, sentinel
	})}
	res := testForwarder(client, 0).Submit(context.Background(), unitSampleQSO(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeUnreachable || !stderr.Is(res.Err, sentinel) {
		t.Fatalf("result = %+v, want unreachable wrapping transport error", res)
	}
}

func TestSubmit_APIFailureDoesNotLeakCredential(t *testing.T) {
	body := `{"status":"ERROR","message":"bad key secret-key"}`
	res := testForwarder(responseClient(http.StatusOK, body, nil), 0).
		Submit(context.Background(), unitSampleQSO(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTerminal || res.Err == nil {
		t.Fatalf("result = %+v, want terminal API rejection", res)
	}
	if strings.Contains(res.Err.Error(), "secret-key") {
		t.Fatalf("error leaked API key: %v", res.Err)
	}
	if !strings.Contains(res.Err.Error(), "[REDACTED]") {
		t.Fatalf("error = %v, want redaction marker", res.Err)
	}
}

func TestSubmit_PacerPreventsSecondPostAndHonoursCancellation(t *testing.T) {
	var calls atomic.Int64
	fwd := testForwarder(responseClient(http.StatusOK, `{"status":"OK"}`, &calls), time.Hour)
	if res := fwd.Submit(context.Background(), unitSampleQSO(), action.Insert, ""); res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("first Submit = %+v, want immediate success", res)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	res := fwd.Submit(ctx, unitSampleQSO(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTransient || !stderr.Is(res.Err, context.DeadlineExceeded) {
		t.Fatalf("paced Submit = %+v, want cancellable transient wait", res)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("transport calls = %d, want 1; second POST must remain gated", got)
	}
}

func TestSubmit_AlreadyCancelledContextDoesNotReserveOrPost(t *testing.T) {
	var calls atomic.Int64
	fwd := testForwarder(responseClient(http.StatusOK, `{"status":"OK"}`, &calls), time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := fwd.Submit(ctx, unitSampleQSO(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTransient || !stderr.Is(res.Err, context.Canceled) {
		t.Fatalf("result = %+v, want transient context cancellation", res)
	}
	if calls.Load() != 0 {
		t.Fatal("cancelled Submit reached transport")
	}
}
