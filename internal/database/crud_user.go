package database

import (
	"context"
	"database/sql"
	stderr "errors"
	"github.com/Station-Manager/adapters"
	"github.com/Station-Manager/adapters/converters/common"
	"github.com/Station-Manager/database/postgres/models"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
	"github.com/aarondl/sqlboiler/v4/boil"
	"github.com/aarondl/sqlboiler/v4/queries/qm"
)

/*********************************************************************************************************************
Inser User Methods
**********************************************************************************************************************/

// InsertUser inserts a new user into the database and returns the inserted user or an error if any operation fails.
// This calls InsertUserContext with a background context.
func (s *Service) InsertUser(user types.User) (types.User, error) {
	const op errors.Op = "database.Service.InsertUser"
	if err := checkService(op, s); err != nil {
		return user, errors.New(op).Err(err)
	}
	//TODO: validate user struct
	ctx := context.Background()
	return s.InsertUserContext(ctx, user)
}

// InsertUserContext inserts a new user into the database and returns the inserted user or an error if any operation fails.
func (s *Service) InsertUserContext(ctx context.Context, user types.User) (types.User, error) {
	const op errors.Op = "database.Service.InsertUserContext"
	if err := checkService(op, s); err != nil {
		return user, errors.New(op).Err(err)
	}

	s.mu.RLock()
	h := s.handle
	isOpen := s.isOpen.Load()
	s.mu.RUnlock()
	if h == nil || !isOpen {
		return user, errors.New(op).Msg(errMsgNotOpen)
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = s.withDefaultTimeout(ctx)
		defer cancel()
	}

	adapter := adapters.New()
	adapter.RegisterConverter("PassHash", common.TypeToModelStringConverter)
	adapter.RegisterConverter("Issuer", common.TypeToModelStringConverter)
	adapter.RegisterConverter("Subject", common.TypeToModelStringConverter)
	adapter.RegisterConverter("Email", common.TypeToModelStringConverter)
	adapter.RegisterConverter("EmailConfirmed", common.TypeToModelBoolConverter)
	adapter.RegisterConverter("BootstrapHash", common.TypeToModelStringConverter)
	adapter.RegisterConverter("BootstrapExpiresAt", common.TypeToModelTimeConverter)

	var model models.User
	err := adapter.Into(&model, &user)
	if err != nil {
		return user, errors.New(op).Err(err)
	}

	if err = model.Insert(ctx, h, boil.Infer()); err != nil {
		return user, errors.New(op).Err(err)
	}

	user.ID = model.ID
	return user, nil
}

/*********************************************************************************************************************
Fetch User by Callsign Methods
**********************************************************************************************************************/

// FetchUserByCallsign returns a user by its callsign or an empty user if no user was found.
func (s *Service) FetchUserByCallsign(callsign string) (types.User, error) {
	const op errors.Op = "database.Service.InsertUser"
	if err := checkService(op, s); err != nil {
		return types.User{}, errors.New(op).Err(err)
	}
	ctx := context.Background()
	return s.FetchUserByCallsignContext(ctx, callsign)
}

// FetchUserByCallsignContext retrieves a user by their callsign from the database with the provided context.
// Returns an error if the database service is uninitialized, callsign is empty, or the user is not found.
// Uses a default timeout if no deadline is set in the given context.
func (s *Service) FetchUserByCallsignContext(ctx context.Context, callsign string) (types.User, error) {
	const op errors.Op = "database.Service.FetchUserByCallsign"
	emptyRetVal := types.User{}
	if err := checkService(op, s); err != nil {
		return emptyRetVal, errors.New(op).Err(err)
	}

	if callsign == emptyString {
		return emptyRetVal, errors.New(op).Msg("Callsign is empty")
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = s.withDefaultTimeout(ctx)
		defer cancel()
	}

	var mods []qm.QueryMod
	mods = append(mods, models.UserWhere.Callsign.EQ(callsign))
	model, err := models.Users(mods...).One(ctx, s.handle)
	if err != nil && !stderr.Is(err, sql.ErrNoRows) {
		return emptyRetVal, errors.New(op).Err(err)
	}

	if model == nil || err != nil {
		//TODO: Sentinal error
		return emptyRetVal, errors.New(op).Msg("User not found")
	}

	adapter := adapters.New()
	adapter.RegisterConverter("PassHash", common.ModelToTypeStringConverter)
	adapter.RegisterConverter("Issuer", common.ModelToTypeStringConverter)
	adapter.RegisterConverter("Subject", common.ModelToTypeStringConverter)
	adapter.RegisterConverter("Email", common.ModelToTypeStringConverter)
	adapter.RegisterConverter("EmailConfirmed", common.ModelToTypeBoolConverter)
	adapter.RegisterConverter("BootstrapHash", common.ModelToTypeStringConverter)
	adapter.RegisterConverter("BootstrapExpiresAt", common.ModelToTypeTimeConverter)

	var user types.User
	if err = adapter.Into(&user, model); err != nil {
		return emptyRetVal, errors.New(op).Err(err).Msg("Failed to convert model to user")
	}

	return user, nil
}

/*********************************************************************************************************************
Update User Methods
**********************************************************************************************************************/

// UpdateUser updates the provided user in the database using the given service instance and context.
func (s *Service) UpdateUser(user types.User) error {
	const op errors.Op = "database.Service.UpdateUser"
	if err := checkService(op, s); err != nil {
		return errors.New(op).Err(err)
	}
	ctx := context.Background()
	return s.UpdateUserContext(ctx, user)
}

// UpdateUserContext updates the database with the provided user data using a database handle and adapter for conversions.
// It ensures the database service is open, applies model converters, and validates context timeout before updating.
// Returns an error if the service is not operational, converters fail, or the update operation encounters an issue.
func (s *Service) UpdateUserContext(ctx context.Context, user types.User) error {
	const op errors.Op = "database.Service.UpdateUserContext"
	if err := checkService(op, s); err != nil {
		return errors.New(op).Err(err)
	}

	s.mu.RLock()
	h := s.handle
	isOpen := s.isOpen.Load()
	s.mu.RUnlock()
	if h == nil || !isOpen {
		return errors.New(op).Msg(errMsgNotOpen)
	}

	adapter := adapters.New()
	adapter.RegisterConverter("PassHash", common.TypeToModelStringConverter)
	adapter.RegisterConverter("Issuer", common.TypeToModelStringConverter)
	adapter.RegisterConverter("Subject", common.TypeToModelStringConverter)
	adapter.RegisterConverter("Email", common.TypeToModelStringConverter)
	adapter.RegisterConverter("EmailConfirmed", common.TypeToModelBoolConverter)
	adapter.RegisterConverter("BootstrapHash", common.TypeToModelStringConverter)
	adapter.RegisterConverter("BootstrapExpiresAt", common.TypeToModelTimeConverter)

	var model models.User
	err := adapter.Into(&model, &user)
	if err != nil {
		return errors.New(op).Err(err)
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = s.withDefaultTimeout(ctx)
		defer cancel()
	}

	if _, err = model.Update(ctx, h, boil.Infer()); err != nil {
		return errors.New(op).Err(err)
	}

	return nil
}
