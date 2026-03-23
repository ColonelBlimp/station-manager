package adapters

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type bsSrc struct{ Name string }

type bsDst struct{ Name string }

type bsDst2 struct{ Name string }

func TestBuilder_ConverterDstScopePrecedence(t *testing.T) {
	b := NewBuilder().
		AddConverter("Name", MapString(strings.ToUpper)).            // global
		AddConverterFor(bsDst{}, "Name", MapString(strings.ToLower)) // dst wins
	a := b.Build()
	s := bsSrc{Name: "MiXeD"}
	d := bsDst{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, "mixed", d.Name)
}

func TestBuilder_ConverterPairScopePrecedence(t *testing.T) {
	b := NewBuilder().
		AddConverter("Name", MapString(strings.ToUpper)).
		AddConverterFor(bsDst2{}, "Name", MapString(strings.ToLower)).
		AddConverterForPair(bsSrc{}, bsDst2{}, "Name", func(v any) (any, error) { return "PAIRWIN", nil })

	a := b.Build()
	s := bsSrc{Name: "MiXeD"}
	d := bsDst2{}
	require.NoError(t, a.Into(&d, &s))
	assert.Equal(t, "PAIRWIN", d.Name)
}
