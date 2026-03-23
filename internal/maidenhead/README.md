# Station Manager: maidenhead package

This module provides a simple API for Maidenhead locator (grid square) calculations.

It can be used standalone (via its own `go.mod`) or as part of the wider Station Manager system.

## Features

- Convert Maidenhead grid squares (6-character locators like `JN58td`) to latitude/longitude.
- Compute great-circle (short-path) distance and initial bearing between two grid squares.
- Compute long-path distance and bearing (the complementary path around the globe).
- Provide a convenient `Location` struct bundling all of the above.

Inputs are case-insensitive: `JN58TD` and `jn58td` are treated identically.

## Installation

If you want to use this package directly in another Go project:

```bash
go get github.com/Station-Manager/maidenhead
```

Then import it in your code:

```go
import "github.com/Station-Manager/maidenhead"
```

## Basic usage

### Get a full location summary

```go
loc, err := maidenhead.GetLocation("JN58td", "FN31pr")
if err != nil {
    // handle error (invalid grid, etc.)
}

fmt.Printf("Short-path: bearing=%.1f°, distance=%d km (%d mi)\n",
    loc.ShortPathBearing,
    loc.ShortPathDistanceKm,
    loc.ShortPathDistanceMiles,
)
fmt.Printf("Long-path:  bearing=%.1f°, distance=%d km (%d mi)\n",
    loc.LongPathBearing,
    loc.LongPathDistanceKm,
    loc.LongPathDistanceMiles,
)
```

### Convert a grid square to latitude/longitude

```go
lat, err := maidenhead.LatitudeFromGridSquare("JN58td")
if err != nil {
    // handle invalid locator
}

lon, err := maidenhead.LongitudeFromGridSquare("JN58td")
if err != nil {
    // handle invalid locator
}

fmt.Printf("JN58td center: lat=%.5f lon=%.5f\n", lat, lon)
```

### Compute short-path distance and bearing only

```go
bearing, err := maidenhead.GetShortPathBearing("JN58td", "FN31pr")
if err != nil {
    // handle invalid input
}

km, miles, err := maidenhead.GetShortPathDistance("JN58td", "FN31pr")
if err != nil {
    // handle invalid input
}

fmt.Printf("Short path: bearing=%.1f°, distance=%.0f km (%.0f mi)\n", bearing, km, miles)
```

### Compute long-path distance and bearing

```go
lpBearing, err := maidenhead.GetLongPathBearing("JN58td", "FN31pr")
if err != nil {
    // handle invalid input
}

lpKm, lpMiles, err := maidenhead.GetLongPathDistance("JN58td", "FN31pr")
if err != nil {
    // handle invalid input
}

fmt.Printf("Long path: bearing=%.1f°, distance=%.0f km (%.0f mi)\n", lpBearing, lpKm, lpMiles)
```

## API overview

### Types

#### `type Location struct`

```go
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
```

### Exported functions

All functions live in the `maidenhead` package.

- `GetLocation(localGrid, remoteGrid string) (*Location, error)`  
  High-level helper: validates the given 6-character grid squares, computes short- and long-path bearings and distances, and returns them in a `Location` struct.

- `GetShortPathBearing(localGrid, remoteGrid string) (float64, error)`  
  Returns the initial great-circle bearing (0–360°, rounded to 0.1°) from `localGrid` to `remoteGrid`.

- `GetLongPathBearing(localGrid, remoteGrid string) (float64, error)`  
  Returns the bearing for the complementary “long path” (opposite side of the globe), also in degrees.

- `GetShortPathDistance(localGrid, remoteGrid string) (km, miles float64, err error)`  
  Returns the great-circle distance between two locators in kilometers and miles, both rounded up using `math.Ceil`.

- `GetLongPathDistance(localGrid, remoteGrid string) (km, miles float64, err error)`  
  Returns the long-path distance (Earth circumference minus short-path distance), in kilometers and miles.

- `LatitudeFromGridSquare(grid string) (float64, error)`  
  Converts a 6-character Maidenhead locator to latitude (center of the subsquare). Input is case-insensitive.

- `LongitudeFromGridSquare(grid string) (float64, error)`  
  Converts a 6-character Maidenhead locator to longitude (center of the subsquare). Input is case-insensitive.

- `CalculateBearing(lat1, lon1, lat2, lon2 float64) float64`  
  Low-level helper that returns the initial great-circle bearing between two latitude/longitude points in degrees.

## Validation rules

The current implementation expects **6-character** Maidenhead grid squares in the form `AA99aa`:

- 1st and 2nd characters: letters `A`–`R` (fields), case-insensitive.
- 3rd and 4th characters: digits `0`–`9` (squares).
- 5th and 6th characters: letters `a`–`x` (subsquares), case-insensitive on input.

Invalid strings (wrong length or characters out of range) will result in an error from the conversion/lookup functions.

## Testing and coverage

From the repository root you can run:

```bash
cd maidenhead

go test ./...
```

From the Station Manager root (where `go.work` lives), you can run tests and coverage for just this module:

```bash
cd /home/mveary/Development/Station-Manager

go test ./maidenhead -coverprofile=maidenhead.cover.out -covermode=count

go tool cover -func=maidenhead.cover.out | grep maidenhead
```

This module currently has tests covering bearing math, grid validation and normalization, coordinate conversion, and short/long path distance and bearing calculations.

## Notes and limitations

- The Earth is modeled as a sphere with radius 6371 km (standard great-circle assumptions); results are approximate but suitable for radio/contest logging and routing use-cases.
- Distances are **rounded up** to the nearest kilometer/mile using `math.Ceil`.
- Bearings are normalized into the range `[0, 360)` and rounded to one decimal place.
- Only 6-character grid squares are supported by this package at present.
