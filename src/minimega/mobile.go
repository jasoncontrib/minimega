// Copyright (2015) Sandia Corporation.
// Under the terms of Contract DE-AC04-94AL85000 with Sandia Corporation,
// the U.S. Government retains certain rights in this software.

package main

import (
	"errors"
	"math"
	"minicli"
	"strconv"
)

var mobileCLIHandlers = []minicli.Handler{
	{ // mobile ap
		HelpShort: "List, add, or remove wifi access points",
		HelpLong: `List, add, or remove wifi access points.

Run "mobile ap" with no arguments to see a list of access points.

To create a wifi access point, specify the "create" command. If only an SSID and VLAN (and optionally a bridge) are given, the access point will be visible to all VMs regardless of location. To make a location-dependent access point, specify latitude and longitude as decimal numbers (with negative numbers of South latitude/West longitude) and a value of milliwatt EIRP (Effective Isotropically Radiated Power) for the access point transmitter. The transmitter power and the latitude/longitude coordinates will be used to determine if the access point is visible to a given VM. 200mW is the FCC's EIRP limit for 802.11 operation.

	# Create a universally-visible network
	mobile ap create MY_NETWORK 100
	# Create a network at a specific location with an EIRP of 200mW
	mobile ap create Network2 20 37.7577 -122.4376 200
`,
		Patterns: []string{
			"mobile ap",
			"mobile ap <create,> <ssid> <vlan>",
			"mobile ap <create,> <ssid> <vlan> <bridge>",
			"mobile ap <create,> <ssid> <vlan> <lat> <long> <milliwatts>",
			"mobile ap <create,> <ssid> <vlan> <bridge> <lat> <long> <milliwatts>",
			"mobile ap <delete,> <ssid>",
		},
		Call: wrapBroadcastCLI(cliMobileAP),
	},
}

func init() {
	registerHandlers("mobile", mobileCLIHandlers)
}

func cliMobileAP(c *minicli.Command, resp *minicli.Response) error {
	if c.BoolArgs["create"] {
		ap := accessPoint{}
		var err error
		if c.StringArgs["milliwatts"] != "" {
			// This is a location-dependent station
			// Convert milliwatts EIRP into dBm
			eirp, err := strconv.ParseFloat(c.StringArgs["milliwatts"], 64)
			if err != nil {
				return errors.New("signal power must be a number")
			}
			ap.mW = eirp
			ap.power = int(0.5 + (10 * math.Log10(eirp))) // int(0.5 + f) rounds a float

			// We only have latitude and longitude if there's a power value
			lat, err := strconv.ParseFloat(c.StringArgs["lat"], 64)
			if err != nil {
				return errors.New("invalid latitude")
			}
			long, err := strconv.ParseFloat(c.StringArgs["long"], 64)
			if err != nil {
				return errors.New("invalid longitude")
			}
			ap.loc = location{lat: lat, long: long, accuracy: 1.0}
		}

		// Populate accessPoint struct
		ap.ssid = c.StringArgs["ssid"]
		ap.vlan, err = lookupVLAN(c.StringArgs["vlan"])
		if err != nil {
			return err
		}
		// There's not always a bridge!
		ap.bridge = DefaultBridge
		if c.StringArgs["bridge"] != "" {
			ap.bridge = c.StringArgs["bridge"]
		}

		// Save the access point
		wifiAPs[ap.ssid] = ap

		// Trigger a re-calculation of accessible APs for all Android devices
		updateAccessPointsVisible()
		return nil
	} else if c.BoolArgs["delete"] {
		ssid := c.StringArgs["ssid"]
		if _, ok := wifiAPs[ssid]; !ok {
			return errors.New("no such access point")
		}
		delete(wifiAPs, ssid)
		updateAccessPointsVisible()
		return nil
	}
	// List all APs
	apList(resp)
	return nil
}

func apList(resp *minicli.Response) {
	resp.Header = []string{"SSID", "VLAN", "Bridge", "Latitude", "Longitude", "Power"}
	resp.Tabular = [][]string{}

	for _, v := range wifiAPs {
		resp.Tabular = append(resp.Tabular, []string{v.ssid, strconv.Itoa(v.vlan), v.bridge,
			strconv.FormatFloat(v.loc.lat, 'f', 4, 64),
			strconv.FormatFloat(v.loc.long, 'f', 4, 64),
			strconv.FormatFloat(v.mW, 'f', 2, 64)})
	}
}
