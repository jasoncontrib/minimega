// Copyright (2015) Sandia Corporation.
// Under the terms of Contract DE-AC04-94AL85000 with Sandia Corporation,
// the U.S. Government retains certain rights in this software.

package main

import (
	"bufio"
	"fmt"
	"minicli"
	log "minilog"
	"errors"
	"minimodem"
	"os"
	"strconv"
	"strings"
)

var smsCLIHandlers = []minicli.Handler{
	{ // sms
		HelpShort: "",
		HelpLong:  ``,
		Patterns: []string{
			"sms <push,> <vm id or name> <from> <msg>...",
			"sms <history,> [vm id or name]",
			"sms <tap,>",
			"sms <tap,> <add,> <low range> <high range> <file path>",
			"sms <tap,> <delete,> <file path or all>",
			"sms <deliver,> <to> <from> <msg>...",
			"sms <deliver-raw,> <raw message in base64>",
			"sms <lookup,> <to>",
		},
		Call: wrapSimpleCLI(cliSMS),
	},
	{ // clear vnc
		HelpShort: "",
		HelpLong:  ``,
		Patterns: []string{
			"clear sms [vm id or name]",
		},
		Call: wrapSimpleCLI(cliSMSClear),
	},
}

func init() {
	registerHandlers("sms", smsCLIHandlers)
}

func cliSMS(c *minicli.Command, resp *minicli.Response) error {
	var androidVms []*AndroidVM
	if c.StringArgs["vm"] == Wildcard {
		androidVms = vms.FindAndroidVMs()
	} else {
		vm, err := vms.FindAndroidVM(c.StringArgs["vm"])
		if err != nil && c.StringArgs["vm"] != "" {
			return err
		}
		androidVms = append(androidVms, vm)
	}

	// TODO: Check errors?
	to, _ := normalizeNumber(c.StringArgs["to"])
	from, _ := normalizeNumber(c.StringArgs["from"])

	// TODO: Are there weird quoting issues?
	message := strings.Join(c.ListArgs["msg"], " ")

	if c.BoolArgs["push"] {
		for _, vm := range androidVms {
			msgs := minimodem.NewMessage(from, vm.Modem.Number, message)
			for _, msg := range msgs {
				if err := vm.Modem.PushSMS(msg); err != nil {
					return fmt.Errorf("error pushing message: %v", err)
				}
			}
		}
		return nil
	} else if c.BoolArgs["history"] {
		// List SMS history
		resp.Header = []string{"VM", "Source", "Dest", "Time", "Message"}
		resp.Tabular = [][]string{}

		getRow := func(vm *AndroidVM, m minimodem.Message) []string {
			return []string{
				strconv.Itoa(vm.GetID()),
				strconv.Itoa(m.Src),
				strconv.Itoa(m.Dst),
				m.Time.String(),
				m.Message,
			}
		}

		for _, vm := range androidVms {
			for _, m := range vm.Modem.Inbox {
				resp.Tabular = append(resp.Tabular, getRow(vm, m))
			}

			for _, m := range vm.Modem.Outbox {
				resp.Tabular = append(resp.Tabular, getRow(vm, m))
			}
		}
		return nil
	} else if c.BoolArgs["tap"] {
		if c.BoolArgs["add"] {
			low, err := normalizeNumber(c.StringArgs["low"])
			if err != nil {
				return fmt.Errorf("%v is not a valid number: %v", c.StringArgs["low"], err.Error())
			}
			high, err := normalizeNumber(c.StringArgs["high"])
			if err != nil {
				return fmt.Errorf("%v is not a valid number: %v", c.StringArgs["high"], err.Error())
			}
			filePath := c.StringArgs["file"]
			file, err := os.Create(filePath)
			if err != nil {
				return fmt.Errorf("can't open file %v: %v", filePath, err.Error())
			}
			output := bufio.NewWriter(file)
			if _, exists := smsHostTaps[filePath]; exists {
				return fmt.Errorf("already a tap using file %v", filePath)
			}

			smsHostTaps[filePath] = hostTap{low, high, file, output}
		} else if _, exists := c.BoolArgs["delete"]; exists {
			filePath := c.StringArgs["file"]
			isWild := filePath == Wildcard

			for path, tap := range smsHostTaps {
				if isWild || filePath == path {
					tap.Connection.Close()
					delete(smsHostTaps, path)
				}
			}
		} else { // must just want us to print out the tap table
			resp.Header = []string{"Low", "High", "Path"}
			resp.Tabular = [][]string{}

			for filePath, tap := range smsHostTaps {
				resp.Tabular = append(resp.Tabular, []string{
					strconv.Itoa(tap.LowRange),
					strconv.Itoa(tap.HighRange),
					filePath,
				})
			}
		}
		return nil
	} else if c.BoolArgs["deliver"] {
		log.Debug("delivering message to %d: `%s`", to, message)

		for _, vm := range vms.FindAndroidVMs() {
			if vm.Modem.Number == to {
				log.Debug("found %d at vm id %d", to, vm.GetID())
				msgs := minimodem.NewMessage(from, vm.Modem.Number, message)
				for _, msg := range msgs {
					if err := vm.Modem.PushSMS(msg); err != nil {
						return fmt.Errorf("error pushing message to %v: %v", vm.GetID(), err)
					}
				}
			}
		}

		// Also deliver to our taps, if applicable
		for path, tap := range smsHostTaps {
			log.Debug("Testing whether to deliver to %s at %s", to, path)
			if tap.LowRange <= to && to <= tap.HighRange {
				fmt.Fprintf(tap.Output, "%d -> %d \t%v\n", from, to, message)
				if err := tap.Output.Flush(); err != nil {
					log.Error("couldn't write to tap %v: %v", path, err)
				}
			}
		}
		return nil
	} else if c.BoolArgs["deliver-raw"] {
		log.Debug("deliver-raw message command initiated")
		raw := c.StringArgs["raw"]
		log.Debug(fmt.Sprintf("pulled out raw string: %v", raw))
		msg, err := minimodem.EatMessage(raw)
		if err != nil {
			log.Debug(fmt.Sprintf("errored while eating raw message: %v", err))
			return fmt.Errorf("can't convert raw %v to message: %v", raw, err)
		}
		log.Debug(fmt.Sprintf("ate raw message to %d: %v", msg.Dst, msg))

		for _, vm := range vms.FindAndroidVMs() {
			if vm.Modem.Number == msg.Dst {
				log.Debug("found %d at vm id %d", msg.Dst, vm.GetID())
				if err = vm.Modem.PushSMS(msg); err != nil {
					return fmt.Errorf("error pushing message to %v: %v", vm.GetID(), err)
				}
			}
		}

		// Also deliver to our taps, if applicable
		for path, tap := range smsHostTaps {
			log.Debug("Testing whether to deliver to %s at %s", to, path)
			if tap.LowRange <= msg.Dst && msg.Dst <= tap.HighRange {
				fmt.Fprintf(tap.Output, "%d -> %d\t%d of %d\t%v\n", msg.Src, msg.Dst, msg.PartId, msg.TotParts, msg.Message)
				if err := tap.Output.Flush(); err != nil {
					log.Error("couldn't write to tap %v: %v", path, err)
				}
			}
		}
		return nil
	} else if c.BoolArgs["lookup"] {
		ids := []int{}

		for _, vm := range vms.FindAndroidVMs() {
			if vm.Modem.Number == to {
				log.Debug("found %d at vm id %d", to, vm.GetID())
				ids = append(ids, vm.GetID())
			}
		}

		resp.Response = fmt.Sprintf("%v", ids)
		return nil
	}
	return errors.New("no sms command specified")
}

func cliSMSClear(c *minicli.Command, resp *minicli.Response) error {
	vm := c.StringArgs["range"]
	if vm == "" {
		vm = Wildcard
	}

	if err := smsClear(vm); err != nil {
		return err
	}

	return nil
}
