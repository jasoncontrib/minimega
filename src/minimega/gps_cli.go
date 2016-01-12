// Copyright (2015) Sandia Corporation.
// Under the terms of Contract DE-AC04-94AL85000 with Sandia Corporation,
// the U.S. Government retains certain rights in this software.

package main

import (
	"fmt"
	"minicli"
	log "minilog"
	"strconv"
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
}

func init() {
	registerHandlers("gps", gpsCLIHandlers)
}

func cliGPS(c *minicli.Command) *minicli.Response {
	resp := &minicli.Response{Host: hostname}

	var vm *AndroidVM
	var err error

	if v := vms.findVm(c.StringArgs["vm"]); v == nil {
		resp.Error = vmNotFound(c.StringArgs["vm"]).Error()
		return resp
	} else if vm2, ok := v.(*AndroidVM); !ok {
		resp.Error = fmt.Sprintf("%v is not an Android VM", c.StringArgs["vm"])
		return resp
	} else {
		vm = vm2
	}

	if c.BoolArgs["push"] {
		lat, err := strconv.ParseFloat(c.StringArgs["lat"], 64)
		if err != nil {
			resp.Error = "lat must be a float"
			return resp
		}
		long, err := strconv.ParseFloat(c.StringArgs["long"], 64)
		if err != nil {
			resp.Error = "long must be a float"
			return resp
		}

		var accuracy float64 = 1.0 // best
		if c.StringArgs["accuracy"] != "" {
			accuracy, err = strconv.ParseFloat(c.StringArgs["accuracy"], 64)
			if err != nil {
				resp.Error = "accuracy must be a float"
				return resp
			}
		}

		nmea := toNMEAString(lat, long, accuracy)

		err = vm.PushGPS(nmea)
	} else if c.BoolArgs["push-raw"] {
		err = vm.PushGPS(c.StringArgs["raw"])
	} else {
		log.Fatal("invalid pattern matched")
	}

	if err != nil {
		resp.Error = err.Error()
	}

	return resp
}
