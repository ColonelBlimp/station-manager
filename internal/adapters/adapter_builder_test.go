package adapters

import (
	"errors"
	"github.com/goccy/go-json"
	"reflect"
	"strings"
	"testing"

	"github.com/aarondl/null/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type bSrc struct{ Name string }

type bDst struct{ Name string }

func TestBuilder_ScopedPrecedence_WithValidators(t *testing.T) {
	b := NewBuilder().
		AddConverter("Name", MapString(strings.ToUpper)).                                             // global: UPPER
		AddConverterFor(bDst{}, "Name", MapString(strings.ToLower)).                                  // dst: lower
		AddConverterForPair(bSrc{}, bDst{}, "Name", func(v any) (any, error) { return "PAIR", nil }). // pair: const
		AddValidator("Name", func(v any) error {
			if v.(string) == "" {
				return errors.New("empty")
			}
			return nil
		}). // global ok
		AddValidatorFor(bDst{}, "Name", func(v any) error {
			if v.(string) == "bad" {
				return errors.New("bad")
			}
			return nil
		}). // dst ok
		AddValidatorForPair(bSrc{}, bDst{}, "Name", func(v any) error {
			if v.(string) != "PAIR" {
				return errors.New("not pair")
			}
			return nil
		}) // pair enforced

	a := b.Build()
	s := bSrc{Name: "MiXeD"}
	d := bDst{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, "PAIR", d.Name)
}

func TestValidators_FromAdditionalData(t *testing.T) {
	a := New()
	type S struct{ AdditionalData null.JSON }
	type D struct{ Code int }
	// validator forbids code < 10
	a.RegisterValidator("Code", func(v any) error {
		if v.(int) < 10 {
			return errors.New("too small")
		}
		return nil
	})
	ad := map[string]any{"Code": 5}
	b, _ := json.Marshal(ad)
	s := S{AdditionalData: null.JSONFrom(b)}
	d := D{}
	err := a.Into(&d, &s)
	assert.Error(t, err)
}

func TestWarmMetadata_Smoke(t *testing.T) {
	a := New()
	type S struct{ A, B int }
	type D struct{ A, B int }
	// Before warm
	_ = a.getOrBuildMetadata(reflect.TypeOf(S{}))
	_ = a.getOrBuildMetadata(reflect.TypeOf(D{}))
	// call warm again just to ensure it doesn't panic and paths run
	a.WarmMetadata(S{}, D{}, 42, nil)
	// Adapt still works
	s := S{A: 1, B: 2}
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, 1, d.A)
	assert.Equal(t, 2, d.B)
}

func TestGeneric_AdaptTo(t *testing.T) {
	a := New()
	type S struct{ A int }
	type D struct{ A int }
	s := S{A: 3}
	d, err := AdaptTo[D](a, &s)
	require.NoError(t, err)
	assert.Equal(t, 3, d.A)
}
