package adapters

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aarondl/null/v8"
	boilertypes "github.com/aarondl/sqlboiler/v4/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Cover direct RegisterConverterFor and RegisterConverterForPair precedence without the builder.
func TestConverterScopingPrecedence_Direct(t *testing.T) {
	a := New()
	type S struct{ Name string }
	type D struct{ Name string }
	// global: append !
	a.RegisterConverter("Name", MapString(func(s string) string { return s + "!" }))
	// dst: lower
	a.RegisterConverterFor(D{}, "Name", MapString(func(s string) string { return strings.ToLower(s) }))
	// pair: const
	a.RegisterConverterForPair(S{}, D{}, "Name", func(v any) (any, error) { return "PAIR", nil })
	s := S{Name: "MiXeD"}
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, "PAIR", d.Name)
}

// Case-sensitivity for AdditionalData keys
func TestAdditionalData_CaseSensitivity_Toggle(t *testing.T) {
	type S struct{ AdditionalData null.JSON }
	type D struct{ City string }
	payload := map[string]any{"cItY": "London"}
	b, _ := json.Marshal(payload)
	s := S{AdditionalData: null.JSONFrom(b)}
	// default: case-sensitive => no match
	a := New()
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, "", d.City)
	// enable case-insensitive => match
	a2 := NewWithOptions(WithCaseInsensitiveAdditionalData(true))
	d2 := D{}
	require.NoError(t, a2.Into(&d2, &s))
	assert.Equal(t, "London", d2.City)
}

// Disabling both marshal and unmarshal fully skips AdditionalData handling
func TestAdditionalData_DisableBoth(t *testing.T) {
	type S struct {
		A              int
		AdditionalData null.JSON
	}
	type D struct {
		A              int
		AdditionalData null.JSON
	}
	b, _ := json.Marshal(map[string]any{"A": 9})
	s := S{A: 1, AdditionalData: null.JSONFrom(b)}
	a := NewWithOptions(WithDisableUnmarshalAdditionalData(true), WithDisableMarshalAdditionalData(true))
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	// Unmarshal disabled, so A remains 1
	assert.Equal(t, 1, d.A)
	// Marshal disabled, so AdditionalData remains zero
	assert.False(t, d.AdditionalData.Valid)
}

// Include zero values should marshal zeros into AdditionalData
func TestAdditionalData_IncludeZeroValuesTrue(t *testing.T) {
	a := NewWithOptions(WithIncludeZeroValues(true))
	type S struct {
		A              int
		B              string
		AdditionalData null.JSON
	}
	type D struct{ AdditionalData null.JSON }
	s := S{} // zeros
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	require.True(t, d.AdditionalData.Valid)
	var m map[string]any
	require.NoError(t, json.Unmarshal(d.AdditionalData.JSON, &m))
	// zeros present
	assert.Equal(t, float64(0), m["A"]) // numbers decode as float64
	assert.Equal(t, "", m["B"])
}

// Verify PreferAdditionalData overwrites direct field from AdditionalData
func TestAdditionalData_PreferAdditionalData_Overwrite(t *testing.T) {
	a := NewWithOptions(WithOverwritePolicy(PreferAdditionalData))
	type S struct {
		Name           string
		AdditionalData null.JSON
	}
	type D struct{ Name string }
	b, _ := json.Marshal(map[string]any{"Name": "AD"})
	s := S{Name: "Field", AdditionalData: null.JSONFrom(b)}
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, "AD", d.Name)
}

// Boilertypes JSON unmarshal case-insensitivity
func TestAdditionalData_BoilerJSON_CaseInsensitive(t *testing.T) {
	a := NewWithOptions(WithCaseInsensitiveAdditionalData(true))
	type S struct{ AdditionalData boilertypes.JSON }
	type D struct{ Phone string }
	b, _ := json.Marshal(map[string]any{"pHoNe": "123"})
	s := S{AdditionalData: boilertypes.JSON(b)}
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, "123", d.Phone)
}

// Global converter applied for AdditionalData path
func TestAdditionalData_Converter_GlobalApplied(t *testing.T) {
	a := New()
	// converter parses number string to int
	a.RegisterConverter("Age", func(v any) (any, error) {
		if s, ok := v.(string); ok {
			if s == "bad" {
				return nil, errors.New("bad")
			}
			return 42, nil
		}
		return v, nil
	})
	type S struct{ AdditionalData null.JSON }
	type D struct{ Age int }
	b, _ := json.Marshal(map[string]any{"Age": "39"})
	s := S{AdditionalData: null.JSONFrom(b)}
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, 42, d.Age)
	// error from converter -> no set, no fallback
	b2, _ := json.Marshal(map[string]any{"Age": "bad"})
	s = S{AdditionalData: null.JSONFrom(b2)}
	d = D{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, 0, d.Age)
}

// WarmMetadata is a no-op for non-structs and pre-builds for structs
func TestWarmMetadata_MixedArgs(t *testing.T) {
	a := New()
	type S struct{ A int }
	type D struct{ A int }
	a.WarmMetadata(123, "x", S{}, &D{}) // ints/strings ignored; S and D warmed
	// Adapt still works and uses cache
	s := S{A: 7}
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, 7, d.A)
}
