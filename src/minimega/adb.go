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
		HelpLong: `
Perform file/shell/APK operations on Android devices.

To transfer a file to all Android VMs (non-Android VMs are ignored):

	adb push all /path/to/local/file /data/somefile

To transfer a file from one VM to the local machine:

	adb pull android-vm /data/somefile /path/to/local/destination

To run a shell command on all devices (results are logged at the 'info' level):

	adb shell all pwd

To install an APK on all devices:

	adb install all /path/to/my_app.apk

NOTE: Although 'adb pull all <remote> <local>' is valid, it will result in
minimega pulling a file from all VMs and attempting to write all versions to
the same place. You almost certainly do not want this.
`,
		Patterns: []string{
			"adb <push,> <vm id or name> <local file> <remote destination>",
			"adb <pull,> <vm id or name> <remote file> [local destination]",
			"adb <shell,> <vm id or name> <command>...",
			"adb <install,> <vm id or name> <filename>",
		},
		Call: wrapBroadcastCLI(cliAdb),
	},
}

func init() {
	registerHandlers("adb", adbCLIHandlers)
}

func runAdbCommand(vm VM, args []string) (string, error) {
	// Get the first IP4 address
	nets := vm.GetNetworks()
	if len(nets) == 0 {
		return "", errors.New("vm does not have an ip address")
	}
	ip4 := nets[0].IP4

	// adb connect IP4:5555
	address := fmt.Sprintf("%v:5555", ip4)
	out, err := processWrapper("adb", "connect", address)
	if err != nil {
		return out, err
	}

	// run the command
	args = append([]string{"adb", "-s", address}, args...)
	out, err = processWrapper(args...)
	if err != nil {
		return out, err
	}
	log.Info("%v: %v", address, out)

	return out, nil
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

		args := []string{"push", local, remote}
		out, err = runAdbCommand(vm, args)
		if err != nil {
			return true, fmt.Errorf("%v: %v", err, out)
		}

		return true, nil
	}

	return vms.apply(vm, true, applyFunc)
}

func adbPull(vm, remote, local string) []error {
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

		args := []string{"pull", remote, local}
		out, err = runAdbCommand(vm, args)
		if err != nil {
			return true, fmt.Errorf("%v: %v", err, out)
		}

		return true, nil
	}

	return vms.apply(vm, true, applyFunc)
}

func adbInstall(vm, filename string) []error {
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

		args := []string{"install", filename}
		out, err = runAdbCommand(vm, args)
		if err != nil {
			return true, fmt.Errorf("%v: %v", err, out)
		}

		return true, nil
	}

	return vms.apply(vm, true, applyFunc)
}

func adbShell(vm string, command []string) []error {
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

		// adb push
		args := append([]string{"shell"}, command...)
		out, err = runAdbCommand(vm, args)
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
		local := c.StringArgs["local"]
		remote := c.StringArgs["remote"]

		return makeErrSlice(adbPull(vm, remote, local))
	} else if c.BoolArgs["shell"] {
		command := c.ListArgs["command"]

		return makeErrSlice(adbShell(vm, command))
	} else if c.BoolArgs["install"] {
		filename := c.StringArgs["filename"]

		return makeErrSlice(adbInstall(vm, filename))
	}
	return nil
}