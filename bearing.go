package maidenhead

import (
	"fmt"
	"math"
	"strconv"
	"unicode"
)

const (
	rounding  = 100000   // Used for rounding calculations to 5 decimal places
	earthRad  = 6371.0   // Earth radius in kilometers
	kmToMiles = 0.621371 // Conversion factor from kilometers to miles

	// Constants for Maidenhead grid square calculations
	asciiUpperA     = 65.0       // ASCII value for 'A'
	asciiLowerA     = 97.0       // ASCII value for 'a'
	fieldWidth      = 20.0       // Width of a field in degrees (longitude)
	fieldHeight     = 10.0       // Height of a field in degrees (latitude)
	squareWidth     = 2.0        // Width of a square in degrees (longitude)
	squareHeight    = 1.0        // Height of a square in degrees (latitude)
	subsquareWidth  = 5.0 / 60.0 // Width of a subsquare in degrees (longitude)
	subsquareHeight = 2.5 / 60.0 // Height of a subsquare in degrees (latitude)
)

type Location struct {
	LocalGridSquare        string  `json:"localGridSquare"`
	RemoteGridSquare       string  `json:"remoteGridSquare"`
	ShortPathBearing       float64 `json:"short_path_bearing"`
	LongPathBearing        float64 `json:"long_path_bearing"`
	ShortPathDistanceKm    int64   `json:"short_path_distance_km"`
	ShortPathDistanceMiles int64   `json:"short_path_distance_miles"`
	LongPathDistanceKm     int64   `json:"long_path_distance_km"`
	LongPathDistanceMiles  int64   `json:"long_path_distance_miles"`
}

// GetLocation calculates the distance, bearing, and other information between two Maidenhead Grid Square locations.
// It returns a `Location` struct containing the computed results or an error if the inputs are invalid.
// Grid square input is case-insensitive (e.g., JN58TD and jn58td are both accepted).
//
// Parameters:
//   - localGridSquare: The Maidenhead Grid Square of the local station (6 characters)
//   - remoteGridSquare: The Maidenhead Grid Square of the remote station (6 characters)
//
// Returns:
//   - *Location: A struct containing the bearing, distance in km and miles, and the original grid squares
//   - error: An error if either grid square is invalid or if calculations fail
func GetLocation(localGridSquare, remoteGridSquare string) (*Location, error) {
	// Calculate bearing between the two grid squares (short path)
	spBearing, err := GetShortPathBearing(localGridSquare, remoteGridSquare)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate short path bearing: %w", err)
	}

	// Calculate the distance between the two grid squares
	spDistanceKm, spDistanceMiles, err := GetShortPathDistance(localGridSquare, remoteGridSquare)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate short path distance: %w", err)
	}

	lpBearing, err := GetLongPathBearing(localGridSquare, remoteGridSquare)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate long path bearing: %w", err)
	}

	lpDistanceKm, lpDistanceMiles, err := GetLongPathDistance(localGridSquare, remoteGridSquare)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate long path distance: %w", err)
	}

	// Return the location information
	return &Location{
		LocalGridSquare:        localGridSquare,
		RemoteGridSquare:       remoteGridSquare,
		ShortPathBearing:       spBearing,
		LongPathBearing:        lpBearing,
		ShortPathDistanceKm:    int64(spDistanceKm),
		ShortPathDistanceMiles: int64(spDistanceMiles),
		LongPathDistanceKm:     int64(lpDistanceKm),
		LongPathDistanceMiles:  int64(lpDistanceMiles),
	}, nil
}

// gridSquareCoordinates represents the latitude and longitude of a grid square
type gridSquareCoordinates struct {
	Latitude  float64
	Longitude float64
}

// extractCoordinates extracts the latitude and longitude from a Maidenhead Grid Square
func extractCoordinates(gridSquare string) (*gridSquareCoordinates, error) {
	// Accept case-insensitive inputs by normalizing first
	lat, err := LatitudeFromGridSquare(gridSquare)
	if err != nil {
		return nil, err
	}

	long, err := LongitudeFromGridSquare(gridSquare)
	if err != nil {
		return nil, err
	}

	return &gridSquareCoordinates{
		Latitude:  lat,
		Longitude: long,
	}, nil
}

// GetShortPathBearing computes the initial bearing between two Maidenhead Grid Square locations.
// It takes two grid square strings (case-insensitive), validates them, and returns the bearing in degrees or an error if invalid.
//
// Parameters:
//   - localGridSquare: The Maidenhead Grid Square of the local station (6 characters)
//   - remoteGridSquare: The Maidenhead Grid Square of the remote station (6 characters)
//
// Returns:
//   - float64: The bearing in degrees from the local to the remote grid square (0-360°)
//   - error: An error if either grid square is invalid
func GetShortPathBearing(localGridSquare, remoteGridSquare string) (float64, error) {
	// Extract coordinates from the local grid square
	localCoords, err := extractCoordinates(localGridSquare)
	if err != nil {
		return 0, fmt.Errorf("invalid local grid square: %w", err)
	}

	// Extract coordinates from the remote grid square
	remoteCoords, err := extractCoordinates(remoteGridSquare)
	if err != nil {
		return 0, fmt.Errorf("invalid remote grid square: %w", err)
	}

	// Calculate the bearing between the coordinates
	return CalculateBearing(
		localCoords.Latitude,
		localCoords.Longitude,
		remoteCoords.Latitude,
		remoteCoords.Longitude,
	), nil
}

func GetLongPathBearing(localGridSquare, remoteGridSquare string) (float64, error) {
	shortPathBearing, err := GetShortPathBearing(localGridSquare, remoteGridSquare)
	if err != nil {
		return 0.0, fmt.Errorf("error calculating short path bearing: %w", err)
	}

	// Long path bearing is 180 degrees opposite of short path bearing
	longPathBearing := math.Mod(shortPathBearing+180, 360)

	// Handle negative angles
	if longPathBearing < 0 {
		longPathBearing += 360
	}

	return math.Round(longPathBearing*10) / 10, nil
}

// GetShortPathDistance calculates the distance in kilometers and miles between two Maidenhead Grid Square locations.
// It takes two grid square strings (case-insensitive) as input and returns the distances and an error if the inputs are invalid.
//
// Parameters:
//   - localGridSquare: The Maidenhead Grid Square of the local station (6 characters)
//   - remoteGridSquare: The Maidenhead Grid Square of the remote station (6 characters)
//
// Returns:
//   - float64: The distance in kilometers between the grid squares
//   - float64: The distance in miles between the grid squares
//   - error: An error if either grid square is invalid
func GetShortPathDistance(localGridSquare, remoteGridSquare string) (float64, float64, error) {
	// Extract coordinates from the local grid square
	localCoords, err := extractCoordinates(localGridSquare)
	if err != nil {
		return 0.0, 0.0, fmt.Errorf("invalid local grid square: %w", err)
	}

	// Extract coordinates from the remote grid square
	remoteCoords, err := extractCoordinates(remoteGridSquare)
	if err != nil {
		return 0.0, 0.0, fmt.Errorf("invalid remote grid square: %w", err)
	}

	// Convert coordinates to radians for calculation
	localLongRad := toRadians(localCoords.Longitude)
	localLatRad := toRadians(localCoords.Latitude)
	remoteLongRad := toRadians(remoteCoords.Longitude)
	remoteLatRad := toRadians(remoteCoords.Latitude)

	// Calculate differences in coordinates
	dLat := remoteLatRad - localLatRad
	dLon := remoteLongRad - localLongRad

	// Haversine formula for great-circle distance
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(localLatRad)*math.Cos(remoteLatRad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	// Calculate distances in kilometers and miles
	distanceKm := math.Ceil(earthRad * c)
	distanceMiles := math.Ceil(distanceKm * kmToMiles)

	return distanceKm, distanceMiles, nil
}

func GetLongPathDistance(localGridSquare, remoteGridSquare string) (float64, float64, error) {
	// Get the short path distance first
	shortPathKm, _, err := GetShortPathDistance(localGridSquare, remoteGridSquare)
	if err != nil {
		return 0.0, 0.0, err
	}

	// Calculate long path distance by subtracting short path from Earth's circumference
	earthCircumferenceKm := 2 * math.Pi * earthRad
	longPathKm := math.Ceil(earthCircumferenceKm - shortPathKm)
	longPathMiles := math.Ceil(longPathKm * kmToMiles)

	return longPathKm, longPathMiles, nil
}

// CalculateBearing calculates the initial bearing (or heading) from one point to another
// given their latitude and longitude coordinates.
//
// Parameters:
//   - lat1: Latitude of the starting point in degrees
//   - lon1: Longitude of the starting point in degrees
//   - lat2: Latitude of the destination point in degrees
//   - lon2: Longitude of the destination point in degrees
//
// Returns:
//   - float64: The initial bearing in degrees from the starting point to the destination (0-360°),
//     rounded to the nearest 0.1 degree
func CalculateBearing(lat1, lon1, lat2, lon2 float64) float64 {
	// Convert degrees to radians
	lat1Rad := toRadians(lat1)
	lon1Rad := toRadians(lon1)
	lat2Rad := toRadians(lat2)
	lon2Rad := toRadians(lon2)

	// Calculate the difference in longitude
	dLon := lon2Rad - lon1Rad

	// Calculate bearing using the formula:
	// θ = atan2(sin(Δlong) * cos(lat2), cos(lat1) * sin(lat2) - sin(lat1) * cos(lat2) * cos(Δlong))
	y := math.Sin(dLon) * math.Cos(lat2Rad)
	x := math.Cos(lat1Rad)*math.Sin(lat2Rad) - math.Sin(lat1Rad)*math.Cos(lat2Rad)*math.Cos(dLon)
	initialBearing := math.Atan2(y, x)

	// Convert bearing from radians to degrees
	initialBearing = toDegrees(initialBearing)

	// Normalize to 0-360 degrees
	if initialBearing < 0 {
		initialBearing += 360
	}

	// Round to the nearest 0.1 degree
	return math.Round(initialBearing*10) / 10
	//bearing, err := strconv.ParseFloat(fmt.Sprintf("%.1f", initialBearing), 64)
	//if err != nil {
	//	return 0.0 // Return 0.0 if rounding fails, though this should not happen
	//}
	//return bearing
}

// LatitudeFromGridSquare calculates the latitude from a Maidenhead Grid Square identifier.
// The input gridSquare is case-insensitive and must be a valid 6-character grid square format. Returns the latitude or an error if the input is invalid.
func LatitudeFromGridSquare(gridSquare string) (float64, error) {
	// Normalize case to expected Maidenhead format (AA99aa)
	normalized := normalizeGridSquare(gridSquare)
	if err := validateInput(normalized); err != nil {
		return 0.0, err
	}

	runes := []rune(normalized)

	// Field calculation (second character, A-R)
	// Each field is 10° tall, starting from -90°
	fieldLat := float64(runes[1]) - asciiUpperA
	fieldLatDegrees := fieldLat * fieldHeight

	// Square calculation (fourth character, 0-9)
	// Each square is 1° tall
	squareNum, err := strconv.Atoi(string(runes[3]))
	if err != nil {
		return 0.0, err
	}
	squareLatDegrees := float64(squareNum) * squareHeight

	// Subsquare calculation (sixth character, a-x)
	// Each subsquare is 2.5 minutes (2.5/60 degrees) tall
	subsquareLat := float64(runes[5]) - asciiLowerA
	subsquareLatDegrees := subsquareLat * subsquareHeight

	// Add center offset (half of subsquare height)
	centerOffset := subsquareHeight / 2.0

	// Calculate final latitude (-90° to +90°)
	latitude := fieldLatDegrees + squareLatDegrees + subsquareLatDegrees + centerOffset - 90.0

	// Round to 5 decimal places
	return math.Round(latitude*rounding) / rounding, nil
}

// LongitudeFromGridSquare calculates the longitude from a Maidenhead Grid Square and returns it as a float64.
// It expects a 6-character grid square string (case-insensitive) and validates its format before processing.
func LongitudeFromGridSquare(gridSquare string) (float64, error) {
	// Normalize case to expected Maidenhead format (AA99aa)
	normalized := normalizeGridSquare(gridSquare)
	if err := validateInput(normalized); err != nil {
		return 0, err
	}

	runes := []rune(normalized)

	// Field calculation (first character, A-R)
	// Each field is 20° wide, starting from -180°
	fieldLong := float64(runes[0]) - asciiUpperA
	fieldLongDegrees := fieldLong * fieldWidth

	// Square calculation (third character, 0-9)
	// Each square is 2° wide
	squareNum, err := strconv.Atoi(string(runes[2]))
	if err != nil {
		return 0, err
	}
	squareLongDegrees := float64(squareNum) * squareWidth

	// Subsquare calculation (fifth character, a-x)
	// Each subsquare is 5 minutes (5/60 degrees) wide
	subsquareLong := float64(runes[4]) - asciiLowerA
	subsquareLongDegrees := subsquareLong * subsquareWidth

	// Add the centre offset (half of subsquare width)
	centerOffset := subsquareWidth / 2.0

	// Calculate final longitude (-180° to +180°)
	longitude := fieldLongDegrees + squareLongDegrees + subsquareLongDegrees + centerOffset - 180.0

	// Round to 5 decimal places
	return math.Round(longitude*rounding) / rounding, nil
}

// validateInput checks if a grid square string follows the required format:
// - Must be 6 characters long
// - First two characters must be uppercase letters (A-Z)
// - Middle two characters must be digits (0-9)
// - Last two characters must be lowercase letters (a-z)
// normalizeGridSquare standardizes a provided grid square to the expected case pattern AA99aa.
// It uppercases the first two letters, keeps digits as-is, and lowercases the last two letters.
func normalizeGridSquare(s string) string {
	if len(s) != 6 {
		return s
	}
	runes := []rune(s)
	// Uppercase first two
	runes[0] = unicode.ToUpper(runes[0])
	runes[1] = unicode.ToUpper(runes[1])
	// Digits unchanged (2,3)
	// Lowercase last two
	runes[4] = unicode.ToLower(runes[4])
	runes[5] = unicode.ToLower(runes[5])
	return string(runes)
}

func validateInput(str string) error {
	if len(str) != 6 {
		return fmt.Errorf("invalid gridsquare format: %s (must be 6 characters)", str)
	}

	// Define the expected character types for each position
	validators := []struct {
		position int
		validate func(string, int) (bool, error)
		errMsg   string
	}{
		{0, isUpperARAtPosition, "first character must be A-R"},
		{1, isUpperARAtPosition, "second character must be A-R"},
		{2, isDigitAtPosition, "third character must be a digit"},
		{3, isDigitAtPosition, "fourth character must be a digit"},
		{4, isLowerAXAtPosition, "fifth character must be a-x"},
		{5, isLowerAXAtPosition, "sixth character must be a-x"},
	}

	// Check each position with its corresponding validator
	for _, v := range validators {
		ok, err := v.validate(str, v.position)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("invalid gridsquare format: %s (%s)", str, v.errMsg)
		}
	}

	return nil
}

func isUppercaseAtPosition(s string, pos int) (bool, error) {
	if pos < 0 || pos >= len(s) {
		return false, fmt.Errorf("position %d is out of range for string '%s'", pos, s)
	}
	char := rune(s[pos])

	return unicode.IsUpper(char), nil
}

func isUpperARAtPosition(s string, pos int) (bool, error) {
	ok, err := isUppercaseAtPosition(s, pos)
	if err != nil || !ok {
		return ok, err
	}
	char := rune(s[pos])
	if char < 'A' || char > 'R' {
		return false, nil
	}
	return true, nil
}

func isDigitAtPosition(s string, pos int) (bool, error) {
	if pos < 0 || pos >= len(s) {
		return false, fmt.Errorf("position %d is out of range for string '%s'", pos, s)
	}
	char := s[pos]
	if char < '0' || char > '9' {
		return false, nil
	}
	return true, nil
}

func isLowerAXAtPosition(s string, pos int) (bool, error) {
	if pos < 0 || pos >= len(s) {
		return false, fmt.Errorf("position %d is out of range for string '%s'", pos, s)
	}
	char := rune(s[pos])

	if !unicode.IsLower(char) {
		return false, nil
	}
	if char < 'a' || char > 'x' {
		return false, nil
	}
	return true, nil
}

func toRadians(degrees float64) float64 {
	return degrees * math.Pi / 180
}

func toDegrees(radians float64) float64 {
	return radians * 180 / math.Pi
}
