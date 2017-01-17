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
	"miniwifi"
	"net"
	"path"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

const (
	// GPS: 0
	// Telephony: 1
	// Accelerometer: 2
	AndroidSerialPorts = 3
	// Wifi: 0,1
	AndroidVirtioPorts = 2
)

type NumberPrefix int64

type AndroidConfig struct {
	// Set the telephone network number prefix for newly launched VMs. For example:
	//
	// 	vm config telephony 223344
	//
	// Would cause newly launched VMs to be assigned numbers 2233440000, 2233440001,
	// and so on. Number blocks should not overlap, the following produces an error:
	//
	// 	vm config telephony 223344
	// 	vm config telephony 22334
	//
	// Note: this configuration only applies to Android-based VMs.
	NumberPrefix int64
}

type accessPoint struct {
	ssid   string
	vlan   int
	bridge string
	loc    location
	power  int     // power is in dBm, not milliwatts!
	mW     float64 // We keep this for display
}

// Calculate the signal strength in dBm of the access point as seen
// from the given location.
func (ap *accessPoint) signalStrength(loc location) float64 {
	// Wifi frequency is approximately 2400 MHz
	f := 2400.0
	// Find distance in meters
	d := locationDistance(loc, ap.loc)
	if d == 0 {
		d = 0.01
	}

	// Calculate Free-Space Path Loss
	// 27.55 is the constant for meters & megahertz
	fspl := 20*math.Log10(d) + 20*math.Log10(f) - 27.55

	signal := float64(ap.power) - fspl
	return signal
}

type AndroidVM struct {
	KvmVM         // embed
	AndroidConfig // embed

	Modem     *minimodem.Modem
	wifiModem *miniwifi.Modem

	gpsPath   string        // path to socket for communicating with vm
	gpsConn   net.Conn      // connection to GPS socket
	gpsWriter *bufio.Writer // writer for GPS socket

	originLocation      location // the most recent manually-set location of this phone
	currentLocation     location // the most recent location of this phone
	destinationLocation location // wherever this VM is wandering to
	moveSpeed           float64  // Actually a Δlat/long to reach approximate walk/drive speeds
	// about 0.000007 for walking, 0.0004 for driving

	Number int

	accessPoints map[string]accessPoint // map ssid to vlan/bridge
}

var wifiAPs map[string]accessPoint // Global collection of access points

// Ensure that vmAndroid implements the VM interface
var _ VM = (*AndroidVM)(nil)

var telephonyAllocs = map[int]*Counter{}

var androidMasks = []string{
	"id", "name", "state", "memory", "vcpus", "type", "vlan", "bridge", "tap",
	"mac", "ip", "ip6", "bandwidth", "migrate", "disk", "snapshot", "initrd",
	"kernel", "cdrom", "append", "uuid", "number", "tags",
}

// TODO get rid of this?
func init() {
	telephonyAllocs[int(vmConfig.NumberPrefix)] = NewCounter()

	wifiAPs = make(map[string]accessPoint)

	go gpsMove()
}

// Copy makes a deep copy and returns reference to the new struct.
func (old AndroidConfig) Copy() AndroidConfig {
	// Copy all fields
	res := old

	// Make deep copy of slices

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

func NewAndroid(name, namespace string, config VMConfig) (*AndroidVM, error) {
	vm := new(AndroidVM)

	nkvm, err := NewKVM(name, namespace, config)
	if err != nil {
		return nil, err
	}
	vm.KvmVM = *nkvm
	vm.Type = Android

	vm.AndroidConfig = vmConfig.AndroidConfig.Copy() // deep-copy configured fields

	// Clear the CPU
	if vm.CPU != "" {
		log.Info("clearing CPU type for Android VM")
		vm.CPU = ""
	}

	if vm.SerialPorts != 0 {
		log.Info("incrementing number of serial ports by %d for mobile sensors", AndroidSerialPorts)
	}

	vm.SerialPorts += AndroidSerialPorts

	if vm.VirtioPorts != 0 {
		log.Info("incrementing number of virtio ports by %d for mobile sensors", AndroidVirtioPorts)
	}

	vm.VirtioPorts += AndroidVirtioPorts

	return vm, nil
}

func (vm *AndroidVM) Copy() VM {
	vm.lock.Lock()
	defer vm.lock.Unlock()

	vm2 := new(AndroidVM)

	// Make shallow copies of all fields
	*vm2 = *vm

	// We copied a locked VM so we have to unlock it too...
	//defer vm2.lock.Unlock()

	// Make deep copies
	vm2.BaseConfig = vm.BaseConfig.Copy()
	vm2.KVMConfig = vm.KVMConfig.Copy()
	vm2.AndroidConfig = vm.AndroidConfig.Copy()

	return vm2
}

func (vm *AndroidVM) String() string {
	return fmt.Sprintf("%s:%d:android", hostname, vm.ID)
}

func (vm *AndroidVM) Launch() error {
	defer vm.lock.Unlock()
	// Call "super" Launch on the KVM VM
	if err := vm.KvmVM.launch(); err != nil {
		return err
	}

	var err error

	// Initialize GPS
	vm.gpsPath = path.Join(vm.instancePath, "serial0")
	vm.gpsConn, err = net.Dial("unix", vm.gpsPath)
	if err != nil {
		log.Error("start gps sensor: %v", err)
		vm.setError(err)
		return err
	}
	vm.gpsWriter = bufio.NewWriter(vm.gpsConn)

	// Setup the modem
	prefix := vm.NumberPrefix
	vm.Number, err = NumberPrefix(prefix).Next(telephonyAllocs[int(prefix)])
	if err != nil {
		log.Error("start telephony sensor: %v", err)
		vm.setError(err)
		return err
	}

	outChan := make(chan minimodem.Message)

	vm.Modem, err = minimodem.NewModem(
		vm.Number,
		path.Join(vm.instancePath, "serial1"), // hard coded based on Android vm image
		outChan,
	)
	if err != nil {
		log.Error("start telephony modem: %v", err)
		vm.setError(err)
		return err
	}

	// Run the modem
	go vm.runModem()

	// Read from the modem's outbox periodically and send out the messages
	go vm.runPostman(outChan)

	// Setup the wifi
	vm.wifiModem, err = miniwifi.NewModem(
		path.Join(vm.instancePath, "virtio-serial0"), // hard coded based on Android vm image
		path.Join(vm.instancePath, "virtio-serial1"), // hard coded based on Android vm image
	)
	if err != nil {
		log.Error("start wifi modem: %v", err)
		vm.setError(err)
		return err
	}

	go vm.wifiModem.Run()

	go func() {
		for {
			network := <-vm.wifiModem.NetworkNameChan
			log.Debug("VM %v changed to wifi network named %v", vm.GetName(), network)
			if net, ok := vm.accessPoints[network]; ok {
				log.Debug("VM %v switching to vlan %v on bridge %v", vm.GetName(), net.vlan, net.bridge)
				vm.NetworkConnect(0, net.bridge, net.vlan)
			}
		}
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

func (vm *AndroidVM) Info(field string) (string, error) {
	// If the field is handled by KvmVM, return it.
	if v, err := vm.KvmVM.Info(field); err == nil {
		return v, nil
	}

	vm.lock.Lock()
	defer vm.lock.Unlock()

	switch field {
	case "number":
		return strconv.Itoa(vm.Number), nil
	}

	return vm.AndroidConfig.Info(field)
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
func (n NumberPrefix) Next(c *Counter) (int, error) {
	id := c.Next()

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

func fromNMEA(nmea string) (location, error) {
	parts := strings.Split(nmea, ",")
	if len(parts) < 6 {
		return location{}, errors.New("overly short string")
	}

	lat, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return location{}, err
	}
	latCard := parts[3]
	long, err := strconv.ParseFloat(parts[4], 64)
	if err != nil {
		return location{}, err
	}
	longCard := parts[5]

	latInt := math.Trunc(lat / 100)
	latRemainder := lat - latInt*100
	latCoord := latInt + (latRemainder / 60)
	if latCard == "S" {
		latCoord *= -1
	}

	longInt := math.Trunc(long / 100)
	longRemainder := long - longInt*100
	longCoord := longInt + (longRemainder / 60)
	if longCard == "W" {
		longCoord *= -1
	}

	location := location{lat: latCoord, long: longCoord, accuracy: 1.0}

	return location, nil
}

func (vm *AndroidVM) PushGPS(nmea string) error {
	log.Debug("Writing NMEA to VM: `%s`", nmea)
	location, err := fromNMEA(nmea)
	if err != nil {
		return err
	}
	vm.currentLocation = location
	vm.originLocation = location
	vm.gpsWriter.WriteString(nmea)
	vm.gpsWriter.WriteString("\n")
	// TODO: is this the right place for this?
	updateAccessPointsVisible()
	return vm.gpsWriter.Flush()
}

func (vm *AndroidVM) SetWifiSSIDs(ssids ...string) {
	/*
		vm.accessPoints = make(map[string]accessPoint)
		names := []string{}
		for _, s := range ssids {
			var bridge string
			parts := strings.Split(s, ",")
			if len(parts) < 2 || len(parts) > 3 {
				continue
			}
			if len(parts) == 3 {
				bridge = parts[2]
			} else {
				bridge = DEFAULT_BRIDGE
			}
			vlan, err := strconv.Atoi(parts[1])
			if err != nil {
				continue
			}
			vm.accessPoints[parts[0]] = accessPoint{ vlan: vlan, ssid: parts[0], bridge: bridge }
			names = append(names, parts[0])
		}
		vm.wifiModem.UpdateScanResults(names...)
	*/
}

func updateAccessPointsVisible() {
	for _, vm := range vms.FindAndroidVMs() {
		if vm.GetState() != VM_RUNNING {
			continue
		}
		points := []miniwifi.APInfo{}
		for _, ap := range wifiAPs {
			signal := ap.signalStrength(vm.currentLocation)
			if signal > -70 {
				points = append(points, miniwifi.APInfo{SSID: ap.ssid, Power: ap.power})
			} else if ap.mW == 0 {
				// "universal" ap
				points = append(points, miniwifi.APInfo{SSID: ap.ssid, Power: -44})
			}
		}
		vm.wifiModem.UpdateScanResults(points)
	}
}
