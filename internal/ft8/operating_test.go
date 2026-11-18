package ft8

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Operating ownership (W-0027 C4a). The rule under test: with ownership required, an FT8
// command that can start, resume or alter operation is accepted only from the lifetime
// that is current, and the check is made under the gate a replacement takes. A disarm is
// always accepted. With ownership not required, nothing changes from before C4.

const (
	lifeA = "AAAAAAAAAAAAAAAAAAAAAA"
	lifeB = "BBBBBBBBBBBBBBBBBBBBBB"
)

// operatingCommand is one gated FT8 command, called as a given lifetime.
type operatingCommand struct {
	name string
	call func(o Operating) error
}

// gatedCommands is every FT8 command the rule covers: nineteen service methods behind
// fourteen routes. The profile claim and the disarm are the two that are not gated.
func gatedCommands() []operatingCommand {
	const slot = "2026-06-10T14:30:00Z"
	return []operatingCommand{
		{"arm", func(o Operating) error { return o.ArmTx(true) }},
		{"send", func(o Operating) error { return o.TransmitNext("CQ G0XYZ IO91", 1500) }},
		{"qso start", func(o Operating) error {
			return o.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", slot, 1500, 14.074, 1, false, "")
		}},
		{"qso start, addressee", func(o Operating) error {
			return o.StartQsoAddressee("G0XYZ", "IO91", "VK9XY", slot, 1500, 14.074, 1, false, "")
		}},
		{"qso start fd", func(o Operating) error {
			return o.StartQsoFd("G0XYZ", "K1ABC", "FN42", -10, slot, 1500, 14.074, 1, false)
		}},
		{"qso start t4", func(o Operating) error {
			return o.StartQsoT4("G0XYZ", "K1ABC", "FN42", -10, slot, 1500, 14.074, 1, false)
		}},
		{"cq start", func(o Operating) error {
			return o.StartCallCq("G0XYZ", "IO91", 1500, 14.074, "", "", 1, "")
		}},
		{"work", func(o Operating) error {
			return o.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -10, slot, 1500, 14.074, 1, false, "")
		}},
		{"work fd", func(o Operating) error {
			return o.StartWorkCallerFd("G0XYZ", "K1ABC", "FN42", "1D", "EMA", -10, slot, 1500, 14.074, 1, false)
		}},
		{"work t4", func(o Operating) error {
			return o.StartWorkCallerT4("G0XYZ", "K1ABC", "FN42", -10, slot, 1500, 14.074, 1, false)
		}},
		{"path", func(o Operating) error { return o.SetExchangePath("long") }},
		{"abandon", func(o Operating) error { return o.AbandonQso() }},
		{"autowork stop", func(o Operating) error { return o.StopAutoWorkRun() }},
		{"skip", func(o Operating) error { return o.SetQsoSkip(true) }},
		{"next", func(o Operating) error { return o.NextAnswerer() }},
		{"pick", func(o Operating) error { return o.PickAnswerer("K1ABC") }},
		{"bag", func(o Operating) error { return o.BagAnswerer("K1ABC") }},
		{"unbag", func(o Operating) error { return o.UnbagAnswerer("K1ABC") }},
		{"resume", func(o Operating) error { return o.ResumeDrain() }},
	}
}

func newOwnedService(t *testing.T, current string) *Service {
	t.Helper()
	s := newBareTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	// Configured, so the two Field Day starts reach the gate: a request that is refused
	// for its own content before the gate is refused for that, whoever sent it.
	s.cfg.FieldDay = &types.Ft8FieldDayConfig{Class: "1D", Section: "EMA"}
	s.RequireOperatingLifetime()
	if current != "" {
		s.PublishOperating(current)
		// These tests are about WHO may operate. Presence (C4b) is its own rule and has
		// its own tests, so here the current lifetime simply has its stream open.
		s.presence.open(current)
	}
	t.Cleanup(func() { _ = s.ArmTx(false) })
	return s
}

func (s *Service) armedNow() bool {
	s.txMu.Lock()
	defer s.txMu.Unlock()
	return s.txArmed
}

// Every gated command is refused for a caller with no lifetime (a tool, or a shell from
// before C4), for a lifetime that is not the current one, and when no lifetime is current.
func TestOperating_EveryGatedCommandNeedsTheCurrentLifetime(t *testing.T) {
	for _, c := range gatedCommands() {
		t.Run(c.name, func(t *testing.T) {
			s := newOwnedService(t, lifeA)
			require.ErrorIs(t, c.call(s.OperatingAs("")), ErrLifetimeNotCurrent, "no lifetime")
			require.ErrorIs(t, c.call(s.OperatingAs(lifeB)), ErrLifetimeNotCurrent, "another lifetime")
			require.NotErrorIs(t, c.call(s.OperatingAs(lifeA)), ErrLifetimeNotCurrent, "the current lifetime")

			none := newOwnedService(t, "")
			require.ErrorIs(t, c.call(none.OperatingAs(lifeA)), ErrLifetimeNotCurrent, "no lifetime is current")
		})
	}
}

// A refusal is not only an error: the command had no effect.
func TestOperating_ARefusedCommandChangesNothing(t *testing.T) {
	s := newOwnedService(t, lifeA)

	require.ErrorIs(t, s.OperatingAs(lifeB).ArmTx(true), ErrLifetimeNotCurrent)
	require.False(t, s.armedNow(), "a refused arm armed TX")

	require.ErrorIs(t, s.OperatingAs(lifeB).SetExchangePath("long"), ErrLifetimeNotCurrent)
	s.txMu.Lock()
	path := s.exchPath
	s.txMu.Unlock()
	require.NotEqual(t, normalizeAntPath("long"), path, "a refused path selection was stored")

	// Under the current lifetime's arm, another lifetime's start does not begin a contact.
	require.NoError(t, s.OperatingAs(lifeA).ArmTx(true))
	require.ErrorIs(t,
		s.OperatingAs(lifeB).StartCallCq("G0XYZ", "IO91", 1500, 14.074, "", "", 1, ""),
		ErrLifetimeNotCurrent)
	require.False(t, s.seq.Active(), "a refused start began a run under the current lifetime's arm")
	require.True(t, s.armedNow(), "a refused start disturbed the arm")
}

// With ownership not required (the check off), a command is accepted whoever asks, and
// the commands that took no gate before C4 still take none.
func TestOperating_NotRequired_NothingChanges(t *testing.T) {
	s := newBareTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	t.Cleanup(func() { _ = s.ArmTx(false) })

	for _, c := range gatedCommands() {
		require.NotErrorIs(t, c.call(s.OperatingAs("")), ErrLifetimeNotCurrent, c.name)
		require.NotErrorIs(t, c.call(s.OperatingAs(lifeB)), ErrLifetimeNotCurrent, c.name)
	}
	_ = s.ArmTx(false)
	require.NoError(t, s.ArmTx(true), "the plain arm, with no subscriber and no lifetime")
	require.True(t, s.armedNow())

	// Held by the test: a command that took the gate would never return. Released by
	// defer, so that a failure here ends the test instead of hanging its clean-up.
	s.seqGate.Lock()
	defer s.seqGate.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		setExchangePath(t, s, "long")
		stopAutoWorkRun(t, s)
		_ = s.SetQsoSkip(true)
		_ = s.NextAnswerer()
		_ = s.PickAnswerer("K1ABC")
		_ = s.BagAnswerer("K1ABC")
		_ = s.UnbagAnswerer("K1ABC")
		_ = s.ResumeDrain()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("with ownership not required, a command that took no gate before C4 now waits on it")
	}
}

// A replacement seals, stops, and only then publishes: the old lifetime's operation is
// gone before the new one is current, and the old one can do nothing afterwards.
func TestOperating_RetireStopsOperationAndSealsTheOldLifetime(t *testing.T) {
	s := newOwnedService(t, lifeA)
	a := s.OperatingAs(lifeA)
	require.NoError(t, a.ArmTx(true))
	require.NoError(t, a.StartCallCq("G0XYZ", "IO91", 1500, 14.074, "", "", 1, ""))
	require.True(t, s.seq.Active())

	s.RetireOperating()
	require.False(t, s.armedNow(), "retiring left TX armed")
	require.False(t, s.seq.Active(), "retiring left a run active")
	require.False(t, s.txInFlightNow(), "retiring returned before the transmission had ended")

	// Sealed: between the stop and the publish no lifetime operates, the old one included.
	require.ErrorIs(t, a.ArmTx(true), ErrLifetimeNotCurrent, "the old lifetime armed again after the stop")
	require.ErrorIs(t, s.OperatingAs(lifeB).ArmTx(true), ErrLifetimeNotCurrent, "the new lifetime armed before it was published")

	s.PublishOperating(lifeB)
	s.presence.open(lifeB)
	b := s.OperatingAs(lifeB)
	require.NoError(t, b.ArmTx(true))

	// codex-125 finding 2: a command of the old lifetime that arrives after the new one
	// has armed does not act under that arm.
	require.ErrorIs(t, a.StartCallCq("G0XYZ", "IO91", 1500, 14.074, "", "", 1, ""), ErrLifetimeNotCurrent)
	require.ErrorIs(t, a.TransmitNext("CQ G0XYZ IO91", 1500), ErrLifetimeNotCurrent)
	require.ErrorIs(t, a.AbandonQso(), ErrLifetimeNotCurrent)
	require.False(t, s.seq.Active())
	require.False(t, s.txInFlightNow())
	require.True(t, s.armedNow(), "the old lifetime's refused commands disturbed the new arm")
}

// Retiring with nothing armed is a no-op that still seals.
func TestOperating_RetireWithNothingRunning(t *testing.T) {
	s := newOwnedService(t, lifeA)
	s.RetireOperating()
	require.False(t, s.armedNow())
	require.ErrorIs(t, s.OperatingAs(lifeA).ArmTx(true), ErrLifetimeNotCurrent)
}

// The old lifetime's starts race a replacement. Whatever the interleaving, once the
// replacement has returned nothing of the old lifetime is running, and nothing of it
// starts afterwards: the check and the commit are one step under the gate.
func TestOperating_OldStartsRacingAReplacementNeverSurviveIt(t *testing.T) {
	for i := 0; i < 200; i++ {
		s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
		s.RequireOperatingLifetime()
		s.PublishOperating(lifeA)
		s.presence.open(lifeA)
		a := s.OperatingAs(lifeA)
		require.NoError(t, a.ArmTx(true))

		var wg sync.WaitGroup
		start := make(chan struct{})
		for n := 0; n < 4; n++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_ = a.StartCallCq("G0XYZ", "IO91", 1500, 14.074, "", "", 1, "")
				_ = a.ArmTx(true)
			}()
		}
		close(start)
		s.RetireOperating()
		s.PublishOperating(lifeB)
		wg.Wait()

		require.False(t, s.seq.Active(), "an old start survived the replacement (iteration %d)", i)
		require.False(t, s.armedNow(), "an old arm survived the replacement (iteration %d)", i)
		_ = s.ArmTx(false)
	}
}

// heldInitPlayer is a TX player whose Init waits to be let go, and says when it has been
// entered: a way to stand inside an arm while it is being made.
type heldInitPlayer struct {
	*fakeTxPlayer
	entered chan struct{}
	release chan struct{}
}

func (p *heldInitPlayer) Init() error {
	close(p.entered)
	<-p.release
	return p.fakeTxPlayer.Init()
}

// An arm that is half made when the app is replaced does not survive the replacement.
// Here the current lifetime's arm is stopped inside the device set-up and a replacement
// is asked for: it must wait for the arm and then undo it. The arm is held twice over,
// by the gate and by txMu, which the stop also takes; so this test alone does not show
// the gate is held (a reversion proof passed it). The two tests after it do.
func TestOperating_AReplacementUndoesAnArmThatIsBeingMade(t *testing.T) {
	player := &heldInitPlayer{fakeTxPlayer: newFakeTxPlayer(), entered: make(chan struct{}), release: make(chan struct{})}
	s := newTxTestService(&fakeKeyer{}, player, nil)
	s.RequireOperatingLifetime()
	s.PublishOperating(lifeA)
	s.presence.open(lifeA)
	t.Cleanup(func() { _ = s.ArmTx(false) })

	armed := make(chan error, 1)
	go func() { armed <- s.OperatingAs(lifeA).ArmTx(true) }()
	<-player.entered // the arm has passed its check and is inside the device set-up

	retired := make(chan struct{})
	go func() {
		s.RetireOperating()
		close(retired)
	}()
	// The replacement cannot return yet. Give a wrong implementation ample time to
	// return; a right one is only kept waiting by it.
	select {
	case <-retired:
		// It returned while the arm was still being made: let the arm finish and look.
	case <-time.After(500 * time.Millisecond):
	}
	close(player.release)
	require.NoError(t, <-armed)
	<-retired
	require.False(t, s.armedNow(), "an arm that was being made when the app was replaced survived the replacement")
	require.ErrorIs(t, s.OperatingAs(lifeA).ArmTx(true), ErrLifetimeNotCurrent)
}

// The check and the command are one step (codex-125, finding 2). For a command that takes
// no gate of its own the step is operatorGate: it hands back the gate HELD, and nothing
// else can take it until the release it returned is called. A replacement takes this
// gate, so it cannot fit between "allowed" and "done".
func TestOperating_TheGateIsHeldUntilItIsReleased(t *testing.T) {
	s := newOwnedService(t, lifeA)

	release, err := s.operatorGate("test", lifeA)
	require.NoError(t, err)
	if s.seqGate.TryLock() {
		s.seqGate.Unlock() // or this test's own clean-up would wait on it for ever
		t.Fatal("the gate was let go before the command had acted")
	}
	release()
	require.True(t, s.seqGate.TryLock(), "the release did not let the gate go")
	s.seqGate.Unlock()

	// A refusal leaves the gate free.
	_, err = s.operatorGate("test", lifeB)
	require.ErrorIs(t, err, ErrLifetimeNotCurrent)
	require.True(t, s.seqGate.TryLock(), "a refused caller was left holding the gate")
	s.seqGate.Unlock()
}

// ...and every command that uses it keeps the gate to its last statement: the release is
// deferred, never called part-way.
func TestSource_TheGateIsReleasedOnlyByDefer(t *testing.T) {
	src := scanOperatingSource(t)
	require.GreaterOrEqual(t, len(src.usesGate), 9, "too few commands use the gate: the guard is looking at too little")
	for name, deferred := range src.usesGate {
		require.True(t, deferred, "(*Service).%s takes the gate and does not defer its release", name)
	}
	require.Empty(t, src.callsRelease, "the gate's release is called outside a defer: %v", src.callsRelease)
}

// A disarm is always accepted (operator ruling 2026-11-17, question 11), with its stated
// cost: a replaced lifetime's disarm ends what the current one is doing.
func TestOperating_DisarmIsAlwaysAccepted(t *testing.T) {
	for _, who := range []string{"", lifeA} {
		s := newOwnedService(t, lifeB)
		b := s.OperatingAs(lifeB)
		require.NoError(t, b.ArmTx(true))
		require.NoError(t, b.StartCallCq("G0XYZ", "IO91", 1500, 14.074, "", "", 1, ""))

		require.NoError(t, s.OperatingAs(who).ArmTx(false), "a disarm from %q was refused", who)
		require.False(t, s.armedNow())
		require.False(t, s.seq.Active(), "the disarm left the current lifetime's run active")
	}

	// And from nobody current at all.
	none := newOwnedService(t, "")
	require.NoError(t, none.OperatingAs(lifeA).ArmTx(false))
}

// The profile claim is not gated: it changes the profile only on an idle daemon with no
// subscriber, no arm and no session, so it cannot start, resume or alter operation.
func TestOperating_TheProfileClaimIsNotGated(t *testing.T) {
	s := newOwnedService(t, lifeA)
	_, err := s.OperatingAs("").ClaimProfile("ft4")
	require.NotErrorIs(t, err, ErrLifetimeNotCurrent)
	_, err = s.OperatingAs(lifeB).ClaimProfile("ft8")
	require.NotErrorIs(t, err, ErrLifetimeNotCurrent)
}

// Structure, so a command added later cannot skip the rule:
//   - every method of *Service whose first parameter is `who string` checks it, by
//     operatorCheckLocked or operatorGate, with that parameter, or hands it as the first
//     argument to one that does;
//   - every method of Operating passes its own lifetime to such a method, the profile
//     claim excepted by name.
func TestSource_OperatingCommandsCheckTheirCaller(t *testing.T) {
	src := scanOperatingSource(t)

	// A method that takes a caller checks it itself, or hands it, as the first argument,
	// to a method that takes a caller and checks it itself: one step, never a chain.
	require.NotEmpty(t, src.takesWho, "no command takes a caller: the guard is looking at nothing")
	direct := 0
	for name := range src.takesWho {
		ok := src.checksWho[name]
		if ok {
			direct++
		}
		for _, callee := range src.handsOn[name] {
			ok = ok || (src.takesWho[callee] && src.checksWho[callee])
		}
		require.True(t, ok, "(*Service).%s takes a caller and neither checks it nor hands it to a method that does", name)
	}
	require.GreaterOrEqual(t, direct, 16, "too few methods check their caller themselves: the guard is looking at too little")

	require.Len(t, src.operating, len(gatedCommandMethods)+1, "the Operating surface and this test's list differ")
	for name, m := range src.operating {
		if name == "ClaimProfile" {
			require.False(t, m.passesWho, "the profile claim is documented as not gated")
			continue
		}
		require.Contains(t, gatedCommandMethods, name, "Operating.%s is not in this test's list of gated commands", name)
		require.True(t, m.passesWho, "Operating.%s does not pass its lifetime on", name)
		reaches := false
		for _, callee := range m.calls {
			reaches = reaches || src.takesWho[callee]
		}
		require.True(t, reaches, "Operating.%s calls no method that takes a caller", name)
	}
}

// operatingSource is what the guard reads from the package's non-test files.
type operatingSource struct {
	takesWho  map[string]bool     // *Service methods whose first parameter is `who string`
	checksWho map[string]bool     // those that call operatorCheckLocked/operatorGate with it
	handsOn   map[string][]string // method -> the methods it passes `who` to, first
	operating map[string]*operatingMethod
	// usesGate: caller-taking methods that call operatorGate -> whether `defer release()`
	// follows. callsRelease: those that call release() any other way.
	usesGate     map[string]bool
	callsRelease []string
}

type operatingMethod struct {
	passesWho bool
	calls     []string
}

func scanOperatingSource(t *testing.T) *operatingSource {
	t.Helper()
	src := &operatingSource{
		takesWho: map[string]bool{}, checksWho: map[string]bool{},
		handsOn: map[string][]string{}, operating: map[string]*operatingMethod{},
		usesGate: map[string]bool{},
	}
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		code, err := os.ReadFile(name)
		require.NoError(t, err)
		f, err := parser.ParseFile(fset, name, code, 0)
		require.NoError(t, err)
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || fn.Body == nil {
				continue
			}
			switch recvName(fn.Recv.List[0].Type) {
			case "Service":
				if firstParamIsWho(fn) {
					src.takesWho[fn.Name.Name] = true
					src.scanServiceMethod(fn)
					src.scanGateUse(fn)
				}
			case "Operating":
				src.operating[fn.Name.Name] = scanOperatingMethod(fn)
			}
		}
	}
	return src
}

// scanServiceMethod records whether a caller-taking method checks its caller, and which
// methods it hands the caller to.
func (src *operatingSource) scanServiceMethod(fn *ast.FuncDecl) {
	for _, call := range selectorCalls(fn) {
		sel := call.Fun.(*ast.SelectorExpr)
		if (sel.Sel.Name == "operatorCheckLocked" || sel.Sel.Name == "operatorGate") && hasIdentArg(call, "who") {
			src.checksWho[fn.Name.Name] = true
		}
		if len(call.Args) == 0 {
			continue
		}
		if id, ok := call.Args[0].(*ast.Ident); ok && id.Name == "who" {
			src.handsOn[fn.Name.Name] = append(src.handsOn[fn.Name.Name], sel.Sel.Name)
		}
	}
}

// scanGateUse records, for a method that calls operatorGate, whether its release is
// deferred, and any call of release() that is not the deferred one.
func (src *operatingSource) scanGateUse(fn *ast.FuncDecl) {
	uses, deferred, plain := false, 0, 0
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.DeferStmt:
			if id, ok := x.Call.Fun.(*ast.Ident); ok && id.Name == "release" {
				deferred++
				return false // its call is the deferred one, not a plain one
			}
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "operatorGate" {
				uses = true
			}
			if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "release" {
				plain++
			}
		}
		return true
	})
	if !uses {
		return
	}
	src.usesGate[fn.Name.Name] = deferred == 1
	if plain > 0 {
		src.callsRelease = append(src.callsRelease, fn.Name.Name)
	}
}

func scanOperatingMethod(fn *ast.FuncDecl) *operatingMethod {
	m := &operatingMethod{}
	for _, call := range selectorCalls(fn) {
		m.calls = append(m.calls, call.Fun.(*ast.SelectorExpr).Sel.Name)
		if len(call.Args) > 0 && isSelector(call.Args[0], "o", "who") {
			m.passesWho = true
		}
	}
	return m
}

// selectorCalls is every call in fn of the form x.y(...).
func selectorCalls(fn *ast.FuncDecl) []*ast.CallExpr {
	var out []*ast.CallExpr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if _, ok := call.Fun.(*ast.SelectorExpr); ok {
				out = append(out, call)
			}
		}
		return true
	})
	return out
}

// gatedCommandMethods names the Operating methods that are gated, one per service method.
var gatedCommandMethods = []string{
	"ArmTx", "TransmitNext",
	"StartQso", "StartQsoAddressee", "StartQsoFd", "StartQsoT4", "StartCallCq",
	"StartWorkCaller", "StartWorkCallerFd", "StartWorkCallerT4",
	"SetExchangePath", "AbandonQso", "StopAutoWorkRun", "SetQsoSkip",
	"NextAnswerer", "PickAnswerer", "BagAnswerer", "UnbagAnswerer", "ResumeDrain",
}

func recvName(e ast.Expr) string {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func firstParamIsWho(fn *ast.FuncDecl) bool {
	if fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
		return false
	}
	p := fn.Type.Params.List[0]
	id, ok := p.Type.(*ast.Ident)
	return ok && id.Name == "string" && len(p.Names) > 0 && p.Names[0].Name == "who"
}

func hasIdentArg(call *ast.CallExpr, name string) bool {
	for _, a := range call.Args {
		if id, ok := a.(*ast.Ident); ok && id.Name == name {
			return true
		}
	}
	return false
}

func isSelector(e ast.Expr, x, sel string) bool {
	s, ok := e.(*ast.SelectorExpr)
	if !ok || s.Sel.Name != sel {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	return ok && id.Name == x
}
