package sqlite

import (
	"context"
	"database/sql"
	stderr "errors"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// The country provider's answer for ONE callsign (W-0031; reference migration
// 0004). The country table is matched by prefix, so a row there answers for
// every call that begins with it; what is kept here answers for exactly the
// callsign it was given for. The two are kept apart so that an answer for one
// call, the provider's entry for a single portable callsign say, can never be
// applied to another.

// normalizeCallsignKey is the one form a callsign takes as this table's key.
func normalizeCallsignKey(callsign string) string {
	return strings.ToUpper(strings.TrimSpace(callsign))
}

// FetchCallsignCountryWithContext returns what was remembered for exactly this
// callsign, with its freshness stamp, or errors.ErrNotFound. It never matches a
// prefix.
func (s *Service) FetchCallsignCountryWithContext(ctx context.Context, callsign string) (types.Country, error) {
	const op errors.Op = "sqlite.Service.FetchCallsignCountryWithContext"
	if err := checkService(op, s); err != nil {
		return types.Country{}, err
	}
	key := normalizeCallsignKey(callsign)
	if key == "" {
		return types.Country{}, errors.New(op).WithMsg(errMsgEmptyCallsign)
	}
	h, err := s.getOpenHandle(op)
	if err != nil {
		return types.Country{}, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	var c types.Country
	err = h.QueryRowContext(ctx,
		`SELECT name, prefix, ccode, continent, cq_zone, itu_zone, dxcc_prefix, time_offset, last_refreshed_at
		   FROM callsign_country WHERE callsign = ?`, key).
		Scan(&c.Name, &c.Prefix, &c.Ccode, &c.Continent, &c.CQZone, &c.ITUZone, &c.DXCCPrefix, &c.TimeOffset,
			&c.LastRefreshedAt)
	if err != nil {
		if stderr.Is(err, sql.ErrNoRows) {
			return types.Country{}, errors.ErrNotFound
		}
		return types.Country{}, errors.New(op).WithErr(err)
	}
	return c, nil
}

// UpsertCallsignCountryWithContext remembers the provider's answer for exactly
// this callsign, replacing whole whatever was remembered before (a field that
// went empty upstream is cleared) and stamping it fresh. It writes nothing to
// the country table.
func (s *Service) UpsertCallsignCountryWithContext(ctx context.Context, callsign string, country types.Country) error {
	const op errors.Op = "sqlite.Service.UpsertCallsignCountryWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}
	key := normalizeCallsignKey(callsign)
	if key == "" {
		return errors.New(op).WithMsg(errMsgEmptyCallsign)
	}
	if strings.TrimSpace(country.Name) == "" {
		return errors.New(op).WithMsg("country.Name cannot be empty")
	}
	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	_, err = h.ExecContext(ctx,
		`INSERT INTO callsign_country
		     (callsign, last_refreshed_at, name, prefix, ccode, continent, cq_zone, itu_zone, dxcc_prefix, time_offset)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (callsign) DO UPDATE SET
		     last_refreshed_at = excluded.last_refreshed_at,
		     name = excluded.name, prefix = excluded.prefix, ccode = excluded.ccode,
		     continent = excluded.continent, cq_zone = excluded.cq_zone, itu_zone = excluded.itu_zone,
		     dxcc_prefix = excluded.dxcc_prefix, time_offset = excluded.time_offset`,
		key, time.Now().UTC(), country.Name, country.Prefix, country.Ccode, country.Continent,
		country.CQZone, country.ITUZone, country.DXCCPrefix, country.TimeOffset)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("remembering the callsign's country failed")
	}
	return nil
}
