// Copyright (2015) Sandia Corporation.
// Under the terms of Contract DE-AC04-94AL85000 with Sandia Corporation,
// the U.S. Government retains certain rights in this software.

package main

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"minicli"
	log "minilog"
	"minimodem"
	"net"
	"path"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

const (
	AndroidSerialPorts = 2
)

type NumberPrefix int

type AndroidConfig struct {
	NumberPrefix int
}

type AndroidVM struct {
	KvmVM         // embed
	AndroidConfig // embed

	Modem *minimodem.Modem

	gpsPath   string        // path to socket for communicating with vm
	gpsConn   net.Conn      // connection to GPS socket
	gpsWriter *bufio.Writer // writer for GPS socket

	Number int
}

// Ensure that vmAndroid implements the VM interface
var _ VM = (*AndroidVM)(nil)

var telephonyAllocs = map[int]chan int{}

var androidMasks = []string{
	"id", "name", "state", "memory", "vcpus", "type", "vlan", "bridge", "tap",
	"mac", "ip", "ip6", "bandwidth", "migrate", "disk", "snapshot", "initrd",
	"kernel", "cdrom", "append", "uuid", "number", "tags",
}

func init() {
	// Reset everything to default
	for _, fns := range androidConfigFns {
		fns.Clear(&vmConfig.AndroidConfig)
	}
}

// Copy makes a deep copy and returns reference to the new struct.
func (old *AndroidConfig) Copy() *AndroidConfig {
	res := new(AndroidConfig)

	// Copy all fields
	*res = *old

	return res
}

func (vm *AndroidConfig) String() string {
	// create output
	var o bytes.Buffer
	w := new(tabwriter.Writer)
	w.Init(&o, 5, 0, 1, ' ', 0)
	fmt.Fprintln(&o, "Current Android configuration:")
	fmt.Fprintf(w, "Telephony Prefix:\t%v\n", NumberPrefix(vm.NumberPrefix))
	w.Flush()
	fmt.Fprintln(&o)
	return o.String()
}

func NewAndroid(name string) *AndroidVM {
	vm := new(AndroidVM)

	vm.KvmVM = *NewKVM(name)
	vm.Type = Android

	vm.AndroidConfig = *vmConfig.AndroidConfig.Copy() // deep-copy configured fields

	// Clear the CPU
	if vm.CPU != "" {
		log.Info("clearing CPU type for Android VM")
		vm.CPU = ""
	}

	if vm.SerialPorts != 0 {
		log.Info("incrementing number of serial ports by %d for mobile sensors", AndroidSerialPorts)
	}

	vm.SerialPorts += AndroidSerialPorts

	return vm
}

func (vm *AndroidVM) String() string {
	return fmt.Sprintf("%s:%d:android", hostname, vm.ID)
}

func (vm *AndroidVM) Launch(ack chan int) error {
	// Create new ACK channel so that we can initialize sensors after kvm does
	// it's thing.
	localAck := make(chan int)

	// Call "super" Launch
	err := vm.KvmVM.Launch(localAck)
	if err != nil {
		return err
	}

	// Initialize sensors after VM boots
	go func() {
		<-localAck
		defer func() { ack <- vm.ID }()

		// Initialize GPS
		vm.gpsPath = path.Join(vm.instancePath, "serial0")
		vm.gpsConn, err = net.Dial("unix", vm.gpsPath)
		if err != nil {
			log.Error("start gps sensor: %v", err)
			vm.setState(VM_ERROR)
			return
		}
		vm.gpsWriter = bufio.NewWriter(vm.gpsConn)

		// Setup the modem
		prefix := vm.NumberPrefix
		vm.Number, err = NumberPrefix(prefix).Next(telephonyAllocs[prefix])
		if err != nil {
			log.Error("start telephony sensor: %v", err)
			vm.setState(VM_ERROR)
			return
		}

		outChan := make(chan minimodem.Message)

		vm.Modem, err = minimodem.NewModem(
			vm.Number,
			path.Join(vm.instancePath, "serial1"), // hard coded based on Android vm image
			outChan,
		)
		if err != nil {
			log.Error("start telephony sensor: %v", err)
			vm.setState(VM_ERROR)
			return
		}

		// Run the modem
		go vm.runModem()

		// Read from the modem's outbox periodically and send out the messages
		go vm.runPostman(outChan)
	}()

	return nil
}

func (vm *AndroidVM) runModem() {
	if err := vm.Modem.Run(); err != nil {
		log.Error("%v", err)
	}

	// TODO: Do something when run exits??
}

func (vm *AndroidVM) runPostman(outbox chan minimodem.Message) {
	for msg := range outbox {
		rawMsg, err := msg.Raw()
		if err != nil {
			log.Fatal("can't convert message to raw: %v", err)
		}
		rawCmd := fmt.Sprintf("sms deliver-raw %v", rawMsg)
		log.Debug("cmd: %v", rawCmd)
		cmd := minicli.MustCompile(rawCmd)
		cmd.Record = false

		// Broadcast message to the cluster
		for resps := range runCommandGlobally(cmd) {
			for _, resp := range resps {
				if resp.Error != "" {
					log.Warn("sms deliver error: %v", resp.Error)
				}
			}
		}
	}
}

func (vm *AndroidVM) Info(mask string) (string, error) {
	// If it's a field handled by the KvmVM, use it.
	if v, err := vm.KvmVM.Info(mask); err == nil {
		return v, nil
	}

	// If it's a configurable field, use the Print fn.
	if fns, ok := androidConfigFns[mask]; ok {
		return fns.Print(&vm.AndroidConfig), nil
	}

	switch mask {
	case "number":
		return fmt.Sprintf("%v", vm.Number), nil
	}

	return "", fmt.Errorf("invalid mask: %s", mask)
}

// Print padded prefix to length 10
func (n NumberPrefix) String() string {
	s := strconv.Itoa(int(n))
	if n == -1 {
		s = ""
	}
	return s + strings.Repeat("X", 10-len(s))
}

// Get the next number with assistance of chan
func (n NumberPrefix) Next(idChan chan int) (int, error) {
	id := <-idChan

	var prefixLen int
	for i := n; i > 0; i /= 10 {
		prefixLen += 1
	}
	if n == -1 {
		prefixLen = 0
	}

	// Must want a particular number... return the exact prefix
	if prefixLen == 10 {
		return int(n), nil
	}

	var idLen int
	for i := id; i > 0; i /= 10 {
		idLen += 1
	}
	if id == 0 {
		idLen = 1
	}

	if idLen+prefixLen > 10 {
		return 0, errors.New("no numbers left to allocate")
	}

	res := int(n)
	if n == -1 {
		res = 0
	}
	for i := 0; i < 10-prefixLen; i++ {
		res *= 10
	}

	return res + id, nil
}

func toNMEAString(lat, long, accuracy float64) string {
	// From: http://www.gpsinformation.org/dale/nmea.htm#GGA
	//
	// $GPGGA,123519,4807.038,N,01131.000,E,1,08,0.9,545.4,M,46.9,M,,*47
	// Where:
	//      GGA          Global Positioning System Fix Data
	//      123519       Fix taken at 12:35:19 UTC
	//      4807.038,N   Latitude 48 deg 07.038' N
	//      01131.000,E  Longitude 11 deg 31.000' E
	//      1            Fix quality: 0 = invalid
	//                                1 = GPS fix (SPS)
	//                                2 = DGPS fix
	//                                3 = PPS fix
	// 			       										4 = Real Time Kinematic
	// 			       										5 = Float RTK
	//                                6 = estimated (dead reckoning) (2.3 feature)
	// 			       										7 = Manual input mode
	// 			       										8 = Simulation mode
	//      08           Number of satellites being tracked
	//      0.9          Horizontal dilution of position
	//      545.4,M      Altitude, Meters, above mean sea level
	//      46.9,M       Height of geoid (mean sea level) above WGS84
	//                       ellipsoid
	//      (empty field) time in seconds since last DGPS update
	//      (empty field) DGPS station ID number
	//      *47          the checksum data, always begins with *

	format := "$GPGGA,%s,%09.4f,%s,%010.4f,%s,1,05,%.2f,0.0,M,0.0,M,,,*"

	latCoord := (math.Trunc(lat) * 100) + (60 * (lat - math.Trunc(lat)))
	longCoord := (math.Trunc(long) * 100) + (60 * (long - math.Trunc(long)))
	latCard := "N"
	if latCoord < 0 {
		latCard = "S"
		latCoord *= -1
	}

	longCard := "E"
	if longCoord < 0 {
		longCard = "W"
		longCoord *= -1
	}

	nmea := fmt.Sprintf(format,
		time.Now().UTC().Format("150405"),
		latCoord,
		latCard,
		longCoord,
		longCard,
		accuracy,
	)

	// The checksum field consists of a '*' and two hex digits representing an 8
	// bit exclusive OR of all characters between, but not including, the '$' and
	// '*'.
	var checksum byte
	nmeaBytes := []byte(nmea)
	for i, v := range nmeaBytes {
		if i == 0 || i == len(nmeaBytes) {
			continue
		}

		checksum ^= v
	}

	return nmea + hex.EncodeToString([]byte{checksum})
}

func (vm *AndroidVM) PushGPS(nmea string) error {
	log.Debug("Writing NMEA to VM: `%s`", nmea)
	vm.gpsWriter.WriteString(nmea)
	vm.gpsWriter.WriteString("\n")
	return vm.gpsWriter.Flush()
}
