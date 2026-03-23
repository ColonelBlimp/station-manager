package database

//
//import (
//	"context"
//	"database/sql"
//	stderr "errors"
//	"github.com/Station-Manager/adapters"
//	"github.com/Station-Manager/database/postgres/models"
//	"github.com/Station-Manager/errors"
//	"github.com/Station-Manager/types"
//	"github.com/aarondl/sqlboiler/v4/boil"
//)
//
//// InsertAPIKey inserts a new API key into the database with the given name, prefix, hash, and associated logbook ID.
//// Returns an error if the service is uninitialized, not open, or if insertion fails.
//func (s *Service) InsertAPIKey(name, prefix, hash string, logbookID int64) error {
//	const op errors.Op = "database.Service.InsertAPIKey"
//	if err := checkService(op, s); err != nil {
//		return errors.New(op).Err(err)
//	}
//	ctx := context.Background()
//	return s.InsertAPIKeyContext(ctx, name, prefix, hash, logbookID)
//}
//
//// InsertAPIKeyContext inserts a new API key into the database with the given name, prefix, hash, and associated logbook ID.
//// Returns an error if insertion fails.
//func (s *Service) InsertAPIKeyContext(ctx context.Context, name, prefix, hash string, logbookID int64) error {
//	const op errors.Op = "database.Service.InsertAPIKeyContext"
//	if name == "" || prefix == "" || hash == "" {
//		return errors.New(op).Msg("Name, prefix, and hash are required")
//	}
//
//	if logbookID < 1 {
//		return errors.New(op).Msg("Logbook ID not set")
//	}
//	model := models.APIKey{
//		LogbookID: logbookID,
//		KeyName:   name,
//		KeyHash:   hash,
//		KeyPrefix: prefix,
//	}
//
//	s.mu.RLock()
//	h := s.handle
//	isOpen := s.isOpen.Load()
//	s.mu.RUnlock()
//	if h == nil || !isOpen {
//		return errors.New(op).Msg(errMsgNotOpen)
//	}
//
//	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
//		var cancel context.CancelFunc
//		ctx, cancel = s.withDefaultTimeout(ctx)
//		defer cancel()
//	}
//
//	if err := model.Insert(ctx, h, boil.Infer()); err != nil {
//		return errors.New(op).Err(err)
//	}
//
//	return nil
//}
//
//// InsertAPIKeyWithTxContext inserts the API key using the provided transaction and context.
//// It mirrors InsertAPIKeyContext but uses the given ContextExecutor instead of the shared handle
//// so callers can coordinate it with other changes in a single atomic transaction.
//func (s *Service) InsertAPIKeyWithTxContext(ctx context.Context, tx boil.ContextExecutor, name, prefix, hash string, logbookID int64) error {
//	const op errors.Op = "database.Service.InsertAPIKeyWithTxContext"
//	if err := checkService(op, s); err != nil {
//		return errors.New(op).Err(err)
//	}
//
//	if name == "" || prefix == "" || hash == "" {
//		return errors.New(op).Msg("Name, prefix, and hash are required")
//	}
//
//	if logbookID < 1 {
//		return errors.New(op).Msg("Logbook ID not set")
//	}
//
//	if tx == nil {
//		return errors.New(op).Msg("transaction is nil")
//	}
//
//	model := models.APIKey{
//		LogbookID: logbookID,
//		KeyName:   name,
//		KeyHash:   hash,
//		KeyPrefix: prefix,
//	}
//
//	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
//		var cancel context.CancelFunc
//		ctx, cancel = s.withDefaultTimeout(ctx)
//		defer cancel()
//	}
//
//	if err := model.Insert(ctx, tx, boil.Infer()); err != nil {
//		return errors.New(op).Err(err)
//	}
//
//	return nil
//}
//
//// FetchAPIKeyByPrefix retrieves an API key from the database by matching the given prefix.
//// Returns the corresponding ApiKey and an error if the operation fails.
//func (s *Service) FetchAPIKeyByPrefix(prefix string) (types.ApiKey, error) {
//	const op errors.Op = "database.Service.FetchAPIKeyByPrefix"
//	if err := checkService(op, s); err != nil {
//		return types.ApiKey{}, errors.New(op).Err(err)
//	}
//	ctx := context.Background()
//	return s.FetchAPIKeyByPrefixContext(ctx, prefix)
//}
//
//// FetchAPIKeyByPrefixContext retrieves an API key matching the specified prefix within the provided context.
//// Returns types.ApiKey if found or an error if not found or if an issue occurs during execution.
//func (s *Service) FetchAPIKeyByPrefixContext(ctx context.Context, prefix string) (types.ApiKey, error) {
//	const op errors.Op = "database.Service.FetchAPIKeyByPrefix"
//	emptyRetVal := types.ApiKey{}
//	if err := checkService(op, s); err != nil {
//		return emptyRetVal, errors.New(op).Err(err)
//	}
//
//	s.mu.RLock()
//	h := s.handle
//	isOpen := s.isOpen.Load()
//	s.mu.RUnlock()
//	if h == nil || !isOpen {
//		return emptyRetVal, errors.New(op).Msg(errMsgNotOpen)
//	}
//
//	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
//		var cancel context.CancelFunc
//		ctx, cancel = s.withDefaultTimeout(ctx)
//		defer cancel()
//	}
//
//	model, err := models.APIKeys(models.APIKeyWhere.KeyPrefix.EQ(prefix)).One(ctx, h)
//	if err != nil && !stderr.Is(err, sql.ErrNoRows) {
//		return emptyRetVal, errors.New(op).Err(err)
//	}
//	if model == nil || err != nil {
//		return emptyRetVal, errors.New(op).Err(err).Errorf("prefix not found: %s", prefix)
//	}
//
//	var apiKey types.ApiKey
//	adapter := adapters.New()
//	if err = adapter.Into(&apiKey, model); err != nil {
//		return emptyRetVal, errors.New(op).Err(err)
//	}
//
//	return apiKey, nil
//}
