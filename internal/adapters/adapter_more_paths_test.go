package adapters

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/aarondl/null/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Additional coverage: overwrite blocked when PreferFields
func TestAdapt_OverwriteBlocked(t *testing.T) {
	a2 := NewWithOptions(WithOverwritePolicy(PreferFields))
	type S struct {
		Age            int
		AdditionalData null.JSON
	}
	type D struct {
		Age            int
		AdditionalData null.JSON
	}
	b, _ := json.Marshal(map[string]any{"Age": 99})
	s := S{Age: 10, AdditionalData: null.JSONFrom(b)}
	d := D{}
	require.NoError(t, a2.Into(&d, &s))
	assert.Equal(t, 10, d.Age) // blocked overwrite
}

// Ensure metadata counts embedded fields
func TestMetadata_CountEmbedded(t *testing.T) {
	a := New()
	type E1 struct{ X int }
	type E2 struct{ Y string }
	type P struct {
		E1
		*E2
		Z bool
	}
	m := a.getOrBuildMetadata(reflect.TypeOf(P{}))
	// E1.X + E2.Y + Z => 3 (pointer embedded counted)
	assert.Equal(t, 3, len(m.fields))
}

// Converter returning nil sets zero value
func TestConverter_ReturnNilSetsZero(t *testing.T) {
	a := New()
	type S struct{ V int }
	type D struct{ V int }
	a.RegisterConverter("V", func(v any) (any, error) { return nil, nil })
	s := S{V: 5}
	d := D{V: 99}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, 0, d.V)
}

// Validator error path for dst scoped
func TestValidator_DstScopedError(t *testing.T) {
	a := New()
	type S struct{ V int }
	type D struct{ V int }
	a.RegisterValidatorFor(D{}, "V", func(v any) error {
		if v.(int) > 0 {
			return assert.AnError
		}
		return nil
	})
	s := S{V: 1}
	d := D{}
	err := a.Into(&d, &s)
	assert.Error(t, err)
}
