// Copyright (2015) Sandia Corporation.
// Under the terms of Contract DE-AC04-94AL85000 with Sandia Corporation,
// the U.S. Government retains certain rights in this software.

package main

import "testing"
import "math"

var testPrefixes = []struct {
	Prefix int
	Count  int
}{
	{123456, 10000},
	{12345678, 100},
	{1234567890, 1},
}

func TestNextNumberPrefix(t *testing.T) {
	for _, test := range testPrefixes {
		prefix := NumberPrefix(test.Prefix) // XXXX
		idChan := makeIDChan()

		for i := 0; i < test.Count; i++ {
			want := int(prefix)*test.Count + i

			got, err := prefix.Next(idChan)
			if err != nil {
				t.Fatalf("got error after %d iterations: %v", i, err)
			}
			if got != want {
				t.Fatalf("got: %d, want: %d", got, want)
			}
		}

		// Should have run out of numbers, next should return an error
		_, err := prefix.Next(idChan)
		if test.Count != 1 && err == nil {
			t.Fatalf("expected error after the %dth next", test.Count)
		}
	}
}

func TestNMEAString(t *testing.T) {
	lat, long := 37.7577, -122.4376
	accuracy := 1.0

	//want := "$GPGGA,%s,4124.8963,N,08151.6838,W,1,05,1.5,280.2,M,-34.0,M,,,*75"

	got := toNMEAString(lat, long, accuracy)

	t.Log(got)

	loc, err := fromNMEA(got)
	if err != nil {
		t.Fatal(err)
	}

	if math.Abs(lat-loc.lat) < 0.0001 && math.Abs(long-loc.long) < 0.0001 {
		t.Log("Successfully converted to NMEA and back")
	} else {
		t.Fatalf("Failed to convert to NMEA and back: original lat = %v, new = %v\noriginal long=%v, new = %v", lat, loc.lat, long, loc.long)
	}
}

func TestLocationDistance(t *testing.T) {
	loc1 := location{lat: 37, long: -122}
	loc2 := location{lat: 37.00001, long: -122}
	expectedDistance := 1.1112

	distance := locationDistance(loc1, loc2)
	if math.Abs(distance-expectedDistance) > .01 {
		t.Fatalf("seriously inaccurate results, expected %v meters, got %v", expectedDistance, distance)
	} else {
		t.Logf("Successfully measured distance, expected %v meters, got %v", expectedDistance, distance)
	}

	loc1 = location{lat: 37, long: -122}
	loc2 = location{lat: 37.001, long: -122.001}
	expectedDistance = 142.3

	distance = locationDistance(loc1, loc2)
	if math.Abs(distance-expectedDistance) > .1 {
		t.Fatalf("seriously inaccurate results, expected %v meters, got %v", expectedDistance, distance)
	} else {
		t.Logf("Successfully measured distance, expected %v meters, got %v", expectedDistance, distance)
	}
}
