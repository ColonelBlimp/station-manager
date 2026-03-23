package adapters

import (
	"errors"
	"testing"

	"github.com/aarondl/null/v8"
	"github.com/goccy/go-json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mSrc struct {
	A              int
	AdditionalData null.JSON
}

type mDst struct {
	A int
	B int
}

func TestDisableMarshal_PreventsAdditionalData(t *testing.T) {
	a := NewWithOptions(WithDisableMarshalAdditionalData(true))
	s := mSrc{A: 1}
	d := struct {
		A              int
		AdditionalData null.JSON
	}{}
	require.NoError(t, a.Into(&d, &s))
	assert.False(t, d.AdditionalData.Valid)
}

func TestDisableUnmarshal_SkipsExpansion(t *testing.T) {
	b, _ := json.Marshal(map[string]any{"B": 9})
	a := NewWithOptions(WithDisableUnmarshalAdditionalData(true))
	s := mSrc{A: 1, AdditionalData: null.JSONFrom(b)}
	d := mDst{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, 0, d.B)
}

func TestValidatorPairScope_Error(t *testing.T) {
	a := New()
	type S struct{ X int }
	type D struct{ X int }
	a.RegisterValidatorForPair(S{}, D{}, "X", func(v any) error { return errors.New("pair fail") })
	s := S{X: 1}
	d := D{}
	err := a.Into(&d, &s)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pair fail")
}
