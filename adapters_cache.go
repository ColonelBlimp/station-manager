package database

//
//import (
//	"github.com/Station-Manager/adapters"
//	"github.com/Station-Manager/adapters/converters/common"
//	"github.com/Station-Manager/adapters/converters/postgres"
//	"github.com/Station-Manager/adapters/converters/sqlite"
//	sqmodels "github.com/Station-Manager/database/sqlite/models"
//	"github.com/Station-Manager/types"
//)
//
//// initAdapters lazily initializes reusable adapters for model<->type conversions.
//// It is safe to call multiple times; initialization runs once.
//func (s *Service) initAdapters() {
//	s.adaptersOnce.Do(func() {
//		// Adapter used when converting from domain types -> DB models (insert/update)
//		aToModel := adapters.New()
//		// Shared converters
//		aToModel.RegisterConverter("Freq", common.TypeToModelFreqConverter)
//		aToModel.RegisterConverter("Country", common.TypeToModelStringConverter)
//		aToModel.RegisterConverter("Description", common.TypeToModelStringConverter)
//		aToModel.RegisterConverter("AdditionalData", common.TypeToModelStringConverter)
//		if s.DatabaseConfig.Driver == SqliteDriver {
//			aToModel.RegisterConverter("QsoDate", sqlite.TypeToModelDateConverter)
//			aToModel.RegisterConverter("TimeOn", sqlite.TypeToModelTimeConverter)
//			aToModel.RegisterConverter("TimeOff", sqlite.TypeToModelTimeConverter)
//			// Ensure ContactedStation extra fields are marshaled into AdditionalData on the sqlite model.
//			// Direct fields (ID, Name, Call, Country, TimeOffset) stay as columns; remaining exported fields
//			// from types.ContactedStation will be encoded into models.ContactedStation.AdditionalData.
//			// The adapter core already knows how to detect and populate an AdditionalData field.
//		} else {
//			aToModel.RegisterConverter("QsoDate", postgres.TypeToModelDateConverter)
//			aToModel.RegisterConverter("TimeOn", postgres.TypeToModelTimeConverter)
//			aToModel.RegisterConverter("TimeOff", postgres.TypeToModelTimeConverter)
//		}
//		// Warm metadata for ContactedStation mapping so AdditionalData handling is set up.
//		aToModel.WarmMetadata(types.ContactedStation{}, sqmodels.ContactedStation{})
//		s.adapterToModel = aToModel
//
//		// Adapter used when converting from DB models -> domain types (fetch)
//		aFromModel := adapters.New()
//		aFromModel.RegisterConverter("Freq", common.ModelToTypeFreqConverter)
//		aFromModel.RegisterConverter("Country", common.ModelToTypeStringConverter)
//		aFromModel.RegisterConverter("Description", common.ModelToTypeStringConverter)
//		if s.DatabaseConfig.Driver == SqliteDriver {
//			aFromModel.RegisterConverter("QsoDate", sqlite.ModelToTypeDateConverter)
//			aFromModel.RegisterConverter("TimeOn", sqlite.ModelToTypeTimeConverter)
//			aFromModel.RegisterConverter("TimeOff", sqlite.ModelToTypeTimeConverter)
//		} else {
//			aFromModel.RegisterConverter("QsoDate", postgres.ModelToTypeDateConverter)
//			aFromModel.RegisterConverter("TimeOn", postgres.ModelToTypeTimeConverter)
//			aFromModel.RegisterConverter("TimeOff", postgres.ModelToTypeTimeConverter)
//		}
//		// Warm metadata for ContactedStation reverse mapping so AdditionalData is properly expanded.
//		aFromModel.WarmMetadata(sqmodels.ContactedStation{}, types.ContactedStation{})
//		s.adapterFromModel = aFromModel
//	})
//}
