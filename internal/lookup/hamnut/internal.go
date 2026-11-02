package hamnut

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/lookup"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// unmarshalResponse decodes a JSON response body into a types.Country
// using the typed PrefixLookupResponse model and maps the upstream
// shape into the canonical type.
//
// Hamnut returns 200 + `{"found": false}` when it doesn't have a
// prefix, so a successful HTTP call may still represent "no record."
// That explicit verdict becomes lookup.ErrPrefixNotRecognised (which wraps
// errors.ErrNotFound, so ADR 0017's "hamnut no-data: no row" still holds).
// A body that does not state `found` (`{}`, `null`, a missing or null field)
// is unusable rather than a verdict: it is an ordinary error, so a malformed
// response never raises the Phone form's unrecognised-callsign warning
// (W-0012, operator ruling 2026-11-02).
func (s *Service) unmarshalResponse(body []byte) (types.Country, error) {
	const op errors.Op = "hamnut.Service.unmarshalResponse"
	var country types.Country

	var resp PrefixLookupResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return country, errors.New(op).WithErr(err).WithMsg("decoding Hamnut response")
	}

	if resp.Found == nil {
		return country, errors.New(op).WithMsg("unusable Hamnut response: no found field")
	}
	if !*resp.Found {
		return country, errors.New(op).WithErr(lookup.ErrPrefixNotRecognised).
			WithMsg("prefix not recognised by Hamnut (found=false)")
	}

	country.Name = resp.CountryName
	country.Prefix = resp.Prefix
	country.Ccode = resp.CountryCode
	country.Continent = resp.Continent
	country.DXCCPrefix = resp.PrimaryDXCCPrefix
	if resp.CQZone != 0 {
		country.CQZone = strconv.Itoa(resp.CQZone)
	}
	if resp.ITUZone != 0 {
		country.ITUZone = strconv.Itoa(resp.ITUZone)
	}
	country.TimeOffset = deriveTimeOffset(resp)
	country.LocalTime = resp.LocalTime

	return country, nil
}

// deriveTimeOffset prefers the dedicated TimeOffset field; falls back
// to parsing LocalTime as RFC 3339 and extracting its zone; finally
// to the legacy "last 6 chars are the offset" heuristic. Returns ""
// when none of the strategies yield a usable value — the caller
// stores empty rather than fabricating an offset.
func deriveTimeOffset(resp PrefixLookupResponse) string {
	if resp.TimeOffset != "" {
		return resp.TimeOffset
	}
	if resp.LocalTime == "" {
		return ""
	}

	if t, err := time.Parse(time.RFC3339, resp.LocalTime); err == nil {
		_, offsetSec := t.Zone()
		offset := time.Duration(offsetSec) * time.Second
		sign := "+"
		if offset < 0 {
			sign = "-"
			offset = -offset
		}
		hours := int(offset.Hours())
		minutes := int(offset.Minutes()) % 60
		var buf string
		if hours < 10 {
			buf = sign + "0" + strconv.Itoa(hours)
		} else {
			buf = sign + strconv.Itoa(hours)
		}
		if minutes < 10 {
			buf += ":0" + strconv.Itoa(minutes)
		} else {
			buf += ":" + strconv.Itoa(minutes)
		}
		return buf
	}

	if len(resp.LocalTime) >= 6 {
		return resp.LocalTime[len(resp.LocalTime)-6:]
	}
	return ""
}
