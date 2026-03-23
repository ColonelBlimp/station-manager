package adif

import (
	"github.com/Station-Manager/types"
	"testing"
)

func TestRecord_String(t *testing.T) {
	record := &Record{
		QsoDetails: types.QsoDetails{
			Freq:       "7.050.000",
			Band:       "40m",
			Mode:       "SSB",
			Submode:    "LSB",
			QsoDate:    "2025-05-08",
			QsoDateOff: "2025-05-08",
			TimeOn:     "08:45:00",
			TimeOff:    "08:50:00",
			RstRcvd:    "59",
			RstSent:    "59",
		},
		ContactedStation: types.ContactedStation{
			Call: "M0CMC",
			Name: "Marc L",
		},
		LoggingStation: types.LoggingStation{
			StationCallsign: "7Q5MLV/T",
			MyName:          "Veary",
		},
		//		QslSection:       QslSection{},
	}

	adif := record.String()

	t.Log(adif)
}
