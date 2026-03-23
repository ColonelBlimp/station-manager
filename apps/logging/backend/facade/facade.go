package facade

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/Station-Manager/enums/cmds"
	"github.com/Station-Manager/enums/upload"
	"github.com/Station-Manager/enums/upload/action"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/maidenhead"
	"github.com/Station-Manager/types"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// allowedBrowserDomains is the allowlist of domains that can be opened in the browser.
// This prevents SSRF-style attacks and phishing via malicious URLs.
var allowedBrowserDomains = map[string]bool{
	"www.qrz.com":     true,
	"qrz.com":         true,
	"www.hamqth.com":  true,
	"hamqth.com":      true,
	"clublog.org":     true,
	"www.clublog.org": true,
	"lotw.arrl.org":   true,
}

// FetchUiConfig retrieves the UI configuration object. It returns an error if the service is not initialized, or the underlying
// ConfigService returns an error.
func (s *Service) FetchUiConfig() (*types.UiConfig, error) {
	const op errors.Op = "facade.Service.UiConfig"

	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return nil, err
	}

	requiredCfg, err := s.ConfigService.RequiredConfigs()
	if err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to fetch required configs.")
		return nil, err
	}

	loggingStation, err := s.ConfigService.LoggingStationConfigs()
	if err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to fetch logging station configs.")
		return nil, err
	}

	optionalCfg, err := s.ConfigService.OptionalConfigs()
	if err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to fetch optional configs.")
		return nil, err
	}

	return &types.UiConfig{
		DefaultRigID:       requiredCfg.DefaultRigID,
		Logbook:            s.CurrentLogbook,
		RigName:            s.CatService.RigConfig().Name,
		DefaultIsRandomQso: requiredCfg.DefaultIsRandomQso,
		DefaultTxPower:     requiredCfg.DefaultTxPower,
		UsePowerMultiplier: requiredCfg.UsePowerMultiplier,
		PowerMultiplier:    requiredCfg.PowerMultiplier,
		DefaultFreq:        requiredCfg.DefaultFreq,
		DefaultMode:        requiredCfg.DefaultMode,
		DefaultFwdEmail:    requiredCfg.DefaultFwdEmail,
		OwnersCallsign:     strings.ToUpper(loggingStation.OwnerCallsign),
		QrzViewUrl:         optionalCfg.QrzViewUrl,
	}, nil
}

// FetchCatStateValues retrieves the cat state values from the ConfigService.
func (s *Service) FetchCatStateValues() (map[string]map[string]string, error) {
	const op errors.Op = "facade.Service.FetchCatStateValues"

	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return nil, err
	}

	values, err := s.ConfigService.CatStateValues()
	if err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to fetch CAT state values.")
		return nil, err
	}

	return values, nil
}

// Ready checks if the service is initialized and started, then enqueues initialization and read commands to the CatService.
func (s *Service) Ready() error {
	const op errors.Op = "facade.Service.Ready"
	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return errors.Root(err)
	}

	if !s.started.Load() {
		err := errors.New(op).Msg(errMsgServiceNotStarted)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotStarted)
		return errors.Root(err)
	}

	if err := s.CatService.EnqueueCommand(cmds.Init); err != nil {
		s.LoggerService.ErrorWith().Err(err).Msgf("Failed to enqueue command: %s", cmds.Init)
		return errors.Root(err)
	}

	if err := s.CatService.EnqueueCommand(cmds.Read); err != nil {
		s.LoggerService.ErrorWith().Err(err).Msgf("Failed to enqueue command: %s", cmds.Read)
		return errors.Root(err)
	}

	return nil
}

// NewQso initializes a new QSO object with the given callsign.
func (s *Service) NewQso(callsign string) (*types.Qso, error) {
	const op errors.Op = "facade.Service.NewQso"
	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return nil, errors.Root(err)
	}

	if !s.started.Load() {
		err := errors.New(op).Msg(errMsgServiceNotStarted)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotStarted)
		return nil, errors.Root(err)
	}

	callsign = strings.ToUpper(strings.TrimSpace(callsign))

	if len(callsign) < 3 {
		return nil, errors.New(op).Msg(errMsgInvalidCallsign)
	}

	qso, err := s.initializeQso(callsign)
	if err != nil {
		return nil, errors.Root(err)
	}

	return qso, nil
}

// LogQso inserts a new QSO into the database.
func (s *Service) LogQso(qso types.Qso) error {
	const op errors.Op = "facade.Service.LogQso"
	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return errors.Root(err)
	}

	if !s.started.Load() {
		err := errors.New(op).Msg(errMsgServiceNotStarted)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotStarted)
		return errors.Root(err)
	}

	// Set the current session ID
	qso.SessionID = s.sessionID

	if err := s.validate.Struct(qso); err != nil {
		verr := errors.New(op).Msg("QSO Validation failed")
		s.LoggerService.ErrorWith().Err(err).Msg("QSO Validation failed")
		return verr
	}

	distance, direction := s.distanceAndDirection(qso)
	qso.Distance = distance
	qso.LoggingStation.AntennaAzimuth = direction

	// Insert the QSO into the database
	qsoId, err := s.DatabaseService.InsertQso(qso)
	if err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to insert QSO into database.")
		return errors.Root(err)
	}
	s.LoggerService.InfoWith().Str("callsign", qso.Call).Msg("QSO logged successfully")

	// Check if the contacted station exists in the database and insert or update it if it does not
	// match the current QSO's contacted station. The ContactedStation object is loaded when
	// the QSO is initialized.
	if err = s.insertOrUpdateContactedStation(qso.ContactedStation); err != nil {
		// This is a serious error, but not fatal, so log and carry on.
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to insert or update contacted station.")
	}

	if err = s.insertOrUpdateCountry(qso.CountryDetails); err != nil {
		// This is a serious error, but not fatal, so log and carry on.
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to insert or update country.")
	}

	// The last operation is to add an upload record.
	if err = s.DatabaseService.InsertQsoUpload(qsoId, action.Insert, upload.OnlineServiceQRZ); err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to insert QSO upload into database.")
		return errors.Root(err)
	}

	return nil
}

// UpdateQso updates an existing QSO record in the database and logs the operation; validates input and service state.
func (s *Service) UpdateQso(qso types.Qso) error {
	const op errors.Op = "facade.Service.UpdateQso"
	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return errors.Root(err)
	}
	if !s.started.Load() {
		err := errors.New(op).Msg(errMsgServiceNotStarted)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotStarted)
		return errors.Root(err)
	}

	if qso.ID < 1 {
		return errors.New(op).Msg("Invalid QSO ID")
	}

	if err := s.validate.Struct(qso); err != nil {
		verr := errors.New(op).Msg("QSO Validation failed")
		s.LoggerService.ErrorWith().Err(err).Msg("QSO Validation failed")
		return verr
	}

	if location, err := maidenhead.GetLocation(qso.MyGridsquare, qso.Gridsquare); err != nil {
		s.LoggerService.WarnWith().Err(err).Msg("Failed to get location between logging station and contacted station")
	} else {
		if qso.AntPath == "S" {
			qso.AntennaAzimuth = strconv.FormatFloat(location.ShortPathBearing, 'f', -1, 64)
			qso.Distance = strconv.Itoa(int(location.ShortPathDistanceKm))
		} else {
			qso.AntennaAzimuth = strconv.FormatFloat(location.LongPathBearing, 'f', -1, 64)
			qso.Distance = strconv.Itoa(int(location.LongPathDistanceKm))
		}
	}

	if err := s.DatabaseService.UpdateQso(qso); err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to update QSO in database.")
		return errors.Root(err)
	}

	if err := s.DatabaseService.InsertQsoUpload(qso.ID, action.Update, upload.OnlineServiceQRZ); err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to insert QSO upload into database.")
		return errors.Root(err)
	}

	return nil
}

// CurrentSessionQsoSlice retrieves the list of QSOs associated with the current session ID from the database.
func (s *Service) CurrentSessionQsoSlice() ([]types.Qso, error) {
	const op errors.Op = "facade.Service.CurrentSessionQsoSlice"
	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return nil, err
	}

	if !s.started.Load() {
		err := errors.New(op).Msg(errMsgServiceNotStarted)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotStarted)
		return nil, errors.Root(err)
	}

	list, err := s.DatabaseService.FetchQsoSliceBySessionID(s.sessionID)
	if err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to fetch QSOs by session ID.")
		return nil, errors.Root(err)
	}

	return list, nil
}

// OpenInBrowser opens the specified URL in the default web browser using the service's context.
func (s *Service) OpenInBrowser(urlStr string) error {
	const op errors.Op = "facade.Service.OpenInBrowser"
	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return err
	}

	if !s.started.Load() {
		err := errors.New(op).Msg(errMsgServiceNotStarted)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotStarted)
		return errors.Root(err)
	}

	if s.ctx == nil || (s.ctx.Err() != nil) {
		s.LoggerService.ErrorWith().Msg("Context is not set or cancelled")
		return errors.New(op).Msg("Context is not set or cancelled")
	}
	u, err := url.ParseRequestURI(urlStr)
	if err != nil || u == nil {
		err = errors.New(op).Err(err).Msg("Invalid URL")
		s.LoggerService.ErrorWith().Err(err).Str("url", urlStr).Msg("Invalid URL")
		return err
	}
	if u.Scheme != "https" {
		err = errors.New(op).Msg("URL scheme must be https")
		s.LoggerService.ErrorWith().Err(err).Str("url_scheme", u.Scheme).Msg("Invalid or unsafe URL")
		return err
	}

	// Check domain against allowlist
	host := strings.ToLower(u.Host)
	if !allowedBrowserDomains[host] {
		err = errors.New(op).Msg("Domain not in allowlist")
		s.LoggerService.ErrorWith().Err(err).Str("host", host).Msg("Blocked attempt to open non-allowlisted domain")
		return err
	}

	runtime.BrowserOpenURL(s.ctx, urlStr)

	return nil
}

// ForwardSessionQsosByEmail forwards a slice of QSOs to the specified email address as an ADIF attachment.
// Returns an error if the service is uninitialized, not started, input is invalid, or operation fails.
func (s *Service) ForwardSessionQsosByEmail(slice []types.Qso, recipientEmail string) error {
	const op errors.Op = "facade.Service.ForwardSessionQsosByEmail"
	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return errors.Root(err)
	}

	if !s.started.Load() {
		err := errors.New(op).Msg(errMsgServiceNotStarted)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotStarted)
		return errors.Root(err)
	}

	if len(slice) == 0 {
		err := errors.New(op).Msg("No QSOs to forward")
		s.LoggerService.ErrorWith().Err(err).Msg("No QSOs to forward")
		return errors.Root(err)
	}

	recipientEmail = strings.TrimSpace(recipientEmail)
	if err := s.validate.Var(recipientEmail, "required,email"); err != nil {
		verr := errors.New(op).Msg("Invalid recipient email address")
		s.LoggerService.ErrorWith().Err(err).Msg("Invalid recipient email address")
		return verr
	}

	mail, err := s.EmailService.BuildEmailWithADIFAttachment("", "", "", []string{recipientEmail}, slice)
	if err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to build email with ADIF attachment")
		return errors.Root(err)
	}

	if err = s.EmailService.Send(mail); err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to send email")
		return errors.Root(err)
	}

	if err = s.markQsoSliceAsForwardedByEmail(slice); err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to mark QSOs as forwarded by email")
		return errors.Root(err)
	}

	return nil
}

// GetQsoById retrieves a QSO record by its ID from the database. Returns an error if the service is not ready or ID is invalid.
func (s *Service) GetQsoById(id int64) (types.Qso, error) {
	const op errors.Op = "facade.Service.GetQsoById"
	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return types.Qso{}, errors.Root(err)
	}

	if !s.started.Load() {
		err := errors.New(op).Msg(errMsgServiceNotStarted)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotStarted)
		return types.Qso{}, errors.Root(err)
	}

	if id < 1 {
		return types.Qso{}, errors.New(op).Msg("Invalid QSO ID")
	}

	qso, err := s.DatabaseService.FetchQsoById(id)
	if err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to fetch QSO by ID")
		return types.Qso{}, errors.Root(err)
	}

	return qso, nil
}

// HasDefaultLogbook checks if a default logbook exists in the database. Returns true if it exists, otherwise false.
func (s *Service) HasDefaultLogbook() (bool, error) {
	const op errors.Op = "facade.Service.HasDefaultLogbook"

	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return false, errors.Root(err)
	}

	if !s.started.Load() {
		err := errors.New(op).Msg(errMsgServiceNotStarted)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotStarted)
		return false, errors.Root(err)
	}

	return s.DatabaseService.CheckDefaultLogbookExists()
}

func (s *Service) FinaliseSetup(logbook types.Logbook) error {
	const op errors.Op = "facade.Service.UpdateAppConfig"
	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return errors.Root(err)
	}

	logbook.ID = 1
	logbook.Callsign = strings.ToUpper(logbook.Callsign)

	if err := s.DatabaseService.UpsertLogbook(logbook); err != nil {
		s.LoggerService.ErrorWith().Err(err)
		return errors.Root(err)
	}

	cfg := s.ConfigService.AppConfig
	cfg.RequiredConfigs.SetupComplete = true

	err := s.ConfigService.UpdateAppConfig(cfg)
	if err != nil {
		s.LoggerService.ErrorWith().Err(err)
		return errors.Root(err)
	}

	return nil
}
