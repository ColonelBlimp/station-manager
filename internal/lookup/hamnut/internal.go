package hamnut

import (
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
	"github.com/goccy/go-json"
	"strconv"
	"time"
)

// unmarshalResponse decodes a JSON response body into a Country object using the
// typed HamnutPrefixLookupResponse model, then maps it into types.Country.
func (s *Service) unmarshalResponse(body []byte) (types.Country, error) {
	const op errors.Op = "hamnut.Service.unmarshalResponse"
	var country types.Country

	var resp PrefixLookupResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return country, errors.New(op).Err(err).Msg("decoding Hamnut response")
	}

	// If the upstream explicitly reports not found but still returns 2xx, treat it
	// as a not-found condition to keep semantics consistent.
	if !resp.Found {
		return country, errors.New(op).Err(errors.ErrNotFound).Msg("prefix not found by Hamnut (found=false)")
	}

	// Map basic string fields directly.
	country.Name = resp.CountryName
	country.Prefix = resp.Prefix
	country.Ccode = resp.CountryCode
	country.Continent = resp.Continent
	country.DXCCPrefix = resp.PrimaryDXCCPrefix

	// Numeric zones -> string fields.
	if resp.CQZone != 0 {
		country.CQZone = strconv.Itoa(resp.CQZone)
	}
	if resp.ITUZone != 0 {
		country.ITUZone = strconv.Itoa(resp.ITUZone)
	}

	// Time offset handling: prefer a dedicated TimeOffset field if present; if not,
	// attempt to derive from LocalTime if it looks like an ISO timestamp with
	// timezone information. Otherwise, leave empty rather than panicking.
	if resp.TimeOffset != "" {
		country.TimeOffset = resp.TimeOffset
	} else if resp.LocalTime != "" {
		if t, err := time.Parse(time.RFC3339, resp.LocalTime); err == nil {
			_, offsetSec := t.Zone()
			offset := time.Duration(offsetSec) * time.Second
			// Format offset as "+HH:MM" or "-HH:MM".
			sign := "+"
			if offset < 0 {
				sign = "-"
				offset = -offset
			}
			hours := int(offset.Hours())
			minutes := int(offset.Minutes()) % 60
			country.TimeOffset = strconv.FormatInt(int64(hours), 10)
			if hours < 10 {
				country.TimeOffset = sign + "0" + strconv.Itoa(hours)
			} else {
				country.TimeOffset = sign + strconv.Itoa(hours)
			}
			if minutes < 10 {
				country.TimeOffset += ":0" + strconv.Itoa(minutes)
			} else {
				country.TimeOffset += ":" + strconv.Itoa(minutes)
			}
		} else if len(resp.LocalTime) >= 6 {
			// Fallback for legacy format: last 6 chars are assumed to be offset.
			country.TimeOffset = resp.LocalTime[len(resp.LocalTime)-6:]
		}
	}

	country.LocalTime = resp.LocalTime

	return country, nil
}
