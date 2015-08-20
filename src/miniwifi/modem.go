package miniwifi

import (
	"bufio"
	"fmt"
	"io"
	log "minilog"
	"net"
	"os"
)

type ScanResult struct {
	id    int
	bssid string
	freq  int
	level int
	tsf   string
	flags string
	ssid  string
}

type Modem struct {
	controlPath, monitorPath string
	controlConn, monitorConn net.Conn
	cInput, mInput           *bufio.Reader
	cOutput, mOutput         *bufio.Writer

	networks        map[int]map[string]string // map of ids to map of network properties
	selectedNetwork int
	scanResults     []*ScanResult
	netIdChan       chan int
}

func NewModem(controlPath, monitorPath string) (*Modem, error) {
	m := &Modem{
		controlPath:     controlPath,
		monitorPath:     monitorPath,
		networks:        make(map[int]map[string]string),
		selectedNetwork: -1,
	}

	if err := m.connect(); err != nil {
		return nil, err
	}

	// make netIdChan
	m.netIdChan = make(chan int)
	go func() {
		for i := 0; ; i++ {
			m.netIdChan <- i
		}
	}()

	return m, nil
}

func (m *Modem) connect() (err error) {
	// make sure unix socket already exists
	if fi, err := os.Stat(m.controlPath); err != nil || (fi.Mode()&os.ModeSocket) == 0 {
		return fmt.Errorf("%s is not a valid unix socket: %s\n", m.controlPath, err)
	}

	// make sure unix socket already exists
	if fi, err := os.Stat(m.monitorPath); err != nil || (fi.Mode()&os.ModeSocket) == 0 {
		return fmt.Errorf("%s is not a valid unix socket: %s\n", m.monitorPath, err)
	}

	// create socket connection
	m.controlConn, err = net.Dial("unix", m.controlPath)
	if err != nil {
		return fmt.Errorf("error in connecting to socket %s:\n%s\n", m.controlPath, err)
	}

	// create socket connection
	m.monitorConn, err = net.Dial("unix", m.monitorPath)
	if err != nil {
		m.controlConn.Close()
		return fmt.Errorf("error in connecting to socket %s:\n%s\n", m.monitorPath, err)
	}

	// initialize input and output buffers using sockets
	m.cInput = bufio.NewReader(m.controlConn)
	m.mInput = bufio.NewReader(m.monitorConn)
	m.cOutput = bufio.NewWriter(m.controlConn)
	m.mOutput = bufio.NewWriter(m.monitorConn)

	return nil
}

func (m *Modem) Run() {
	// read from control in, match up with a response, respond on control out
	// (and sometimes monitor out)
	for {
		command := m.readCommand()
		if command != "" {
			m.handleCommand(command)
		}
	}
}

func (m *Modem) UpdateScanResults(ssids ...string) {
	// First, clear old scan results
	m.scanResults = nil

	for id, ssid := range ssids {
		id++ // increment by 1 so id starts at 1
		bssid := fmt.Sprintf("00:00:00:%02x:%02x:%02x", (id/65536)%256, (id/256)%256, id%256)
		// defaults, static values
		freq := 2412              // channel 1
		level := -44              // in dBm | 0 is 100% signal. -100 is ~0% signal. -44 should still be 5 bars. -73 should be 2-3 bars.
		tsf := "0000000000000000" // timestamp of last beacon frame - ignore
		flags := "[OPEN]"         // this might get changed later if we want WPA
		m.scanResults = append(m.scanResults, &ScanResult{id, bssid, freq, level, tsf, flags, ssid})
	}

	return
}

func (m *Modem) readCommand() string {
	b, err := m.cInput.ReadBytes('\x00')
	if err != nil {
		if err == io.EOF {
			// TODO: terminate
			/*
				log.Error("readCommand received EOF - exiting\n")
				os.Exit(3)
			*/
		}
		log.Error("readCommand error: %s\n", err)
		return ""
	}
	return string(b)
}
