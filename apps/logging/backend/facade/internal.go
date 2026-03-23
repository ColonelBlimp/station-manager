package facade

import (
	"strconv"
	"strings"
	"time"

	"github.com/Station-Manager/enums/upload/action"
	"github.com/Station-Manager/enums/upload/status"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/maidenhead"
	"github.com/Station-Manager/types"
)

// launchWorkerThread starts a new goroutine for the given worker function and manages its lifecycle using a wait group.
func (s *Service) launchWorkerThread(run *runState, workerFunc func(<-chan struct{}), workerName string) {
	run.wg.Add(1)
	go func() {
		defer run.wg.Done()
		s.LoggerService.InfoWith().Str("worker", workerName).Msg("Facade worker starting")
		workerFunc(run.shutdownChannel)
		s.LoggerService.InfoWith().Str("worker", workerName).Msg("Facade worker stopped")
	}()
}

// parseCallsign processes and cleans up a callsign string, removing trailing known modifiers and excess whitespace.
func (s *Service) parseCallsign(callsign string) string {
	str := strings.TrimSpace(callsign)
	if str == "" {
		return str
	}
	parts := strings.Split(str, "/")
	if len(parts) == 1 {
		return str
	}
	// Known trailing modifiers to strip
	known := map[string]struct{}{
		"P":        {},
		"PORTABLE": {},
		"M":        {},
		"MM":       {},
		"MOBILE":   {},
		"QRP":      {},
		"QRO":      {},
		"AM":       {},
		"PM":       {},
	}
	for len(parts) > 1 { // keep at least one segment
		last := strings.ToUpper(strings.TrimSpace(parts[len(parts)-1]))
		if _, ok := known[last]; ok {
			parts = parts[:len(parts)-1]
			continue
		}
		break
	}
	result := strings.Join(parts, "/")
	return result
}

// lookupCallsignOnline fetches details about the specified callsign from an online service (QRZ.com, HamQTH, etc.).
// Returns a ContactedStation object or an error in case of failure.
func (s *Service) lookupCallsignOnline(callsign string) (types.ContactedStation, error) {
	const op errors.Op = "facade.Service.lookupCallsignOnline"
	emptyRetVal := types.ContactedStation{}

	if !s.initialized.Load() {
		return emptyRetVal, errors.New(op).Msg("service is not initialized")
	}

	station, err := s.QrzLookupService.Lookup(callsign)
	if err != nil {
		return emptyRetVal, errors.New(op).Err(err).Msgf("Failed to lookup callsign: %s", callsign)
	}

	return station, nil
}

func (s *Service) calculateBearingAndDistance(country *types.Country, ls types.LoggingStation, cs types.ContactedStation) error {
	const op errors.Op = "facade.Service.calculateBearingAndDistance"
	if country == nil {
		return errors.New(op).Msg("country parameter is nil")
	}
	if ls.MyGridsquare == "" {
		return errors.New(op).Msg("logging station's gridsquare is empty")
	}
	if cs.Gridsquare == "" {
		return errors.New(op).Msg("contacted station's gridsquare is empty")
	}

	if location, err := maidenhead.GetLocation(ls.MyGridsquare, cs.Gridsquare); err != nil {
		s.LoggerService.WarnWith().Err(err).Msg("Failed to get location between logging station and contacted station")
	} else {
		country.ShortPathBearing = strconv.FormatFloat(location.ShortPathBearing, 'f', -1, 64)
		country.ShortPathDistance = strconv.Itoa(int(location.ShortPathDistanceKm))
		country.LongPathBearing = strconv.FormatFloat(location.LongPathBearing, 'f', -1, 64)
		country.LongPathDistance = strconv.Itoa(int(location.LongPathDistanceKm))
	}
	return nil
}

// initializeForwarding configures and initializes the internal forwarding service workers required for QSO upload processing.
func (s *Service) initializeForwarding() error {
	// Initialize the internal forwarding service workers
	s.forwarding = &forwarding{
		pollInterval:    s.requiredCfgs.QsoForwardingPollIntervalSeconds * time.Second,
		maxWorkers:      s.requiredCfgs.QsoForwardingWorkerCount,
		forwardingQueue: make(chan types.QsoUpload, s.requiredCfgs.QsoForwardingQueueSize),
		dbWriteQueue:    make(chan func() error, s.requiredCfgs.DatabaseWriteQueueSize), // Buffered to handle bursts
		fetchPending: func() ([]types.QsoUpload, error) {
			return s.DatabaseService.FetchPendingUploads()
		},
		sendAndMarkDone: func(qsoUpload types.QsoUpload) error {
			return s.forwardQsoWithSerializedDB(qsoUpload)
		},
		logger: s.LoggerService,
	}

	return nil
}

// forwardQsoWithSerializedDB forwards a QSO to the network service and serializes all database writes.
// This prevents SQLITE_BUSY errors during concurrent forwarding operations.
func (s *Service) forwardQsoWithSerializedDB(qsoUpload types.QsoUpload) error {
	const op errors.Op = "facade.Service.forwardQsoWithSerializedDB"

	if s.container == nil {
		return errors.New(op).Msg("container is nil. Call SetContainer before using the facade.")
	}

	// Check if forwarding is stopping before doing any work
	if s.forwarding == nil || s.forwarding.stopping.Load() {
		return errors.New(op).Msg("forwarding is stopping or not initialized")
	}

	provider, ok := s.forwarders[qsoUpload.Service]
	if !ok {
		return errors.New(op).Msgf("no forwarder found for service: %s", qsoUpload.Service)
	}

	// Phase 1: Network operation (can be concurrent)
	networkErr := s.forwardNetworkOnly(provider, qsoUpload)

	// Phase 2: Database operations (serialized through dedicated worker)
	// Check stopping flag again before sending to avoid race condition
	if s.forwarding.stopping.Load() {
		// Forwarding is stopping, execute DB update inline instead of through queue
		// This ensures we don't lose the database update
		return s.updateDatabaseOnly(qsoUpload, networkErr)
	}

	// Send the DB operation to the serialized queue.
	// Note: When the operation is queued successfully, this function returns nil immediately.
	// Any errors from updateDatabaseOnly are handled asynchronously by dbWriteWorkerLoop
	// and logged there. This is intentional - we want to return quickly from the network
	// operation and let the DB writes be serialized separately.
	dbOp := func() error {
		return s.updateDatabaseOnly(qsoUpload, networkErr)
	}

	// Non-blocking send to the queue
	select {
	case s.forwarding.dbWriteQueue <- dbOp:
		// Successfully queued - DB errors will be logged by dbWriteWorkerLoop
		return nil
	default:
		/*
			Queue size vs worker count: We have 5 forwarding workers (qso_forwarding_worker_count) but a queue of 100.
			Each worker would need to generate 20 pending DB writes before the queue fills.
			Single DB writer: The dbWriteWorkerLoop serializes all writes, so they execute sequentially.
			As long as the DB writer processes faster than workers enqueue (which it should, since network calls are
			slower than DB writes), the queue won't back up.
			Poll interval: With qso_forwarding_poll_interval_seconds of
			120 and qso_forwarding_row_limit of 5, you're only fetching 5 QSOs every 2 minutes. The queue has ample
			time to drain.

			If the queue fills and inline execution triggers, we'd have:
			1. The single dbWriteWorkerLoop goroutine writing to SQLite
			2. One or more forwarding workers also writing inline (concurrently)
			With our SQLite config (_busy_timeout: 10000, _journal_mode: WAL), concurrent writes would wait up to 10
			seconds. WAL mode allows concurrent reads with a single writer, but multiple writers still contend.

			Our configuration is safe for normal operation. The 100-slot queue provides a 20x buffer over your worker
			count. SQLITE_BUSY would only occur under extreme conditions (sustained network latency causing massive
			backlog) and even then, the 10-second busy timeout provides a safety net. The warning log message is
			appropriate - if we ever see it frequently, that would indicate a need to either increase the queue size
			or investigate why DB writes are slow.
		*/
		// Queue is full, execute inline and return any error directly
		s.LoggerService.WarnWith().Msg("DB write queue full, executing inline")
		return dbOp()
	}
}

// forwardNetworkOnly performs only the network call to the forwarding service.
// Database operations are handled separately to enable serialization.
func (s *Service) forwardNetworkOnly(provider interface{}, qsoUpload types.QsoUpload) error {
	const op errors.Op = "facade.Service.forwardNetworkOnly"

	// Type assertion to get the actual forwarder interface
	forwarder, ok := provider.(interface {
		ForwardNetworkOnly(qso types.Qso, param ...string) error
	})

	if !ok {
		// Fallback: if the provider doesn't implement ForwardNetworkOnly,
		// use the old Forward method (which includes DB writes - not ideal but maintains compatibility)
		s.LoggerService.WarnWith().Msgf("Provider %s does not implement ForwardNetworkOnly, using legacy Forward method", qsoUpload.Service)
		legacyForwarder, ok := provider.(interface {
			Forward(qso types.Qso, param ...string) error
		})
		if !ok {
			return errors.New(op).Msgf("provider %s does not implement Forward interface", qsoUpload.Service)
		}
		return legacyForwarder.Forward(qsoUpload.Qso, qsoUpload.Action)
	}

	return forwarder.ForwardNetworkOnly(qsoUpload.Qso, qsoUpload.Action)
}

// updateDatabaseOnly performs all database write operations for a forwarded QSO.
// This is called by the serialized DB write worker to prevent concurrent write conflicts.
func (s *Service) updateDatabaseOnly(qsoUpload types.QsoUpload, networkErr error) error {
	const op errors.Op = "facade.Service.updateDatabaseOnly"

	errState := ""
	uploadStatus := status.Failed

	if networkErr != nil {
		s.LoggerService.ErrorWith().Err(networkErr).Msgf("failed to forward QSO to %s", qsoUpload.Service)
		qsoUpload.Attempts++
		errState = errors.Root(networkErr).Error()
	} else {
		qsoUpload.Attempts = 0
		qsoUpload.LastError = ""
		uploadStatus = status.Uploaded
	}

	// Update qso_upload table
	act, _ := action.Parse(qsoUpload.Action) // Error discarded as Parse() always returns nil error
	uerr := s.DatabaseService.UpdateQsoUploadStatus(qsoUpload.ID, uploadStatus, act, qsoUpload.Attempts, errState)
	if uerr != nil {
		s.LoggerService.ErrorWith().Int64("qso_id", qsoUpload.QsoID).Str("service", qsoUpload.Service).Err(uerr).Msg("Database error: Failed to update upload status")
		return errors.New(op).Err(uerr)
	}

	// Update service-specific fields in qso table (e.g., QrzComUploadStatus)
	if networkErr == nil {
		// Get the provider to update its specific QSO fields
		provider, ok := s.forwarders[qsoUpload.Service]
		if ok {
			// Type assertion to get the database updater interface
			dbUpdater, hasDBUpdate := provider.(interface {
				UpdateDatabase(qso types.Qso) error
			})
			if hasDBUpdate {
				if err := dbUpdater.UpdateDatabase(qsoUpload.Qso); err != nil {
					s.LoggerService.ErrorWith().Err(err).Msgf("failed to update %s-specific QSO fields in database", qsoUpload.Service)
					return errors.New(op).Err(err)
				}
			}
		}
	}

	// Log the result
	if networkErr == nil {
		s.LoggerService.InfoWith().Int64("qso_id", qsoUpload.QsoID).Str("service", qsoUpload.Service).Msg("QSO uploaded successfully")
	} else {
		s.LoggerService.WarnWith().Int64("qso_id", qsoUpload.QsoID).Str("service", qsoUpload.Service).Err(networkErr).Msg("Failed to upload QSO")
	}

	return nil
}

func (s *Service) distanceAndDirection(qso types.Qso) (string, string) {
	if strings.ToUpper(qso.AntPath) == "S" {
		return qso.CountryDetails.ShortPathDistance, qso.CountryDetails.ShortPathBearing
	}
	return qso.CountryDetails.LongPathDistance, qso.CountryDetails.LongPathBearing
}
