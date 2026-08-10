package config

/*
   §4.2 station-profiles — the declaration-validation half of the acceptance
   set (PR6 + rulings O1/O2/O3, operator 2026-08-10; the evidence-side
   criteria PR1–PR9 live in internal/evidence/profiles_test.go).

   PR6: a band claimed twice is refused before anything pins — the finding
   names the band and both antennas. Boot behavior is already the config
   contract (error findings are fatal at Load, smd exits; PUT rejects 400
   inside the atomic update), so validation findings ARE the refusal.

   Rulings encoded here:
   - O1: lineage identity is the trimmed, case-sensitive name → duplicate
     post-trim names reject; an empty/whitespace name rejects.
   - O1a: a band duplicated WITHIN one antenna rejects (silent
     normalization would conceal a typo).
   - O1b: an empty band list rejects — it declares nothing; retirement is
     removing the entry.
   - Bands are ADIF band tokens (enums/bands vocabulary): unknown tokens
     reject.
   - O2b: locator is optional; when supplied it must be a valid Maidenhead
     locator (canonicalization is the evidence side's job — validation
     accepts any case).
   - O3: height is optional; when supplied it must be ≥ 0 (0 is a real
     value: a ground-mounted vertical). No upper bound — an unusual
     installation is physically honest. (NaN/Inf cannot arrive via JSON;
     the finite clause needs no validation case.)
   - Declaration validation runs even when capture is DISABLED: the
     declaration is declarative data — broken is broken — and O4 has it
     validate at load while pinning only when the store opens. Only the
     cap floor stays enabled-only (consent-inert).

   All antenna findings carry a Field under "evidence.antennas" and are
   errors, not warnings.
*/

import (
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

func antennasCfg(capture bool, antennas ...types.AntennaDecl) types.EvidenceConfig {
	return types.EvidenceConfig{Capture: capture, CapBytes: 524288000, Antennas: antennas}
}

func decl(name string, bands ...string) types.AntennaDecl {
	return types.AntennaDecl{Name: name, Bands: bands}
}

func TestValidateAntennas(t *testing.T) {
	h := func(v float64) *float64 { return &v }
	cases := []struct {
		name      string
		in        types.EvidenceConfig
		wantErr   bool
		wantInMsg []string // every listed fragment must appear in some finding message
	}{
		{"no antennas is fine (zero-config participation)",
			antennasCfg(true), false, nil},
		{"the dogfood two-antenna declaration is valid",
			antennasCfg(true,
				types.AntennaDecl{Name: "DX Commander", Type: "vertical",
					Bands: []string{"80m", "40m", "30m"}, HeightM: h(0)},
				types.AntennaDecl{Name: "VHQ Hex beam", Type: "hexbeam",
					Bands:   []string{"20m", "17m", "15m", "12m", "10m", "6m"},
					HeightM: h(12), Locator: "KG49dj"}),
			false, nil},
		{"PR6: a band claimed by two antennas names the band and both",
			antennasCfg(true, decl("DX Commander", "80m", "20m"), decl("VHQ Hex beam", "20m")),
			true, []string{"20m", "DX Commander", "VHQ Hex beam"}},
		{"O1: duplicate post-trim names reject",
			antennasCfg(true, decl("DX Commander", "80m"), decl("  DX Commander ", "40m")),
			true, []string{"DX Commander"}},
		{"O1: empty name after trim rejects",
			antennasCfg(true, decl("   ", "80m")),
			true, nil},
		{"O1a: a band duplicated within one antenna rejects",
			antennasCfg(true, decl("DX Commander", "80m", "40m", "80m")),
			true, []string{"80m", "DX Commander"}},
		{"O1b: an empty band list rejects",
			antennasCfg(true, decl("DX Commander")),
			true, []string{"DX Commander"}},
		{"unknown band token rejects",
			antennasCfg(true, decl("DX Commander", "23m")),
			true, []string{"23m"}},
		{"O2b: an invalid locator rejects",
			antennasCfg(true, types.AntennaDecl{Name: "VHQ Hex beam",
				Bands: []string{"20m"}, Locator: "ZZ99xx"}),
			true, []string{"ZZ99xx"}},
		{"O2b: omitted locator is fine (pins not_declared, no inheritance)",
			antennasCfg(true, decl("VHQ Hex beam", "20m")),
			false, nil},
		{"O3: negative height rejects",
			antennasCfg(true, types.AntennaDecl{Name: "DX Commander",
				Bands: []string{"80m"}, HeightM: h(-1)}),
			true, nil},
		{"O3: zero height is a real value and passes",
			antennasCfg(true, types.AntennaDecl{Name: "DX Commander",
				Bands: []string{"80m"}, HeightM: h(0)}),
			false, nil},
		{"O3: an extreme height passes — no invented ceiling",
			antennasCfg(true, types.AntennaDecl{Name: "DX Commander",
				Bands: []string{"80m"}, HeightM: h(300)}),
			false, nil},
		{"declaration validates even with capture disabled (O4: validate at load, pin at store-open)",
			antennasCfg(false, decl("DX Commander", "80m", "80m")),
			true, nil},
	}
	for _, c := range cases {
		findings := validateEvidence(c.in)
		if c.wantErr && len(findings) == 0 {
			t.Errorf("%s: want a finding, got none", c.name)
			continue
		}
		if !c.wantErr && len(findings) != 0 {
			t.Errorf("%s: unexpected findings %v", c.name, findings)
			continue
		}
		all := ""
		for _, f := range findings {
			all += f.Message + "\n"
			if f.Warning {
				t.Errorf("%s: finding must be an error, got warning: %+v", c.name, f)
			}
			if !strings.Contains(f.Field, "evidence") {
				t.Errorf("%s: finding field %q must sit under the evidence block", c.name, f.Field)
			}
		}
		for _, frag := range c.wantInMsg {
			if !strings.Contains(all, frag) {
				t.Errorf("%s: findings must name %q; messages were:\n%s", c.name, frag, all)
			}
		}
	}
}

// The antenna rules must be WIRED into Validate — Load's fatal-on-error and
// PUT's atomic 400 both flow through it (the house wiring guard).
func TestValidate_CarriesAntennaFinding(t *testing.T) {
	cfg := Config{Evidence: antennasCfg(true,
		decl("DX Commander", "20m"), decl("VHQ Hex beam", "20m"))}
	for _, f := range Validate(cfg) {
		if strings.Contains(f.Field, "evidence.antennas") && !f.Warning {
			return
		}
	}
	t.Fatal("Validate must surface the duplicate-band antenna finding under evidence.antennas")
}
