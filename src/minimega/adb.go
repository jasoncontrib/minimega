// Copyright (2015) Sandia Corporation.
// Under the terms of Contract DE-AC04-94AL85000 with Sandia Corporation,
// the U.S. Government retains certain rights in this software.

package main

import (
	"minicli"
	"errors"
	"fmt"
	log "minilog"
)

var adbCLIHandlers = []minicli.Handler{
	{ // adb
		HelpShort: "Perform file/shell operations on Android devices",
		HelpLong: `Perform file/shell operations on Android devices.
`,
		Patterns: []string{
			"adb <push,> <vm id or name> <local file> <remote destination>",
			"adb <pull,> <vm id or name> <remote file> [local destination]",
			"adb <shell,> <vm id or name> <command>...",
		},
		Call: wrapBroadcastCLI(cliAdb),
	},
}

func init() {
	registerHandlers("adb", adbCLIHandlers)
}

func adbPush(vm, local, remote string) []error {
	// adb disconnect, this disconnects from everything!
	out, err := processWrapper("adb", "disconnect")
	if err != nil {
		return []error{ fmt.Errorf("%v: %v", err, out) }
	}

	applyFunc := func(vm VM, _ bool) (bool, error) {
		// Only operate on Android VMs, otherwise silently return
		if vt := vm.GetType(); vt != Android {
			return false, nil
		}
		// Get the first IP4 address
		nets := vm.GetNetworks()
		if len(nets) == 0 {
			return true, errors.New("vm does not have an ip address")
		}
		ip4 := nets[0].IP4

		// adb connect IP4:5555
		address := fmt.Sprintf("%v:5555", ip4)
		out, err := processWrapper("adb", "connect", address)
		if err != nil {
			return true, fmt.Errorf("%v: %v", err, out)
		}

		// adb push
		out, err = processWrapper("adb", "-s", address, "push", local, remote)
		if err != nil {
			return true, fmt.Errorf("%v: %v", err, out)
		}

		return true, nil
	}

	return vms.apply(vm, true, applyFunc)
}

func cliAdb(c *minicli.Command, resp *minicli.Response) error {
	vm := c.StringArgs["vm"]

	if c.BoolArgs["push"] {
		local := c.StringArgs["local"]
		remote := c.StringArgs["remote"]

		return makeErrSlice(adbPush(vm, local, remote))
	} else if c.BoolArgs["pull"] {
	} else if c.BoolArgs["shell"] {
	}
	return nil
}
