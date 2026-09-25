package archive

import (
	"context"
	"encoding/json"
	stderr "errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	_ "github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud" // registers the smcloud descriptor
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ADR 0082 parts 1, 8 and 9 (W-0021 5D): the bindings view and the PUT that
// edits them — aggregate state over live logbooks, whole-candidate validation,
// atomic write, masked keys, the SM Cloud remnant gate, restart_required.

func init() {
	// Each test type has a CONSTRUCTOR as well as a descriptor: enabling a
	// binding builds the exact candidate (ADR 0082, 5D review), and these
	// constructors refuse what real ones refuse — a missing key, or a value
	// that is not a non-empty string (qrz.New cannot parse {"api_key":123}).
	forwarding.Register("bindview-qrz", requireStrings("api_key"))
	forwarding.RegisterForwarderType("bindview-qrz", "QRZ-like",
		[]forwarding.Action{action.Insert},
		[]forwarding.CredentialField{{Key: "api_key", Label: "API key", Kind: "password", Scope: forwarding.ScopeLogbook}})
	forwarding.Register("bindview-cloud", requireStrings("url", "token"))
	forwarding.RegisterForwarderType("bindview-cloud", "Cloud-like",
		[]forwarding.Action{action.Insert},
		[]forwarding.CredentialField{
			{Key: "url", Label: "URL", Kind: "text", Scope: forwarding.ScopeStation},
			{Key: "token", Label: "Token", Kind: "password", Scope: forwarding.ScopeStation},
			{Key: "logbook", Label: "Cloud logbook", Kind: "text", Clearable: true, Scope: forwarding.ScopeLogbook},
		})
	// ClubLog-like: two logbook-scoped fields, so two operators can edit
	// DISJOINT fields of one binding at once.
	forwarding.Register("bindview-club", requireStrings("email", "password"))
	forwarding.RegisterForwarderType("bindview-club", "Club-like",
		[]forwarding.Action{action.Insert},
		[]forwarding.CredentialField{
			{Key: "email", Label: "Account email", Kind: "text", Scope: forwarding.ScopeLogbook},
			{Key: "password", Label: "Application password", Kind: "password", Scope: forwarding.ScopeLogbook},
		})
}

// nopForwarder is what the test constructors return; it never submits.
type nopForwarder struct{}

func (nopForwarder) Type() string       { return "nop" }
func (nopForwarder) AdifPrefix() string { return "" }
func (nopForwarder) Submit(context.Context, types.Qso, forwarding.Action, string) forwarding.Result {
	return forwarding.Result{Outcome: forwarding.OutcomeSuccess}
}

// requireStrings builds only when every key holds a non-empty JSON string.
// The error names the field, never the value (registry rule).
func requireStrings(keys ...string) forwarding.Constructor {
	return func(fc types.ForwarderConfig) (forwarding.Forwarder, error) {
		var m map[string]any
		if err := json.Unmarshal(fc.Credentials, &m); err != nil {
			return nil, stderr.New("credentials are not a JSON object")
		}
		for _, k := range keys {
			if v, ok := m[k].(string); !ok || v == "" {
				return nil, stderr.New("credentials." + k + " must be a non-empty string")
			}
		}
		return nopForwarder{}, nil
	}
}

func bindingsDB(t *testing.T) *sqlite.Service {
	t.Helper()
	cfg := config.DefaultConfig(t.TempDir())
	cfg.Datastore.Path = ":memory:"
	cfg.Logging.FileLogging = false
	cfgSvc := config.New(cfg)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatal(err)
	}
	logSvc := &logging.Service{ConfigService: cfgSvc, WorkingDir: cfgSvc.WorkingDir()}
	if err := logSvc.Initialize(); err != nil {
		t.Fatal(err)
	}
	db := &sqlite.Service{ConfigService: cfgSvc, LoggerService: logSvc}
	if err := db.Initialize(); err != nil {
		t.Fatal(err)
	}
	db.DatabaseConfig = &types.DatastoreConfig{Driver: "sqlite", Path: ":memory:", MaxOpenConns: 1, MaxIdleConns: 1, ContextTimeout: 10, TransactionContextTimeout: 10}
	db.SetMigrationSets(sqlite.MigrationSetLog)
	if err := db.Open(); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(); _ = logSvc.Close() })
	return db
}

func twoLogbooks(t *testing.T, db *sqlite.Service) (int64, int64) {
	t.Helper()
	ctx := context.Background()
	a, err := db.InsertLogbookWithContext(ctx, types.Logbook{Name: "Main", Callsign: "M0ABC"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.InsertLogbookWithContext(ctx, types.Logbook{Name: "Second", Callsign: "M0XYZ"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnsureArchiveIdentityWithContext(ctx, a); err != nil {
		t.Fatal(err)
	}
	return a, b
}

func stationCfg() config.Config {
	return config.Config{Forwarders: []types.ForwarderConfig{
		{Name: "qrz", Type: "bindview-qrz", Enabled: true},
		{Name: "cloud", Type: "bindview-cloud", Label: "Shack cloud", Enabled: true, Credentials: json.RawMessage(`{"url":"https://c","token":"t"}`)},
	}}
}

func destView(t *testing.T, v types.ArchiveBindingsView, typ string) types.DestinationBindingView {
	t.Helper()
	for _, d := range v.Destinations {
		if d.Type == typ {
			return d
		}
	}
	t.Fatalf("no destination %q in the view", typ)
	return types.DestinationBindingView{}
}

func bindingCode(err error) string {
	var re *RequestError
	if stderr.As(err, &re) {
		return re.Code
	}
	return ""
}

func TestBindings_ViewAggregatesOverLiveLogbooksAndMasks(t *testing.T) {
	db := bindingsDB(t)
	a, b := twoLogbooks(t, db)
	ctx := context.Background()
	if err := db.UpsertLogbookDestinationsWithContext(ctx, []sqlite.DestinationUpsert{
		{LogbookID: a, Destination: "bindview-qrz", ForwarderName: "qrz", Enabled: true, Credentials: json.RawMessage(`{"api_key":"SECRET"}`)},
	}); err != nil {
		t.Fatal(err)
	}
	legacy := &types.QsoArchiveConfig{ID: "x", Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy}
	v, err := BindingsView(ctx, db, stationCfg(), legacy, nil)
	if err != nil {
		t.Fatal(err)
	}
	q := destView(t, v, "bindview-qrz")
	if q.State != BindingStateMixed || len(q.Logbooks) != 2 || !q.Account.Configured {
		t.Fatalf("qrz view = %+v; want mixed over two logbooks with a configured account", q)
	}
	if !q.Logbooks[0].Bound || !q.Logbooks[0].Enabled || q.Logbooks[0].ForwarderName != "qrz" || len(q.Logbooks[0].CredentialsSet) != 1 || q.Logbooks[0].CredentialsSet[0] != "api_key" {
		t.Fatalf("logbook a row = %+v", q.Logbooks[0])
	}
	if q.Logbooks[1].Bound || q.Logbooks[1].Enabled {
		t.Fatalf("logbook b row = %+v; want unbound (absent counts as off)", q.Logbooks[1])
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), `"t"`) {
		t.Fatalf("the view carries a credential value: %s", raw)
	}
	c := destView(t, v, "bindview-cloud")
	if c.State != BindingStateOff || !c.Account.Configured || c.Account.Label != "Shack cloud" || len(c.Account.FieldsSet) != 2 {
		t.Fatalf("cloud view = %+v", c)
	}
	// restart_required: the table differs from an empty start fingerprint; equal to its own.
	if !v.RestartRequired {
		t.Fatal("restart_required false although the daemon started with no bindings")
	}
	list, _ := db.ListLogbookDestinationsWithContext(ctx)
	v2, _ := BindingsView(ctx, db, stationCfg(), legacy, FingerprintBindings(list))
	if v2.RestartRequired {
		t.Fatal("restart_required true although the table equals the start snapshot")
	}
	_ = b
}

func TestBindings_ApplyValidatesWholeCandidateAndWritesAtomically(t *testing.T) {
	db := bindingsDB(t)
	a, b := twoLogbooks(t, db)
	ctx := context.Background()
	legacy := &types.QsoArchiveConfig{ID: "x", Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy}
	cfg := stationCfg()

	// One row valid, one enabled without its required key → nothing written.
	_, err := applyBindings(ctx, nil, db, cfg, legacy, nil, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
		Type: "bindview-qrz", Logbooks: []types.LogbookBindingEdit{
			{LogbookID: a, Enabled: true, Credentials: map[string]string{"api_key": "k1"}},
			{LogbookID: b, Enabled: true},
		}}}})
	if bindingCode(err) != "binding_field_required" || !strings.Contains(err.Error(), "Second") || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("err = %v; want binding_field_required naming the logbook and field", err)
	}
	if rows, _ := db.ListLogbookDestinationsWithContext(ctx); len(rows) != 0 {
		t.Fatalf("a refused PUT wrote rows: %+v", rows)
	}
	// The aggregate write: both rows on, both keys given → both rows created,
	// the new ones named `<type>.<uuid>`.
	v, err := applyBindings(ctx, nil, db, cfg, legacy, nil, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
		Type: "bindview-qrz", Logbooks: []types.LogbookBindingEdit{
			{LogbookID: a, Enabled: true, Credentials: map[string]string{"api_key": "k1"}},
			{LogbookID: b, Enabled: true, Credentials: map[string]string{"api_key": "k2"}},
		}}}})
	if err != nil {
		t.Fatal(err)
	}
	q := destView(t, v, "bindview-qrz")
	if q.State != BindingStateOn || !strings.HasPrefix(q.Logbooks[1].ForwarderName, "bindview-qrz.") {
		t.Fatalf("after the aggregate write: %+v", q)
	}
	// Merge-on-PUT: a blank keeps the key; turning one row off makes it mixed;
	// a station key typed into a binding is refused.
	v, err = applyBindings(ctx, nil, db, cfg, legacy, nil, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
		Type: "bindview-qrz", Logbooks: []types.LogbookBindingEdit{{LogbookID: b, Enabled: false, Credentials: map[string]string{"api_key": ""}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	q = destView(t, v, "bindview-qrz")
	if q.State != BindingStateMixed || q.Logbooks[1].Enabled || len(q.Logbooks[1].CredentialsSet) != 1 {
		t.Fatalf("after disabling b with a blank key: %+v", q)
	}
	_, err = applyBindings(ctx, nil, db, cfg, legacy, nil, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
		Type: "bindview-cloud", Logbooks: []types.LogbookBindingEdit{{LogbookID: a, Enabled: true, Credentials: map[string]string{"url": "https://attacker"}}}}}})
	if bindingCode(err) != "invalid_field_value" {
		t.Fatalf("station key on a binding: %v; want invalid_field_value", err)
	}
	// credentials_clear: refused on a row that ends enabled, honoured when disabled.
	_, err = applyBindings(ctx, nil, db, cfg, legacy, nil, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
		Type: "bindview-qrz", Logbooks: []types.LogbookBindingEdit{{LogbookID: a, Enabled: true, CredentialsClear: []string{"api_key"}}}}}})
	if bindingCode(err) != "binding_clear_requires_disabled" {
		t.Fatalf("clear on an enabled row: %v", err)
	}
	v, err = applyBindings(ctx, nil, db, cfg, legacy, nil, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
		Type: "bindview-qrz", Logbooks: []types.LogbookBindingEdit{{LogbookID: a, Enabled: false, CredentialsClear: []string{"api_key"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if q = destView(t, v, "bindview-qrz"); q.Logbooks[0].Enabled || len(q.Logbooks[0].CredentialsSet) != 0 || q.Logbooks[0].ForwarderName != "bindview-qrz."+q.Logbooks[0].LogbookUUID {
		t.Fatalf("after disable-and-clear: %+v", q.Logbooks[0])
	}
}

func TestBindings_EnableRefusals(t *testing.T) {
	db := bindingsDB(t)
	a, _ := twoLogbooks(t, db)
	ctx := context.Background()
	// No station account for the type → the switch says why and a PUT refuses.
	noAccount := config.Config{}
	v, _ := BindingsView(ctx, db, noAccount, nil, nil)
	if d := destView(t, v, "bindview-qrz"); d.Reason == "" || d.Account.Configured {
		t.Fatalf("no-account destination = %+v; want a reason", d)
	}
	_, err := applyBindings(ctx, nil, db, noAccount, nil, nil, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
		Type: "bindview-qrz", Logbooks: []types.LogbookBindingEdit{{LogbookID: a, Enabled: true, Credentials: map[string]string{"api_key": "k"}}}}}})
	if bindingCode(err) != "binding_not_enableable" {
		t.Fatalf("enable without an account: %v", err)
	}
	// A cloud account missing its token is not configured either.
	half := config.Config{Forwarders: []types.ForwarderConfig{{Name: "cloud", Type: "bindview-cloud", Credentials: json.RawMessage(`{"url":"https://c"}`)}}}
	v, _ = BindingsView(ctx, db, half, nil, nil)
	if d := destView(t, v, "bindview-cloud"); d.Account.Configured || d.Reason == "" {
		t.Fatalf("half-configured account = %+v", d)
	}
	// Disabling never needs an account.
	if _, err := applyBindings(ctx, nil, db, noAccount, nil, nil, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
		Type: "bindview-qrz", Logbooks: []types.LogbookBindingEdit{{LogbookID: a, Enabled: false}}}}}); err != nil {
		t.Fatalf("disable without an account: %v", err)
	}
}

// ADR 0082 part 7 remnant: SM Cloud binds on the adopted archive only until 5F.
func TestBindings_SmcloudOnlyOnTheAdoptedArchiveUntil5F(t *testing.T) {
	db := bindingsDB(t)
	a, _ := twoLogbooks(t, db)
	ctx := context.Background()
	cfg := config.Config{Forwarders: []types.ForwarderConfig{{Name: "smcloud", Type: "smcloud", Enabled: true, Credentials: json.RawMessage(`{"url":"https://c","token":"t"}`)}}}
	managed := &types.QsoArchiveConfig{ID: "m", Label: "Contest", Ownership: types.QsoArchiveOwnershipManaged}
	v, _ := BindingsView(ctx, db, cfg, managed, nil)
	if d := destView(t, v, "smcloud"); !strings.Contains(d.Reason, "adopted") {
		t.Fatalf("smcloud on a managed archive = %+v; want the identity reason", d)
	}
	_, err := applyBindings(ctx, nil, db, cfg, managed, nil, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
		Type: "smcloud", Logbooks: []types.LogbookBindingEdit{{LogbookID: a, Enabled: true}}}}})
	if bindingCode(err) != "binding_not_enableable" {
		t.Fatalf("smcloud enable on a managed archive: %v", err)
	}
	legacy := &types.QsoArchiveConfig{ID: "h", Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy}
	if _, err := applyBindings(ctx, nil, db, cfg, legacy, nil, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
		Type: "smcloud", Logbooks: []types.LogbookBindingEdit{{LogbookID: a, Enabled: true}}}}}); err != nil {
		t.Fatalf("smcloud enable on the adopted archive: %v (its only logbook-scoped field is clearable)", err)
	}
}

// The port's own refusals: no database wired, an unknown id, an inactive
// archive — before any view or write.
func TestManager_BindingsPortRefusals(t *testing.T) {
	m, cfgSvc, _ := testManager(t)
	ctx := context.Background()
	if _, err := m.Bindings(ctx, "x"); bindingCode(err) != "bindings_unavailable" {
		t.Fatalf("no database wired: %v", err)
	}
	db := bindingsDB(t)
	a, _ := twoLogbooks(t, db)
	m.SetActiveBindings(db, nil)
	if _, err := cfgSvc.Update(func(c *config.Config) error {
		c.QsoArchives = []types.QsoArchiveConfig{
			{ID: "019fd5c5-efcc-7193-be4f-1fee532ee3a1", Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: "/x/home.db"},
			{ID: "019fd5c5-efcc-7193-be4f-1fee532ee3b2", Label: "Contest", Ownership: types.QsoArchiveOwnershipManaged},
		}
		c.ActiveQsoArchiveID = "019fd5c5-efcc-7193-be4f-1fee532ee3a1"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Bindings(ctx, "019fd5c5-efcc-7193-be4f-000000000000"); bindingCode(err) != "archive_not_found" {
		t.Fatalf("unknown id: %v", err)
	}
	if _, err := m.ApplyBindings(ctx, "019fd5c5-efcc-7193-be4f-1fee532ee3b2", types.ArchiveBindingsRequest{}); bindingCode(err) != "archive_not_active" {
		t.Fatalf("inactive archive: %v", err)
	}
	v, err := m.Bindings(ctx, "019fd5c5-efcc-7193-be4f-1fee532ee3a1")
	if err != nil || v.ArchiveLabel != "Home" || v.RestartRequired {
		t.Fatalf("active archive view = %+v (%v)", v, err)
	}
	// A PUT on the active archive writes and reports restart_required.
	v, err = m.ApplyBindings(ctx, "019fd5c5-efcc-7193-be4f-1fee532ee3a1", types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
		Type: "bindview-qrz", Logbooks: []types.LogbookBindingEdit{{LogbookID: a, Enabled: false}}}}})
	if err != nil || !v.RestartRequired {
		t.Fatalf("apply on the active archive = %+v (%v); want restart_required", v, err)
	}
}

// ---- 5D review (2026-09-25) ----

// P1: enabling builds the exact candidate. A DISABLED seeded binding whose
// stored key is not a string is supported as it is (the seed copies a
// disabled entry's logbook-scoped keys verbatim and never constructs it), but
// it cannot be turned on as it is: the PUT is refused whole — together with a
// valid row in the same request — and no row changes.
func TestBindings_EnablingBuildsTheExactCandidate(t *testing.T) {
	db := bindingsDB(t)
	a, b := twoLogbooks(t, db)
	ctx := context.Background()
	res, err := db.SeedLogbookDestinationsWithContext(ctx, []sqlite.DestinationSeed{{
		Destination: "bindview-qrz", LegacyName: "qrz", Enabled: false, Credentials: json.RawMessage(`{"api_key":123}`),
	}})
	if err != nil || !res.Seeded {
		t.Fatalf("seeding a disabled malformed entry: %+v (%v); want it supported", res, err)
	}
	before, err := db.ListLogbookDestinationsWithContext(ctx)
	if err != nil || len(before) != 2 {
		t.Fatalf("seeded rows = %+v (%v)", before, err)
	}
	legacy := &types.QsoArchiveConfig{ID: "x", Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy}
	_, err = applyBindings(ctx, nil, db, stationCfg(), legacy, nil, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
		Type: "bindview-qrz", Logbooks: []types.LogbookBindingEdit{
			{LogbookID: b, Enabled: false}, // a valid, unrelated edit in the same request
			{LogbookID: a, Enabled: true},  // the malformed key, as stored
		}}}})
	if bindingCode(err) != "binding_unusable" || !strings.Contains(err.Error(), "Main") {
		t.Fatalf("enabling the malformed binding: %v; want binding_unusable naming the logbook", err)
	}
	if strings.Contains(err.Error(), "123") {
		t.Fatalf("the refusal echoes a credential value: %v", err)
	}
	after, err := db.ListLogbookDestinationsWithContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%+v", after) != fmt.Sprintf("%+v", before) {
		t.Fatalf("a refused PUT changed rows:\nbefore %+v\nafter  %+v", before, after)
	}
	// Retyping a valid key while enabling builds and writes.
	v, err := applyBindings(ctx, nil, db, stationCfg(), legacy, nil, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
		Type: "bindview-qrz", Logbooks: []types.LogbookBindingEdit{{LogbookID: a, Enabled: true, Credentials: map[string]string{"api_key": "GOOD"}}}}}})
	if err != nil {
		t.Fatalf("enabling with a valid key: %v", err)
	}
	if q := destView(t, v, "bindview-qrz"); !q.Logbooks[0].Enabled {
		t.Fatalf("row after the valid enable = %+v", q.Logbooks[0])
	}
}

// P2: credentials_clear accepts only the type's logbook-scoped keys — a
// station-scoped key or an undeclared one is refused, never "cleared" with a
// success it cannot perform; a key both typed and cleared is contradictory.
// Nothing is written on any refusal.
func TestBindings_ClearAcceptsOnlyLogbookScopedKeys(t *testing.T) {
	db := bindingsDB(t)
	a, _ := twoLogbooks(t, db)
	ctx := context.Background()
	legacy := &types.QsoArchiveConfig{ID: "x", Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy}
	if err := db.UpsertLogbookDestinationsWithContext(ctx, []sqlite.DestinationUpsert{
		{LogbookID: a, Destination: "bindview-cloud", ForwarderName: "cloud", Enabled: false, Credentials: json.RawMessage(`{"logbook":"home"}`)},
	}); err != nil {
		t.Fatal(err)
	}
	before, _ := db.ListLogbookDestinationsWithContext(ctx)
	for name, edit := range map[string]types.LogbookBindingEdit{
		"station-scoped key": {LogbookID: a, Enabled: false, CredentialsClear: []string{"url"}},
		"undeclared key":     {LogbookID: a, Enabled: false, CredentialsClear: []string{"bogus"}},
		"typed and cleared":  {LogbookID: a, Enabled: false, Credentials: map[string]string{"logbook": "x"}, CredentialsClear: []string{"logbook"}},
	} {
		_, err := applyBindings(ctx, nil, db, stationCfg(), legacy, nil, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
			Type: "bindview-cloud", Logbooks: []types.LogbookBindingEdit{edit}}}})
		if bindingCode(err) != "invalid_field_value" {
			t.Fatalf("%s: %v; want invalid_field_value", name, err)
		}
		if after, _ := db.ListLogbookDestinationsWithContext(ctx); fmt.Sprintf("%+v", after) != fmt.Sprintf("%+v", before) {
			t.Fatalf("%s: a refused PUT changed rows: %+v", name, after)
		}
	}
	// The declared logbook-scoped key clears.
	v, err := applyBindings(ctx, nil, db, stationCfg(), legacy, nil, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
		Type: "bindview-cloud", Logbooks: []types.LogbookBindingEdit{{LogbookID: a, Enabled: false, CredentialsClear: []string{"logbook"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if c := destView(t, v, "bindview-cloud"); len(c.Logbooks[0].CredentialsSet) != 0 {
		t.Fatalf("after clearing logbook: %+v", c.Logbooks[0])
	}
}

// gatedDB holds every bindings write at a gate until the test releases it, so
// the test can drive two PUTs into the same interleaving deterministically.
type gatedDB struct {
	*sqlite.Service
	arrived chan struct{}
	release chan struct{}
}

func (g *gatedDB) UpsertLogbookDestinationsWithContext(ctx context.Context, rows []sqlite.DestinationUpsert) error {
	g.arrived <- struct{}{}
	<-g.release
	return g.Service.UpsertLogbookDestinationsWithContext(ctx, rows)
}

func within(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// P1: two PUTs editing DISJOINT fields of one binding at once both survive.
// PUT A (email) is held at its write; PUT B (password) is then started. If B
// could read the stored row while A's write is pending, it would merge onto
// the OLD blob and the last writer would restore the other field's old value.
// The window below only lets the unserialized interleaving happen; the
// serialized path passes whatever it waits (B cannot read until A is done).
func TestManager_ConcurrentDisjointFieldEditsBothSurvive(t *testing.T) {
	m, cfgSvc, _ := testManager(t)
	db := bindingsDB(t)
	a, _ := twoLogbooks(t, db)
	ctx := context.Background()
	const home = "019fd5c5-efcc-7193-be4f-1fee532ee3a1"
	if _, err := cfgSvc.Update(func(c *config.Config) error {
		c.QsoArchives = []types.QsoArchiveConfig{{ID: home, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: "/x/home.db"}}
		c.ActiveQsoArchiveID = home
		c.Forwarders = []types.ForwarderConfig{{Name: "club", Type: "bindview-club", Enabled: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertLogbookDestinationsWithContext(ctx, []sqlite.DestinationUpsert{
		{LogbookID: a, Destination: "bindview-club", ForwarderName: "club", Enabled: true, Credentials: json.RawMessage(`{"email":"old@example.org","password":"OLDPW"}`)},
	}); err != nil {
		t.Fatal(err)
	}
	g := &gatedDB{Service: db, arrived: make(chan struct{}), release: make(chan struct{})}
	m.SetActiveBindings(g, nil)
	edit := func(field, value string) types.ArchiveBindingsRequest {
		return types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
			Type: "bindview-club", Logbooks: []types.LogbookBindingEdit{{LogbookID: a, Enabled: true, Credentials: map[string]string{field: value}}}}}}
	}
	errA, errB := make(chan error, 1), make(chan error, 1)
	go func() { _, err := m.ApplyBindings(ctx, home, edit("email", "new@example.org")); errA <- err }()
	within(t, g.arrived, "PUT A at its write")
	go func() { _, err := m.ApplyBindings(ctx, home, edit("password", "NEWPW")); errB <- err }()
	bEarly := false
	select {
	case <-g.arrived:
		bEarly = true // B read and merged while A's write was still pending
	case <-time.After(300 * time.Millisecond):
	}
	g.release <- struct{}{}
	if !bEarly {
		within(t, g.arrived, "PUT B at its write")
	}
	g.release <- struct{}{}
	for _, ch := range []chan error{errA, errB} {
		select {
		case err := <-ch:
			if err != nil {
				t.Fatalf("PUT: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for a PUT")
		}
	}
	rows, err := db.ListLogbookDestinationsWithContext(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %+v (%v)", rows, err)
	}
	var creds map[string]string
	if err := json.Unmarshal(rows[0].Credentials, &creds); err != nil {
		t.Fatal(err)
	}
	if creds["email"] != "new@example.org" || creds["password"] != "NEWPW" {
		t.Fatalf("after two disjoint concurrent edits: email %q password %q; want both new values", creds["email"], creds["password"])
	}
}
