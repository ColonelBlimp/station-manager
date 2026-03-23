package adapters

import (
	"github.com/Station-Manager/adapters/converters/common"
	sqmodels "github.com/Station-Manager/database/sqlite/models"
	"github.com/Station-Manager/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"testing"
)

type TypeQso struct {
	ID int64
	TypeStation
}

type TypeStation struct {
	Name string
}

type ModelQso struct {
	ID   int64
	Name string
}

type TestSuite struct {
	suite.Suite
}

func TestAdapterSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func (s *TestSuite) TestBasicCopy_TypeToModel() {
	typeQso := types.Qso{
		QsoDetails: types.QsoDetails{
			Freq: "14.320",
		},
	}
	modelQso := sqmodels.Qso{}

	adapter := New()
	adapter.RegisterConverter("Freq", common.TypeToModelFreqConverter)

	// We want to copy Type->Model, so destination is &modelQso, source &typeQso
	err := adapter.Into(&modelQso, &typeQso)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), typeQso.ID, modelQso.ID)
	assert.Equal(s.T(), int64(14320000), modelQso.Freq)
}

func (s *TestSuite) TestBasicCopy_ModelToType() {
	typeQso := types.Qso{}
	modelQso := sqmodels.Qso{
		Freq: 14320000,
	}

	adapter := New()
	adapter.RegisterConverter("Freq", common.ModelToTypeFreqConverter)

	// Copy Model->Type so destination is &typeQso, source &modelQso
	err := adapter.Into(&typeQso, &modelQso)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), modelQso.ID, typeQso.ID)
	assert.Equal(s.T(), "14.320", typeQso.Freq)
}
