package hamnut

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// unmarshalResponse decodes a JSON response body into a types.Country
// using the typed PrefixLookupResponse model and maps the upstream
// shape into the canonical type.
//
// Hamnut returns 200 + `{"found": false}` when it doesn't have a
// prefix, so a successful HTTP call may still represent "no record."
// We translate this into errors.ErrNotFound so the orchestrator can
// branch the same way it does on a 404 — per ADR 0017's "hamnut no-
// data: no row" decision, the orchestrator writes nothing in either
// case.
func (s *Service) unmarshalResponse(body []byte) (types.Country, error) {
	const op errors.Op = "hamnut.Service.unmarshalResponse"
	var country types.Country

	var resp PrefixLookupResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return country, errors.New(op).WithErr(err).WithMsg("decoding Hamnut response")
	}

	if !resp.Found {
		return country, errors.New(op).WithErr(errors.ErrNotFound).
			WithMsg("prefix not found by Hamnut (found=false)")
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
