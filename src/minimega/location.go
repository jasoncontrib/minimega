package main

import (
	"math"
)

type location struct {
	lat      float64
	long     float64
	accuracy float64
}

// Returns a distance, in meters, between two locations
func locationDistance(p1, p2 location) float64 {
	R := 6371000.0 // metres
	φ1 := p1.lat * math.Pi / 180
	φ2 := p2.lat * math.Pi / 180
	Δφ := (p2.lat - p1.lat) * math.Pi / 180
	Δλ := (p2.long - p1.long) * math.Pi / 180

	a := math.Sin(Δφ/2)*math.Sin(Δφ/2) +
		math.Cos(φ1)*math.Cos(φ2)*
			math.Sin(Δλ/2)*math.Sin(Δλ/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	d := R * c

	return d
}

// Check if p1 and p2 are "close enough", that is, within 1 meter.
func closeEnough(p1, p2 location) bool {
	if locationDistance(p1, p2) < 1.0 {
		return true
	}
	return false
}
