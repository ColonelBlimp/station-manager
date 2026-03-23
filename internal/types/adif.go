package types

import "time"

type ADIFDate struct {
	time.Time
}

type ADIFTime struct {
	time.Time
}

type ADIFFreq float64

type ADIFBand string
