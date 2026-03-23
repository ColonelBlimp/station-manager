package facade

import (
	"unicode"

	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
)

func mergeCountryIntoContactedStation(station *types.ContactedStation, country types.Country) error {
	const op errors.Op = "facade.mergeCountryIntoContactedStation"
	if station == nil {
		return errors.New(op).Msg("Contacted station parameter is nil")
	}
	station.Country = country.Name
	station.Cont = country.Continent

	return nil
}

func mergeIntoQso(qso *types.Qso, country types.Country, history []types.ContactHistory) error {
	const op errors.Op = "facade.mergeCountryIntoQso"
	if qso == nil {
		return errors.New(op).Msg("QSO parameter is nil")
	}
	if history == nil {
		history = make([]types.ContactHistory, 0)
	}

	qso.ContactHistory = history
	qso.CountryDetails = country

	return nil
}

// isAllNumbers checks if a string contains only numeric digits.
func isAllNumbers(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return len(s) > 0
}
