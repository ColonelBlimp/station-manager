package utils

import "math"

// Great-circle bearing + distance between two Maidenhead locators, for the
// daemon-side derivation of ADIF ANT_AZ / DISTANCE / ANT_PATH when logging a
// QSO (FT8, where the daemon builds the record). The TypeScript mirror lives
// at frontend/app/src/lib/utils/bearing.ts; the two are kept in sync
// (same Earth radius, same rounding) so a QSO logged via the daemon and one
// logged via the SPA carry identical geometry.

// earthRadiusKm is the mean Earth radius used by both the haversine distance
// and the long-path "circumference minus short" derivation. Matches bearing.ts.
const earthRadiusKm = 6371.0

// PathGeo carries the short- and long-path bearing (degrees, 0–360, 0.1°
// precision) and great-circle distance (km, ceil-rounded) between two points.
type PathGeo struct {
	ShortBearingDeg float64
	LongBearingDeg  float64
	ShortDistanceKm float64
	LongDistanceKm  float64
}

// GridPath returns the short/long path geometry from localGrid to remoteGrid.
// ok is false when either locator is empty or invalid — the caller should then
// leave ANT_AZ / DISTANCE unset (the path choice can still stamp ANT_PATH).
func GridPath(localGrid, remoteGrid string) (PathGeo, bool) {
	lat1, lon1, ok1 := MaidenheadToDecimal(localGrid)
	lat2, lon2, ok2 := MaidenheadToDecimal(remoteGrid)
	if !ok1 || !ok2 {
		return PathGeo{}, false
	}

	short := initialBearing(lat1, lon1, lat2, lon2)
	long := math.Round(math.Mod(short+180, 360)*10) / 10

	shortDist := haversineKm(lat1, lon1, lat2, lon2)
	// Near-antipodal grids + float rounding can push shortDist a hair past the
	// great-circle limit, yielding a tiny negative long path — clamp to 0
	// (matches bearing.ts).
	longDist := math.Max(0, math.Ceil(2*math.Pi*earthRadiusKm-shortDist))

	return PathGeo{
		ShortBearingDeg: short,
		LongBearingDeg:  long,
		ShortDistanceKm: shortDist,
		LongDistanceKm:  longDist,
	}, true
}

// initialBearing is the great-circle initial bearing from point 1 to point 2,
// in degrees 0–360, rounded to 0.1°.
func initialBearing(lat1, lon1, lat2, lon2 float64) float64 {
	lat1Rad := lat1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180

	y := math.Sin(dLon) * math.Cos(lat2Rad)
	x := math.Cos(lat1Rad)*math.Sin(lat2Rad) - math.Sin(lat1Rad)*math.Cos(lat2Rad)*math.Cos(dLon)
	bearing := math.Atan2(y, x) * 180 / math.Pi
	if bearing < 0 {
		bearing += 360
	}
	return math.Round(bearing*10) / 10
}

// haversineKm is the great-circle distance in km, ceil-rounded (matches v1 +
// bearing.ts).
func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	lat1Rad := lat1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return math.Ceil(earthRadiusKm * c)
}
