package adapters

import (
	"encoding/json"
	"testing"

	"github.com/aarondl/null/v8"
	boilertypes "github.com/aarondl/sqlboiler/v4/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdditionalTag_Rename_MarshalToExtras(t *testing.T) {
	a := New()
	type S struct {
		Name string
		Age  int
		City string
	}
	type D struct {
		Name   string
		Age    int
		Extras null.JSON `adapter:"additional"`
	}
	s := S{Name: "N", Age: 10, City: "C"}
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, "N", d.Name)
	assert.Equal(t, 10, d.Age)
	require.True(t, d.Extras.Valid)
	var m map[string]any
	require.NoError(t, json.Unmarshal(d.Extras.JSON, &m))
	assert.Equal(t, "C", m["City"])
	_, hasName := m["Name"]
	assert.False(t, hasName)
}

func TestAdditionalTag_Rename_UnmarshalFromExtras(t *testing.T) {
	a := New()
	type S struct {
		Extras null.JSON `adapter:"additional"`
	}
	type D struct{ City string }
	b, _ := json.Marshal(map[string]any{"City": "C"})
	s := S{Extras: null.JSONFrom(b)}
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, "C", d.City)
}

func TestBatchRegistration_Works(t *testing.T) {
	a := New()
	type S struct {
		A string
		B int
	}
	type D struct {
		A string
		B int
	}
	a.Batch(func(r *RegistryBatch) {
		r.GlobalConverter("A", MapString(func(s string) string { return s + "!" }))
		r.GlobalValidator("B", func(v any) error {
			if v.(int) < 0 {
				return assert.AnError
			}
			return nil
		})
	})
	s := S{A: "x", B: 1}
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, "x!", d.A)
}

func TestMarshalRemainingFields_BoilerJSON(t *testing.T) {
	a := NewWithOptions(WithIncludeZeroValues(false))
	type S struct {
		A string
		B int
	}
	type D struct {
		A              string
		AdditionalData boilertypes.JSON
	}
	s := S{A: "x", B: 2}
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	assert.NotNil(t, d.AdditionalData)
	var m map[string]any
	require.NoError(t, json.Unmarshal(d.AdditionalData, &m))
	assert.Equal(t, float64(2), m["B"]) // numbers decode as float64
}
