package adapters

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type batchSrc struct {
	Name string
	Code int
}

type batchDst struct {
	Name string
	Code int
}

func TestBatch_AllHelperRegistrations_PrecedenceAndValidation(t *testing.T) {
	a := New()
	// Pre-seed existing registries to exercise copy/merge path in Batch
	a.RegisterConverter("Name", MapString(func(s string) string { return s + "-G" }))
	a.RegisterValidator("Code", func(v any) error { return nil })

	// Apply a batch with all helper methods
	a.Batch(func(r *RegistryBatch) {
		// global converter appends suffix; will be shadowed by dst and pair
		r.GlobalConverter("Name", MapString(func(s string) string { return s + "-BG" }))
		// destination-scoped lowercasing (to be shadowed by pair)
		r.ConverterFor(batchDst{}, "Name", MapString(func(s string) string { return strings.ToLower(s) }))
		// pair-scoped: constant value to assert precedence
		r.ConverterForPair(batchSrc{}, batchDst{}, "Name", func(v any) (any, error) { return "PAIR", nil })

		// global validator always ok; dst validator rejects Code==0; pair validator overrides to ok only if Name=="PAIR"
		r.GlobalValidator("Code", func(v any) error { return nil })
		r.ValidatorFor(batchDst{}, "Code", func(v any) error {
			if v.(int) == 0 {
				return assert.AnError
			}
			return nil
		})
		r.ValidatorForPair(batchSrc{}, batchDst{}, "Name", func(v any) error {
			if v.(string) != "PAIR" {
				return assert.AnError
			}
			return nil
		})
	})

	s := batchSrc{Name: "MiXeD", Code: 0}
	d := batchDst{}
	// Code validator at dst scope would fail
	err := a.Into(&d, &s)
	assert.Error(t, err)

	// Make it pass
	s.Code = 7
	d = batchDst{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, "PAIR", d.Name) // pair precedence
	assert.Equal(t, 7, d.Code)
}

func TestMapString_NonString_NoOp(t *testing.T) {
	a := New()
	// Register MapString on an int field; should no-op
	a.RegisterConverter("Code", MapString(func(s string) string { return s + "!" }))
	s := batchSrc{Name: "X", Code: 5}
	d := batchDst{}
	require.NoError(t, a.Into(&d, &s))
	// Code remains unchanged since converter ignored non-string
	assert.Equal(t, 5, d.Code)
}

func TestGenerics_Copy_And_Make(t *testing.T) {
	a := New()
	s := batchSrc{Name: "hi", Code: 11}
	var d1 batchDst
	require.NoError(t, Copy(a, &d1, &s))
	assert.Equal(t, "hi", d1.Name)
	assert.Equal(t, 11, d1.Code)

	// Make returns a value (not pointer)
	d2, err := Make[batchDst](a, &s)
	require.NoError(t, err)
	assert.Equal(t, "hi", d2.Name)
	assert.Equal(t, 11, d2.Code)
}
