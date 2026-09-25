package forwarding

import (
	"encoding/json"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ADR 0082 parts 5–6 (W-0021 5B): a binding resolved against its station
// account is one synthesized ForwarderConfig — the shape the existing
// constructor, worker and enqueue path already consume — plus the logbook it
// serves. Station-scoped keys come from the account, logbook-scoped keys from
// the binding; the binding's name and enabled state win.

func init() {
	RegisterForwarderType("bindres-test", "Binding Resolve",
		[]Action{action.Insert, action.Delete},
		[]CredentialField{
			{Key: "url", Label: "URL", Kind: "text", Scope: ScopeStation},
			{Key: "token", Label: "Token", Kind: "password", Scope: ScopeStation},
			{Key: "logbook", Label: "Logbook", Kind: "text", Scope: ScopeLogbook},
		})
}

func TestBindingConfig_MergesAccountAndBinding(t *testing.T) {
	retry := &types.RetryConfig{MaxAttempts: 2, InitialBackoffSec: 1, MaxBackoffSec: 2}
	account := types.ForwarderConfig{
		Name: "cloud", Type: "bindres-test", Label: "My cloud", Enabled: true,
		Credentials:     json.RawMessage(`{"url":"https://c","token":"t","logbook":"station-level-ignored"}`),
		ActionFilter:    []string{"insert"},
		Endpoints:       map[string]string{"insert": "https://c/put"},
		TickIntervalSec: 7, BatchSize: 3, Retry: retry, AllowInsecureHTTP: true,
	}
	binding := types.LogbookDestination{
		LogbookID: 2, Destination: "bindres-test", ForwarderName: "bindres-test.0192-uuid", Enabled: false,
		Credentials: json.RawMessage(`{"logbook":"contest"}`),
	}
	fc, err := BindingConfig(binding, account)
	if err != nil {
		t.Fatal(err)
	}
	if fc.Name != "bindres-test.0192-uuid" || fc.Type != "bindres-test" || fc.Enabled || fc.Label != "My cloud" {
		t.Fatalf("identity/state = %+v", fc)
	}
	if fc.TickIntervalSec != 7 || fc.BatchSize != 3 || fc.Retry != retry || !fc.AllowInsecureHTTP ||
		fc.Endpoints["insert"] != "https://c/put" || len(fc.ActionFilter) != 1 || fc.ActionFilter[0] != "insert" {
		t.Fatalf("station-scoped settings not carried: %+v", fc)
	}
	var creds map[string]string
	if err := json.Unmarshal(fc.Credentials, &creds); err != nil {
		t.Fatal(err)
	}
	if creds["url"] != "https://c" || creds["token"] != "t" || creds["logbook"] != "contest" || len(creds) != 3 {
		t.Fatalf("credentials = %v; want the account's station keys and the binding's logbook key", creds)
	}
}

// Keys of the wrong scope never cross ownership (operator review of 5B(b),
// P1): a binding carrying `url` cannot replace the station's URL while keeping
// its token, an account carrying `logbook` cannot bind a logbook, and a key the
// type never declared is dropped from both.
func TestBindingConfig_WrongScopeKeysCannotCrossOwnership(t *testing.T) {
	account := types.ForwarderConfig{Name: "cloud", Type: "bindres-test",
		Credentials: json.RawMessage(`{"url":"https://station","token":"t","logbook":"from-account","stray":"x"}`)}
	binding := types.LogbookDestination{Destination: "bindres-test", ForwarderName: "b", Enabled: true,
		Credentials: json.RawMessage(`{"url":"https://attacker","token":"t2","logbook":"mine","stray":"y"}`)}
	fc, err := BindingConfig(binding, account)
	if err != nil {
		t.Fatal(err)
	}
	var creds map[string]string
	if err := json.Unmarshal(fc.Credentials, &creds); err != nil {
		t.Fatal(err)
	}
	if creds["url"] != "https://station" || creds["token"] != "t" {
		t.Fatalf("station keys = url %q token %q; the binding must not override them", creds["url"], creds["token"])
	}
	if creds["logbook"] != "mine" {
		t.Fatalf("logbook key = %q; the account must not supply a logbook-scoped key", creds["logbook"])
	}
	if _, ok := creds["stray"]; ok || len(creds) != 3 {
		t.Fatalf("credentials = %v; an undeclared key must be dropped", creds)
	}
}

func TestBindingConfig_Refusals(t *testing.T) {
	account := types.ForwarderConfig{Name: "cloud", Type: "bindres-test", Credentials: json.RawMessage(`{"url":"u","token":"t"}`)}
	cases := []struct {
		name    string
		binding types.LogbookDestination
		account types.ForwarderConfig
	}{
		{"type mismatch", types.LogbookDestination{Destination: "qrz", ForwarderName: "x"}, account},
		{"non-object binding blob", types.LogbookDestination{Destination: "bindres-test", ForwarderName: "x", Credentials: json.RawMessage(`"abc"`)}, account},
		{"non-object account blob", types.LogbookDestination{Destination: "bindres-test", ForwarderName: "x"},
			types.ForwarderConfig{Name: "cloud", Type: "bindres-test", Credentials: json.RawMessage(`[1]`)}},
		{"unregistered type", types.LogbookDestination{Destination: "no-such", ForwarderName: "x"},
			types.ForwarderConfig{Name: "n", Type: "no-such"}},
		{"empty name", types.LogbookDestination{Destination: "bindres-test"}, account},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := BindingConfig(c.binding, c.account); err == nil {
				t.Fatalf("%s: want an error", c.name)
			}
		})
	}
}

// ResolveBindings pairs every binding with its account; a binding whose
// destination has no station account is a named fault, never silently dropped
// and never a worker.
func TestResolveBindings_PairsAndFaults(t *testing.T) {
	accounts := []types.ForwarderConfig{{Name: "cloud", Type: "bindres-test", Credentials: json.RawMessage(`{"url":"u","token":"t"}`)}}
	bindings := []types.LogbookDestination{
		{LogbookID: 1, Destination: "bindres-test", ForwarderName: "cloud", Enabled: true},
		{LogbookID: 1, Destination: "qrz", ForwarderName: "qrz", Enabled: true},
	}
	routes, faults := ResolveBindings(bindings, accounts)
	if len(routes) != 1 || routes[0].LogbookID != 1 || routes[0].Config.Name != "cloud" || !routes[0].Config.Enabled {
		t.Fatalf("routes = %+v", routes)
	}
	if len(faults) != 1 || faults[0].ForwarderName != "qrz" || faults[0].Err == nil {
		t.Fatalf("faults = %+v; want the account-less qrz binding named", faults)
	}
}
