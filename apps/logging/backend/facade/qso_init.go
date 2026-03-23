package facade

import (
	stderr "errors"

	"github.com/Station-Manager/adif"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
)

// initializeQso initializes a QSO object by populating its sections such as logging station, contacted station, and country details.
// It performs operations to calculate the necessary details like bearing and distance, and merges retrieved data into the final QSO object.
// If an error occurs during critical initialization steps, it returns an error.
func (s *Service) initializeQso(callsign string) (*types.Qso, error) {
	const op errors.Op = "facade.Service.initializeQso"

	loggingStation, err := s.initLoggingStationSection()
	if err != nil {
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to initialize the QSO's logging station section")
		return nil, errors.New(op).Err(err)
	}

	contactedStation, err := s.initContactedStationSection(callsign)
	if err != nil {
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to initialize the QSO's contacted station section")
		return nil, errors.New(op).Err(err)
	}

	country, err := s.initCountrySection(callsign)
	if err != nil {
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to initialize the QSO's country section")
		return nil, errors.New(op).Err(err)
	}

	if err = mergeCountryIntoContactedStation(contactedStation, country); err != nil {
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to merge country details into contacted station")
		return nil, errors.New(op).Err(err)
	}

	if err = s.calculateBearingAndDistance(&country, loggingStation, *contactedStation); err != nil {
		// Not a serious error, we can still continue.
		s.LoggerService.WarnWith().Err(err).Msg("Failed to calculate bearing and distance between stations")
	}

	qso := &types.Qso{
		QsoDetails:          s.initQsoDetailsSection(),
		LoggingStation:      loggingStation,
		ContactedStation:    *contactedStation,
		CountryDetails:      country,
		QrzComUploadStatus:  adif.NoString,
		SmQsoUploadStatus:   adif.NoString,
		SmFwrdByEmailStatus: adif.NoString,
	}

	history, err := s.getContactHistory(*contactedStation)
	if err != nil {
		// Serious error, but we can still continue.
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to fetch contact history")
	}

	if err = mergeIntoQso(qso, country, history); err != nil {
		return nil, errors.New(op).Err(err)
	}

	return qso, nil
}

// initLoggingStationSection initializes the logging station using the current logbook's callsign and configuration data.
// Returns a LoggingStation instance and an error if the station configuration retrieval fails.
func (s *Service) initLoggingStationSection() (types.LoggingStation, error) {
	const op errors.Op = "facade.Service.initLoggingStationSection"

	loggingStation, err := s.ConfigService.LoggingStationConfigs()
	if err != nil {
		return types.LoggingStation{}, errors.New(op).Err(err)
	}

	// All QSO must be logged to the current logbook, this is done by using the logbook's callsign
	// as the station callsign.
	loggingStation.StationCallsign = s.CurrentLogbook.Callsign

	return loggingStation, nil
}

// initContactedStationSection initializes or retrieves a contacted station's information based on the provided callsign.
// It attempts to fetch the station from the database or an online lookup if not found, returning the station details.
// The callsign provided is prioritized for the contacted station's "Call" field.
func (s *Service) initContactedStationSection(callsign string) (*types.ContactedStation, error) {
	const op errors.Op = "facade.Service.initContactedStation"
	parsedCallsign := s.parseCallsign(callsign)

	contactedStation, err := s.DatabaseService.FetchContactedStationByCallsign(parsedCallsign)
	if err != nil && !stderr.Is(err, errors.ErrNotFound) {
		// There is something seriously wrong with the database, so we can't continue.
		s.LoggerService.ErrorWith().Err(err).Msgf("Failed to fetch contacted station with callsign %s", parsedCallsign)
		return nil, errors.New(op).Err(err)
	}

	// New contact, so we must look up the details from somewhere else: online services.
	if stderr.Is(err, errors.ErrNotFound) {
		contactedStation, err = s.lookupCallsignOnline(parsedCallsign)
		if err != nil {
			// This is not a show-stopper, so we can continue without the contacted station details.
			s.LoggerService.ErrorWith().Err(err).Msgf("Failed to look up contacted station with callsign %s", parsedCallsign)
		}
	}

	// We use the callsign provided by the caller as this might contain more information than the callsign returned by
	// the lookup (for example, portable/mobile/etc. suffixes).
	contactedStation.Call = callsign

	return &contactedStation, nil
}

// initCountrySection initializes and retrieves country information based on the provided callsign.
// It fetches data from multiple sources, logs relevant information, and handles possible lookup errors.
func (s *Service) initCountrySection(callsign string) (types.Country, error) {
	const op errors.Op = "facade.Service.initCountrySection"

	parsedCallsign := s.parseCallsign(callsign)

	// Check the local database first.
	dbCountry, err := s.DatabaseService.FetchCountryByCallsign(parsedCallsign)
	if err != nil && !stderr.Is(err, errors.ErrNotFound) {
		// This is a major database problem, but we can continue without the country details.
		s.LoggerService.ErrorWith().Err(err).Msgf("Failed to fetch country details for callsign %s", parsedCallsign)
	}

	isNewEntity := false
	// Mark the country as new if it has not found in the database, but we need to confirm this status using the
	// country name, which we don't know until we do a lookup online. So, 'new' can mean new in the sense of a new entity (new country),
	// or new in the sense of a new prefix for the country already on the database.
	if stderr.Is(err, errors.ErrNotFound) {
		isNewEntity = true
	}

	// Look up the country online.
	country, err := s.HamnutLookupService.Lookup(parsedCallsign)
	if err != nil {
		return dbCountry, errors.New(op).Err(err)
	}

	if isNewEntity {
		dbCountry, err = s.DatabaseService.FetchCountryByName(country.Name)
		if err != nil && !stderr.Is(err, errors.ErrNotFound) {
			// This is a major database problem, but we can continue without the country details.
			s.LoggerService.ErrorWith().Err(err).Msgf("Failed online look up country details for %s", country.Name)
			isNewEntity = false
		}
	}

	if dbCountry != country {
		dbCountry = country
	}
	dbCountry.IsNewEntity = isNewEntity

	return dbCountry, nil
}

// initQsoDetailsSection initializes the QsoDetails section with default values and returns the QsoDetails object.
func (s *Service) initQsoDetailsSection() types.QsoDetails {
	return types.QsoDetails{
		AntPath: "S",
	}
}

// getContactHistory retrieves the contact history for a given contacted station from the database.
// Returns a slice of ContactHistory or an empty slice if no history exists, along with any errors encountered.
func (s *Service) getContactHistory(station types.ContactedStation) ([]types.ContactHistory, error) {
	const op errors.Op = "facade.Service.fetchWorkedHistory"

	callsign := s.parseCallsign(station.Call)
	history, err := s.DatabaseService.FetchQsoSliceByCallsign(callsign)
	if err != nil && !stderr.Is(err, errors.ErrNotFound) {
		return nil, errors.New(op).Err(err)
	}

	// The error at this point is ErrNotFound, which is fine.
	if history == nil {
		// We should not return nil
		history = make([]types.ContactHistory, 0)
	}

	return history, nil
}
