// Copyright (2015) Sandia Corporation.
// Under the terms of Contract DE-AC04-94AL85000 with Sandia Corporation,
// the U.S. Government retains certain rights in this software.

package main

import (
	"errors"
	"math"
	"math/rand"
	"minicli"
	log "minilog"
	"strconv"
	"time"
)

var gpsCLIHandlers = []minicli.Handler{
	{ // gps
		HelpShort: "update GPS location of an Android VM",
		HelpLong: `Update the GPS location of an Android VM.

Supports sending coordinates in the form of latitude, longitude (optionally
including accuracy). For example:

	gps push <name> 34.444 34.444

TODO: Write up description of how these are degrees and we convert them into
real lat/long. Explain +/- for E/W, N/S. Other things.

Or, pushing a full NMEA sentence (note that only $GPGGA sentences are supported
by Android):

	gps push-raw $GPGGA,170834,4124.8963,N,08151.6838,W,1,05,1.5,280.2,M,-34.0,M,,,*75

NMEA sentences are explained here:
	http://aprs.gids.nl/nmea/#gga`,
		Patterns: []string{
			"gps <push,> <vm id or name> <lat> <long> [accuracy]",
			"gps <push-raw,> <vm id or name> <raw NMEA string>",
		},
		Call: wrapSimpleCLI(cliGPS),
	},
	{
		HelpShort: "start or stop Android VM GPS-wandering",
		HelpLong: `Start or stop an Android VM's GPS-wandering.

An Android VM can be made to "wander" at walking or driving pace. It will choose random points within a certain radius of the last manually-set (via 'gps push') GPS location and travel to those points.

	# Start wandering at walking speeds
	gps wander walking

	# Start wandering at driving speeds
	gps wander driving

	# Stop wandering
	gps wander stop
`,
		Patterns: []string{
			"gps wander <walking,> <vm id or name>",
			"gps wander <driving,> <vm id or name>",
			"gps wander <stop,> <vm id or name>",
		},
		Call: wrapSimpleCLI(cliGPSWander),
	},
}

func init() {
	registerHandlers("gps", gpsCLIHandlers)
}

func cliGPS(c *minicli.Command, resp *minicli.Response) error {
	vm, err := vms.FindAndroidVM(c.StringArgs["vm"])
	if err != nil {
		return err
	}

	if c.BoolArgs["push"] {
		lat, err := strconv.ParseFloat(c.StringArgs["lat"], 64)
		if err != nil {
			return errors.New("lat must be a float")
		}
		long, err := strconv.ParseFloat(c.StringArgs["long"], 64)
		if err != nil {
			return errors.New("long must be a float")
		}

		var accuracy float64 = 1.0 // best
		if c.StringArgs["accuracy"] != "" {
			accuracy, err = strconv.ParseFloat(c.StringArgs["accuracy"], 64)
			if err != nil {
				return errors.New("accuracy must be a float")
			}
		}

		nmea := toNMEAString(lat, long, accuracy)

		return vm.PushGPS(nmea)
	} else if c.BoolArgs["push-raw"] {
		return vm.PushGPS(c.StringArgs["raw"])
	}

	return errors.New("unreachable")
}

func cliGPSWander(c *minicli.Command, resp *minicli.Response) error {
	vm, err := vms.FindAndroidVM(c.StringArgs["vm"])
	if err != nil {
		return err
	}

	// Check if we're walking, driving, or stopping
	if c.BoolArgs["stop"] {
		// Set movement speed to zero and return
		vm.moveSpeed = 0.0
	} else if c.BoolArgs["walking"] {
		// Set the speed to a walking pace
		vm.moveSpeed = 0.000007
	} else if c.BoolArgs["driving"] {
		// Set the speed to a driving pace
		vm.moveSpeed = 0.0001
	}

	return nil
}

func gpsMove() {
	for {
		for _, vm := range vms.FindAndroidVMs() {
			if vm.GetState() != VM_RUNNING {
				continue
			}
			if vm.moveSpeed == 0.0 {
				// this one isn't moving
				continue
			}
			// Check if we're close enough to the destination
			if closeEnough(vm.destinationLocation, vm.currentLocation) || (vm.destinationLocation.lat == 0 && vm.destinationLocation.long == 0) {
				log.Info("VM %v reached its destination %v.", vm.GetName(), vm.destinationLocation)
				// If so, pick a new destinationLocation within ~15km of the origin
				// TODO: make the radius more intelligent
				// 1. Choose a random Δlat & Δlong of up to 0.1
				// TODO: CHANTGED 0.1 TO 0.01 FOR DEBUGGING
				Δlat := rand.Float64() * 0.01
				Δlong := rand.Float64() * 0.01

				// 2. Half the time, the Δ should be negative
				if rand.Float64() < 0.5 {
					Δlat *= -1
				}
				if rand.Float64() < 0.5 {
					Δlong *= -1
				}

				// Set the new destinationLocation
				vm.destinationLocation = location{lat: vm.originLocation.lat + Δlat, long: vm.originLocation.long + Δlong}
				log.Info("VM %v new destination is %v", vm.GetName(), vm.destinationLocation)
			}

			// Move toward the destination point
			// Calculate Δlat and Δlong
			var Δlat, Δlong float64
			if diff := vm.destinationLocation.lat - vm.currentLocation.lat; math.Abs(diff) < vm.moveSpeed {
				Δlat = diff
			} else {
				Δlat = vm.moveSpeed
				if vm.destinationLocation.lat < vm.currentLocation.lat {
					Δlat *= -1
				}
			}
			if diff := vm.destinationLocation.long - vm.currentLocation.long; math.Abs(diff) < vm.moveSpeed {
				Δlong = diff
			} else {
				Δlong = vm.moveSpeed
				if vm.destinationLocation.long < vm.currentLocation.long {
					Δlong *= -1
				}
			}

			// Update currentLocation
			vm.currentLocation.lat += Δlat
			vm.currentLocation.long += Δlong
			log.Info("VM %v moved to point %v", vm.GetName(), vm.currentLocation)
		}
		updateAccessPointsVisible()
		time.Sleep(1 * time.Second)
	}
}
