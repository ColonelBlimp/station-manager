package cat

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// newLookupService returns a minimal Service with the given prefix→CatState
// mapping. maxCatPrefixLen is derived from the longest key.
func newLookupService(states map[string]types.CatState) *Service {
	maxLen := 0
	for k := range states {
		if l := len(k); l > maxLen {
			maxLen = l
		}
	}
	return &Service{
		supportedCatStates: states,
		maxCatPrefixLen:    maxLen,
	}
}

func TestLookupCatStateLineTooShort(t *testing.T) {
	svc := newLookupService(map[string]types.CatState{"AB": {Prefix: "AB"}})

	_, ok := svc.lookupCatState([]byte("A"))
	require.False(t, ok, "single byte should not match (< minPrefix)")

	_, ok = svc.lookupCatState(nil)
	require.False(t, ok, "nil input should not match")

	_, ok = svc.lookupCatState([]byte{})
	require.False(t, ok, "empty input should not match")
}

func TestLookupCatStateCaseInsensitive(t *testing.T) {
	svc := newLookupService(map[string]types.CatState{"IF": {Prefix: "IF"}})

	state, ok := svc.lookupCatState([]byte("if001234"))
	require.True(t, ok)
	require.Equal(t, "001234", state.Data)
}

func TestLookupCatStateLongestPrefixFirst(t *testing.T) {
	svc := newLookupService(map[string]types.CatState{
		"IF":  {Prefix: "IF", Markers: []types.Marker{{Tag: "short"}}},
		"IFX": {Prefix: "IFX", Markers: []types.Marker{{Tag: "long"}}},
	})

	state, ok := svc.lookupCatState([]byte("IFX9999"))
	require.True(t, ok)
	require.Equal(t, "long", state.Markers[0].Tag, "should match the longer IFX prefix")
	require.Equal(t, "9999", state.Data)
}

func TestLookupCatStateDataExtraction(t *testing.T) {
	svc := newLookupService(map[string]types.CatState{"FA": {Prefix: "FA"}})

	state, ok := svc.lookupCatState([]byte("FA00014250000"))
	require.True(t, ok)
	require.Equal(t, "00014250000", state.Data)
}

func TestLookupCatStateExactPrefixLength(t *testing.T) {
	svc := newLookupService(map[string]types.CatState{"FA": {Prefix: "FA"}})

	state, ok := svc.lookupCatState([]byte("FA"))
	require.True(t, ok)
	require.Equal(t, "", state.Data, "data should be empty when line length equals prefix length")
}

func TestLookupCatStateNoMatch(t *testing.T) {
	svc := newLookupService(map[string]types.CatState{"IF": {Prefix: "IF"}})

	_, ok := svc.lookupCatState([]byte("ZZ9999"))
	require.False(t, ok)
}

func TestLookupCatStateEmptyMap(t *testing.T) {
	svc := newLookupService(map[string]types.CatState{})

	_, ok := svc.lookupCatState([]byte("FA1234"))
	require.False(t, ok, "empty state map should never match")
}

func TestLookupCatStateMixedCase(t *testing.T) {
	svc := newLookupService(map[string]types.CatState{"SMETER": {Prefix: "SMETER"}})

	state, ok := svc.lookupCatState([]byte("sMeTeR0255"))
	require.True(t, ok)
	require.Equal(t, "0255", state.Data)
}

func TestLookupCatStateMultiplePrefixesShortestMatch(t *testing.T) {
	// When the line only matches the shorter prefix (not the longer), ensure the short one wins.
	svc := newLookupService(map[string]types.CatState{
		"FA":  {Prefix: "FA", Markers: []types.Marker{{Tag: "freq"}}},
		"FAA": {Prefix: "FAA", Markers: []types.Marker{{Tag: "other"}}},
	})

	// "FAB123" — first 3 chars "FAB" don't match "FAA", so longest-first falls to "FA".
	state, ok := svc.lookupCatState([]byte("FAB123"))
	require.True(t, ok)
	require.Equal(t, "freq", state.Markers[0].Tag)
	require.Equal(t, "B123", state.Data)
}
