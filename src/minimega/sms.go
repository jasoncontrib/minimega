// Copyright (2015) Sandia Corporation.
// Under the terms of Contract DE-AC04-94AL85000 with Sandia Corporation,
// the U.S. Government retains certain rights in this software.

package main

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
)

var (
	smsHostTaps = map[string]hostTap{} // key is the path to the socket, which should be unique
)

type hostTap struct {
	LowRange   int
	HighRange  int
	Connection io.Closer
	Output     *bufio.Writer
}

func normalizeNumber(num string) (int, error) {
	// Figure out how to normalize telephone numbers
	//   e.g. (925) 294-XXXX -> 925294XXXX
	// TODO
	v, err := strconv.Atoi(num)
	if err != nil {
		return 0, err
	}

	// TODO: Look for more complicated stuff

	return v, nil
}

func smsClear(vm string) error {
	if vm == Wildcard {
		for _, vm := range vms {
			vm, ok := vm.(*AndroidVM)
			if !ok {
				continue
			}

			vm.Modem.ClearHistory()
		}
	} else if v := vms.findVm(vm); v == nil {
		return vmNotFound(vm)
	} else if vm, ok := v.(*AndroidVM); !ok {
		return fmt.Errorf("%v is not an Android VM", vm)
	} else {
		vm.Modem.ClearHistory()
	}

	return nil
}
