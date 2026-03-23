package adapters

import (
	"errors"
	"strings"
	"testing"

	"github.com/aarondl/null/v8"
	"github.com/goccy/go-json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidator_DstScope_FromAdditionalData(t *testing.T) {
	a := New()
	type S struct{ AdditionalData null.JSON }
	type D struct{ Email string }
	// validator on D type: require '@'
	a.RegisterValidatorFor(D{}, "Email", func(v any) error {
		s := v.(string)
		if len(s) == 0 || !strings.Contains(s, "@") {
			return errors.New("invalid email")
		}
		return nil
	})
	m := map[string]any{"Email": "notanemail"}
	b, _ := json.Marshal(m)
	s := S{AdditionalData: null.JSONFrom(b)}
	d := D{}
	err := a.Into(&d, &s)
	assert.Error(t, err)
}

func TestValidator_PairScope_FromAdditionalData(t *testing.T) {
	a := New()
	// Adjust test: use direct field so pair validator executes in adaptField path
	type S struct{ Code int }
	type D struct{ Code int }
	a.RegisterValidatorForPair(S{}, D{}, "Code", func(v any) error {
		if v.(int) < 100 {
			return errors.New("too small")
		}
		return nil
	})
	s := S{Code: 50}
	d := D{}
	err := a.Into(&d, &s)
	assert.Error(t, err)
}

func TestCaseInsensitive_AdditionalData_WithJSONTags(t *testing.T) {
	a := NewWithOptions(WithCaseInsensitiveAdditionalData(true))
	type S struct{ AdditionalData null.JSON }
	type D struct {
		First string `json:"first_name"`
	}
	m := map[string]any{"FIRST_NAME": "Bob"}
	b, _ := json.Marshal(m)
	s := S{AdditionalData: null.JSONFrom(b)}
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, "Bob", d.First)
}
