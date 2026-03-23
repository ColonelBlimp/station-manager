// Package facade: logbook-app
package facade

import (
	"fmt"
	"strings"

	"github.com/Station-Manager/database/sqlite"
	"github.com/Station-Manager/database/sqlite/meta"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
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
func (s *Service) FetchUiConfig() (types.UiConfig, error) {
	const op errors.Op = "facade.Service.UiConfig"

	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return types.UiConfig{}, err
	}

	requiredCfg, err := s.ConfigService.RequiredConfigs()
	if err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to fetch required configs.")
		return types.UiConfig{}, err
	}

	retVal := types.UiConfig{
		Logbook:            s.currentLogbook,
		PaginationPageSize: requiredCfg.PagingationPageSize,
		DefaultFwdEmail:    requiredCfg.DefaultFwdEmail,
	}

	return retVal, nil
}

func (s *Service) GetDatabaseMetadata() ([]meta.SqliteMeta, error) {
	const op errors.Op = "facade.Service.GetDatabaseFileList"

	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return nil, err
	}

	//dbDir := filepath.Dir(s.DatabaseService.DatabaseConfig.Path)
	//
	//metaList, err := meta.LoadSqliteDatabaseList(dbDir)
	//if err != nil {
	//	return nil, err
	//}

	//	fmt.Printf("Loaded metadata: %+v\n", metaList)

	//entries, err := os.ReadDir(dbDir)
	//if err != nil {
	//	err = errors.New(op).Err(err)
	//	s.LoggerService.ErrorWith().Err(err).Msg("Failed to read database directory.")
	//	return nil, err
	//}
	//
	//var slice []string
	//for _, entry := range entries {
	//	if entry.IsDir() {
	//		continue
	//	}
	//	matched, merr := filepath.Match("*.db", entry.Name())
	//	if merr != nil {
	//		merr = errors.New(op).Err(merr)
	//		s.LoggerService.ErrorWith().Err(merr).Msg("Failed to match database file.")
	//		return nil, merr
	//	}
	//	if matched {
	//		slice = append(slice, strings.ReplaceAll(entry.Name(), ".db", ""))
	//	}
	//}

	return nil, nil
}

func (s *Service) GetLogbookList() ([]types.Logbook, error) {
	const op errors.Op = "facade.Service.GetLogbookList"

	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return nil, err
	}

	slice, err := s.DatabaseService.FetchAllLogbooks()
	if err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to fetch logbook list.")
		return nil, err
	}

	return slice, nil
}

func (s *Service) NewLogbook(logbook types.Logbook) error {
	const op errors.Op = "facade.Service.NewLogbook"

	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return err
	}

	id, err := s.DatabaseService.InsertLogbook(logbook)
	if err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msgf("Failed to insert logbook: %+v", logbook)
		return err
	}

	logbook.ID = id

	return nil
}

func (s *Service) UpdateLogbook(logbook types.Logbook) error {
	return nil
}

func (s *Service) DeleteLogbook(id int64) error {
	const op errors.Op = "facade.Service.DeleteLogbook"

	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return err
	}

	if err := s.DatabaseService.DeleteLogbookByID(id); err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msgf("Failed to delete logbook with id: %d", id)
		return err
	}

	return nil
}

func (s *Service) SetCurrentLogbook(id int64) error {
	return nil
}

func (s *Service) GetQsoSlice(logbookId, pageNum, pageSize int64, ordering sqlite.Ordering) ([]types.Qso, error) {
	const op errors.Op = "facade.Service.GetQsoSlice"

	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return nil, err
	}

	if logbookId < 1 {
		return nil, errors.New(op).Msg("Logbook ID must be greater than 0")
	}
	if pageNum < 1 {
		return nil, errors.New(op).Msg("Page number must be greater than 0")
	}
	if pageSize < 1 {
		return nil, errors.New(op).Msg("Page size must be greater than 0")
	}

	slice, err := s.DatabaseService.FetchQsoSlicePaging(logbookId, pageNum, pageSize, ordering)
	if err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msgf("Failed to fetch QSO slice for logbook ID: %d", logbookId)
		return types.QsoSlice{}, err
	}

	if len(slice) == 0 {
		fmt.Println("No QSOs found for logbook ID: ", logbookId)
		return types.QsoSlice{}, nil
	}

	return slice, nil
}

// GetQsoCount retrieves the count of QSOs for a given logbook ID. Returns an error if the service is not initialized or input is invalid.
func (s *Service) GetQsoCount(logbookId int64) (int64, error) {
	const op errors.Op = "facade.Service.GetQsoCount"

	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return -1, err
	}

	if logbookId < 1 {
		return -1, errors.New(op).Msgf("Invalid logbook ID: %d", logbookId)
	}

	count, err := s.DatabaseService.FetchQsoCountByLogbookId(logbookId)
	if err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msgf("Failed to fetch QSO count for logbook ID: %d", logbookId)
		return -1, err
	}

	return count, nil
}

// ForwardQsosViaEmail forwards a list of QSO records by email to a specified recipient.
// Requires the service to be initialized and a valid recipient email address.
// Validates input IDs, retrieves QSOs, builds an email with an ADIF attachment, and sends it.
// Marks forwarded QSOs as sent upon success or logs errors if operations fail.
func (s *Service) ForwardQsosViaEmail(slice []int64, recipientEmail string) error {
	const op errors.Op = "facade.Service.ForwardQsosViaEmail"

	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return err
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

	var list types.QsoSlice
	for _, id := range slice {
		qso, err := s.DatabaseService.FetchQsoById(id)
		if err != nil {
			err = errors.New(op).Err(err)
			s.LoggerService.ErrorWith().Err(err).Msgf("Failed to fetch QSO with ID: %d", id)
			return errors.Root(err)
		}
		list = append(list, qso)
	}

	mail, err := s.EmailService.BuildEmailWithADIFAttachment("", "", "", []string{recipientEmail}, list)
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

	if err = s.markQsoSliceAsForwardedByEmail(list); err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msg("Failed to mark QSOs as forwarded by email")
		return errors.Root(err)
	}

	return nil
}

// GetQsoById retrieves a single QSO record from the database based on the provided ID. Returns an error if the operation fails.
func (s *Service) GetQsoById(id int64) (types.Qso, error) {
	const op errors.Op = "facade.Service.GetQsoById"
	if !s.initialized.Load() {
		err := errors.New(op).Msg(errMsgServiceNotInit)
		s.LoggerService.ErrorWith().Err(err).Msg(errMsgServiceNotInit)
		return types.Qso{}, err
	}

	if id < 1 {
		return types.Qso{}, errors.New(op).Msgf("Invalid QSO ID: %d", id)
	}

	qso, err := s.DatabaseService.FetchQsoById(id)
	if err != nil {
		err = errors.New(op).Err(err)
		s.LoggerService.ErrorWith().Err(err).Msgf("Failed to fetch QSO with ID: %d", id)
		return qso, err
	}

	return qso, nil
}
