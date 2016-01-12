// Copyright (2015) Sandia Corporation.
// Under the terms of Contract DE-AC04-94AL85000 with Sandia Corporation,
// the U.S. Government retains certain rights in this software.

package main

import "testing"

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
}
