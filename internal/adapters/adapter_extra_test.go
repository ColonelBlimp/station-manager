package adapters

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aarondl/null/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Validate precedence where global + dst validators would fail but pair passes, ensuring short-circuit.
func TestValidator_PrecedencePairOverridesOthers(t *testing.T) {
	a := New()
	type S struct{ V int }
	type D struct{ V int }
	// global failing validator
	a.RegisterValidator("V", func(v any) error { return errors.New("global fail") })
	// dst failing validator
	a.RegisterValidatorFor(D{}, "V", func(v any) error { return errors.New("dst fail") })
	// pair succeeding validator should override
	a.RegisterValidatorForPair(S{}, D{}, "V", func(v any) error { return nil })
	s := S{V: 1}
	d := D{}
	err := a.Into(&d, &s)
	// Should succeed because pair validator wins
	require.NoError(t, err)
	assert.Equal(t, 1, d.V)
}

// Builder with validator pair precedence
func TestBuilder_ValidatorPairPrecedence(t *testing.T) {
	b := NewBuilder().
		AddValidator("Name", func(v any) error { return errors.New("global fail") }).
		AddValidatorFor(bsDst{}, "Name", func(v any) error { return errors.New("dst fail") }).
		AddValidatorForPair(bsSrc{}, bsDst{}, "Name", func(v any) error { return nil }).
		AddConverter("Name", MapString(strings.ToUpper))
	a := b.Build()
	s := bsSrc{Name: "x"}
	d := bsDst{}
	err := a.Into(&d, &s)
	require.NoError(t, err)
	assert.Equal(t, "X", d.Name)
}

// Zero value exclusion when IncludeZeroValues=false (default)
func TestAdditionalData_ExcludeZeroValues(t *testing.T) {
	a := New() // default exclude zeros
	type S struct {
		A              int
		B              string
		C              bool
		AdditionalData null.JSON
	}
	type D struct{ AdditionalData null.JSON }
	s := S{} // all zero values
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	assert.False(t, d.AdditionalData.Valid, "no remaining non-zero fields => should not marshal")
}

// PreferAdditionalData overwrites direct field
func TestAdditionalData_OverwritePolicyPreferAdditional(t *testing.T) {
	a := NewWithOptions(WithOverwritePolicy(PreferAdditionalData))
	type S struct {
		Name           string
		AdditionalData null.JSON
	}
	type D struct{ Name string }
	m := map[string]any{"Name": "AD"}
	b, _ := json.Marshal(m)
	s := S{Name: "Field", AdditionalData: null.JSONFrom(b)}
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, "AD", d.Name)
}

// Converter error inside AdditionalData should not set field and not fallback
func TestAdditionalData_ConverterError_NoFallback(t *testing.T) {
	a := New()
	type S struct{ AdditionalData null.JSON }
	type D struct{ X int }
	a.RegisterConverter("X", func(v any) (any, error) { return nil, errors.New("convert err") })
	m := map[string]any{"X": 10}
	b, _ := json.Marshal(m)
	s := S{AdditionalData: null.JSONFrom(b)}
	d := D{}
	err := a.Into(&d, &s)
	require.NoError(t, err) // adaptation itself does not error (converter error ignored for AdditionalData field)
	assert.Equal(t, 0, d.X) // field not set
}

// Concurrency test for validators copy-on-write registry
func TestValidators_ConcurrentRegistrationAndAdapt(t *testing.T) {
	a := New()
	type S struct{ V int }
	type D struct{ V int }
	// initial pass validator
	a.RegisterValidator("V", func(v any) error {
		if v.(int) < 0 {
			return errors.New("neg")
		}
		return nil
	})

	var start sync.WaitGroup
	start.Add(1)
	adaptations := runtime.GOMAXPROCS(0) * 5
	var wg sync.WaitGroup
	wg.Add(adaptations + 1)
	var done atomic.Bool
	errCh := make(chan string, adaptations*2)

	// writer continuously swaps validators
	go func() {
		defer wg.Done()
		start.Wait()
		for i := 0; i < 300; i++ {
			// alternate a validator that always passes and one that enforces value != 999
			if i%2 == 0 {
				a.RegisterValidator("V", func(v any) error { return nil })
			} else {
				a.RegisterValidator("V", func(v any) error {
					if v.(int) == 999 {
						return errors.New("bad999")
					}
					return nil
				})
			}
			if done.Load() {
				return
			}
		}
	}()

	for r := 0; r < adaptations; r++ {
		go func() {
			defer wg.Done()
			start.Wait()
			for i := 0; i < 200; i++ {
				s := S{V: i}
				d := D{}
				if err := a.Into(&d, &s); err != nil {
					errCh <- fmt.Sprintf("unexpected validator error: %v", err)
					return
				}
				if d.V != i {
					errCh <- fmt.Sprintf("value mismatch %d != %d", d.V, i)
					return
				}
			}
		}()
	}
	start.Done()
	wg.Wait()
	done.Store(true)
	close(errCh)
	for msg := range errCh {
		t.Fatalf("validators concurrency error: %s", msg)
	}
}

// Case-insensitive lookup with both field name and JSON tag conflict
func TestAdditionalData_CaseInsensitiveConflictPrefersDirect(t *testing.T) {
	a := NewWithOptions(WithCaseInsensitiveAdditionalData(true))
	type S struct {
		AdditionalData null.JSON
		Name           string
	}
	type D struct {
		Name string `json:"name"`
	}
	m := map[string]any{"NaMe": "AD"}
	b, _ := json.Marshal(m)
	s := S{Name: "Direct", AdditionalData: null.JSONFrom(b)}
	d := D{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, "Direct", d.Name) // direct field precedence retained when PreferFields
}
