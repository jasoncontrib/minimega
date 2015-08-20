// Copyright (2015) Sandia Corporation.
// Under the terms of Contract DE-AC04-94AL85000 with Sandia Corporation,
// the U.S. Government retains certain rights in this software.

package main

import (
	"fmt"
	"minicli"
)

var wifiCLIHandlers = []minicli.Handler{
	{ // wifi
		HelpShort: "update the available wifi access points",
		HelpLong: `Update the available wifi access points.

Takes a list of SSIDs that should show up in Android, for example:

wifi 0 minitel mininet`,
		Patterns: []string{
			"wifi <vm id or name> [ssid]...",
		},
		Call: wrapSimpleCLI(cliWifi),
	},
}

func init() {
	registerHandlers("wifi", wifiCLIHandlers)
}

func cliWifi(c *minicli.Command) *minicli.Response {
	resp := &minicli.Response{Host: hostname}

	var vm *AndroidVM

	if v := vms.findVm(c.StringArgs["vm"]); v == nil {
		resp.Error = vmNotFound(c.StringArgs["vm"]).Error()
		return resp
	} else if vm2, ok := v.(*AndroidVM); !ok {
		resp.Error = fmt.Sprintf("%v is not an Android VM", c.StringArgs["vm"])
		return resp
	} else {
		vm = vm2
	}

	if len(c.ListArgs["ssid"]) == 0 {
		resp.Response = "TODO"
	} else {
		vm.SetWifiSSIDs(c.ListArgs["ssid"]...)
	}

	return resp
}
