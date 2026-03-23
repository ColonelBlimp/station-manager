package adapters

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aarondl/null/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type srcJSONTag struct {
	FirstName      string    `json:"first_name"`
	AdditionalData null.JSON `json:"additional_data"`
}

type dstJSONTag struct {
	FirstName      string    `json:"first_name"`
	AdditionalData null.JSON `json:"additional_data"`
}

func TestJSONTagMatching_Default(t *testing.T) {
	a := New()
	s := srcJSONTag{FirstName: "jane"}
	d := dstJSONTag{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, "jane", d.FirstName)
}

func TestScopedConverters_Precedence(t *testing.T) {
	// global upper, dst overrides to lower, pair overrides to exact
	a := New()
	a.RegisterConverter("Name", MapString(func(s string) string { return strings.ToUpper(s) }))
	type dst struct{ Name string }
	// dst-scoped lower
	a.RegisterConverterFor(dst{}, "Name", MapString(func(s string) string { return strings.ToLower(s) }))
	// pair-scoped exact return "X"
	type src struct{ Name string }
	a.RegisterConverterForPair(src{}, dst{}, "Name", func(v any) (any, error) { return "X", nil })

	sv := src{Name: "MiXeD"}
	dv := dst{}
	require.NoError(t, a.Into(&dv, &sv))
	assert.Equal(t, "X", dv.Name)
}

func TestAdditionalData_OverwritePolicy(t *testing.T) {
	a := NewWithOptions(WithOverwritePolicy(PreferFields))
	type S struct {
		Name           string
		AdditionalData null.JSON
	}
	type D struct {
		Name           string
		AdditionalData null.JSON
	}
	// Name present in src field and also in AdditionalData => with PreferFields, do not overwrite
	m := map[string]any{"Name": "AD"}
	b, _ := json.Marshal(m)
	s := S{Name: "Field", AdditionalData: null.JSONFrom(b)}
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, "Field", d.Name)

	// Now allow overwrite
	a2 := NewWithOptions(WithOverwritePolicy(PreferAdditionalData))
	d2 := D{}
	require.NoError(t, a2.Into(&d2, &s))
	assert.Equal(t, "AD", d2.Name)
}

func TestIncludeZeroValues_InAdditionalData(t *testing.T) {
	a := NewWithOptions(WithIncludeZeroValues(true))
	type S struct {
		N              int
		AdditionalData null.JSON
	}
	type D struct{ AdditionalData null.JSON }
	s := S{} // N is zero => Should be included
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	var m map[string]any
	require.NoError(t, json.Unmarshal(d.AdditionalData.JSON, &m))
	_, ok := m["N"]
	assert.True(t, ok)
}

func TestCaseInsensitive_AdditionalData(t *testing.T) {
	a := NewWithOptions(WithCaseInsensitiveAdditionalData(true))
	type S struct{ AdditionalData null.JSON }
	type D struct{ Foo string }
	m := map[string]any{"fOo": "bar"}
	b, _ := json.Marshal(m)
	s := S{AdditionalData: null.JSONFrom(b)}
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, "bar", d.Foo)
}

func TestComposeConverters(t *testing.T) {
	f := ComposeConverters(
		MapString(func(s string) string { return s + "!" }),
		MapString(strings.ToUpper),
	)
	out, err := f("hi")
	require.NoError(t, err)
	assert.Equal(t, "HI!", out)
}

func TestValidators_AreApplied(t *testing.T) {
	a := New()
	type S struct{ Name string }
	type D struct{ Name string }
	// validator enforces uppercase
	a.RegisterValidator("Name", func(v any) error {
		s, _ := v.(string)
		if s != strings.ToUpper(s) {
			return errors.New("name must be uppercase")
		}
		return nil
	})
	s := S{Name: "john"}
	d := D{}
	err := a.Into(&d, &s)
	assert.Error(t, err)
}

func TestDisableAdditionalDataOptions(t *testing.T) {
	a := NewWithOptions(WithDisableUnmarshalAdditionalData(true), WithDisableMarshalAdditionalData(true))
	type S struct {
		Name           string
		AdditionalData null.JSON
	}
	type D struct {
		Name           string
		AdditionalData null.JSON
	}
	m := map[string]any{"Name": "AD"}
	b, _ := json.Marshal(m)
	s := S{Name: "Field", AdditionalData: null.JSONFrom(b)}
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	// Unmarshal disabled -> Name remains from field only
	assert.Equal(t, "Field", d.Name)
	// Marshal disabled -> AdditionalData remains zero
	assert.False(t, d.AdditionalData.Valid)
}

func TestBuilder_Basic(t *testing.T) {
	b := NewBuilder().WithOptions(WithCaseInsensitiveAdditionalData(true)).
		AddConverter("Name", MapString(strings.ToUpper)).
		AddValidator("Name", func(v any) error {
			if v.(string) == "" {
				return errors.New("empty")
			}
			return nil
		})

	a := b.Build()
	// sanity test
	type S struct{ Name string }
	type D struct{ Name string }
	s := S{Name: "x"}
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, "X", d.Name)
}
