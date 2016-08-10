// Copyright (2015) Sandia Corporation.
// Under the terms of Contract DE-AC04-94AL85000 with Sandia Corporation,
// the U.S. Government retains certain rights in this software.

package miniwifi

import (
	"errors"
	"fmt"
	"io/ioutil"
	log "minilog"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const PREFIX = "IFNAME=eth0 " // space at end is important

func (m *Modem) handleCommand(command string) {
	// command should always start with prefix, so strip that off
	cmd := strings.TrimPrefix(command, PREFIX)
	var key string

	switch {
	case isOkMessage(cmd):
		m.sayOk()
		log.Debug(command + " - autoresponse: OK\n")
	case isResponseMessage(cmd, &key): // side effect - key gets set
		writeString(m.cOutput, RESPONSE_MESSAGES[key])
		log.Debug(command+" - autoresponse: %s\n", RESPONSE_MESSAGES[key])
	case strings.HasPrefix(cmd, "SCAN"):
		m.sayOk()
		log.Debug(command + " - autoresponse: OK | also writing \"CTRL-EVENT-SCAN-RESULTS\" to monitor socket\n")
		writeString(m.mOutput, "IFNAME=eth0 CTRL-EVENT-SCAN-RESULTS\x00")
	case strings.HasPrefix(cmd, "BSS RANGE=0-"): // SCAN RESULTS
		log.Debug(command)
		sr := m.getScanResults()
		log.Debug(" - autoresponding with scan results\n")
		writeString(m.cOutput, sr)
	case strings.HasPrefix(cmd, "DRIVER WLS_BATCHING GET"): // BATCH SCAN RESULTS
		log.Debug(command)
		sr := m.getBatchedScanResults()
		log.Debug(" - autoresponding with batched scan results\n")
		writeString(m.cOutput, sr)
	case strings.HasPrefix(cmd, "ADD_NETWORK"):
		id := m.addNetwork()
		log.Debug(command+" - autoresponse: %d\n", id)
		writeString(m.cOutput, strconv.Itoa(id))
	case strings.HasPrefix(cmd, "REMOVE_NETWORK"):
		m.removeNetwork(cmd)
		log.Debug(command + " - autoresponse: OK\n")
		m.sayOk()
	case strings.HasPrefix(cmd, "DISABLE_NETWORK"):
		m.disableNetwork(cmd)
		log.Debug(command + " - autoresponse: OK\n")
		m.sayOk()
	case strings.HasPrefix(cmd, "SET_NETWORK"):
		success := m.setNetworkProperty(cmd)
		if success {
			log.Debug(command + " - autoresponse: OK\n")
			m.sayOk()
		} else {
			log.Debug(command + " - could not parse\n")
			m.sayNull()
		}
	case strings.HasPrefix(cmd, "GET_NETWORK"):
		value := m.getNetworkProperty(cmd)
		if value == "" {
			log.Debug(command + " - could not parse, responding NULL\n")
			m.sayNull()
		} else {
			log.Debug(command+" - autoresponse: %s\n", value)
			writeString(m.cOutput, value)
		}
	case strings.HasPrefix(cmd, "ENABLE_NETWORK"):
		m.enableNetwork(cmd)
		log.Debug(command + " - autoresponse: OK\n")
		m.sayOk()
	case strings.HasPrefix(cmd, "SELECT_NETWORK"):
		m.selectNetwork(cmd)
		con, err := m.getConnectedString()
		if err != nil {
			log.Debug(command+" - autoresponse: OK | error with connection: %s", err.Error())
			m.sayOk()
			break
		}
		sc, err := m.getStateChangeString()
		if err != nil {
			log.Debug(command+" - autoresponse: OK | error with state change: %s", err.Error())
			m.sayOk()
			break
		}
		log.Debug(command + " - autoresponse: OK | also writing \"CTRL-EVENT-CONNECTED\" and \"CTRL-EVENT-STATE-CHANGE\" to monitor socket\n")
		writeString(m.mOutput, con, sc)
		m.sayOk()
	case strings.HasPrefix(cmd, "RECONNECT"):
		if m.selectedNetwork < 0 || m.networks[m.selectedNetwork] == nil {
			log.Debug(command + " - autoresponse: NULL - no selected network\n")
			m.sayNull()
		} else {
			con, err := m.getConnectedString()
			if err != nil {
				log.Debug(command+" - autoresponse: OK | error with connection: %s", err.Error())
				m.sayOk()
				break
			}
			sc, err := m.getStateChangeString()
			if err != nil {
				log.Debug(command+" - autoresponse: OK | error with state change: %s", err.Error())
				m.sayOk()
				break
			}
			log.Debug(command + " - autoresponse: OK | also writing \"CTRL-EVENT-CONNECTED\" and \"CTRL-EVENT-STATE-CHANGE\" to monitor socket\n")
			writeString(m.mOutput, con, sc)
			m.sayOk()
		}
	case strings.HasPrefix(cmd, "DISCONNECT"):
		m.selectedNetwork = -1
		log.Debug(command + " - autoresponse: OK | also writing \"CTRL-EVENT-DISCONNECTED\" to monitor socket\n")
		writeString(m.mOutput, "IFNAME=eth0 CTRL-EVENT-DISCONNECTED\x00")
		m.sayOk()
	case strings.HasPrefix(cmd, "SIGNAL_POLL"):
		sr, err := m.getSignal()
		if err != nil {
			log.Debug(command+" - autoresponse: FAIL - error: %s\n", err)
			m.sayFail()
		} else {
			writeString(m.cOutput, sr)
			log.Debug(command + " - autoresponding with signal results\n")
		}
	case strings.HasPrefix(cmd, "LIST_NETWORKS"):
		nr, err := m.getNetworks()
		if err != nil {
			log.Debug(command+" - autoresponse: FAIL - error: %s\n", err)
			m.sayFail()
		} else {
			writeString(m.cOutput, nr)
			log.Debug(command+" - autoresponding with networks: %s\n", nr)
			//log.Debug(command+" - autoresponding with networks\n")
		}
	case strings.HasPrefix(cmd, "STATUS"):
		sr, err := m.getStatus()
		if err != nil {
			log.Debug(command+" - autoresponse: FAIL - error: %s\n", err)
			m.sayFail()
		} else {
			writeString(m.cOutput, sr)
			log.Debug(command+" - autoresponding with status: %s\n", sr)
			//log.Debug(command+" - autoresponding with status\n")
		}
	default:
		log.Debug(command)
		for {
			bytes, _ := ioutil.ReadAll(os.Stdin)
			if bytes[0] == '|' { // signal to send to m.monitorOut
				m.mOutput.Write(bytes[1:])
				m.mOutput.Flush()
			} else {
				m.cOutput.Write(bytes)
				m.cOutput.Flush()
				break
			}
		}
	}
}

func (m *Modem) getScanResults() string {
	// see line 1881 of WifiStateMachine.java for expected format
	var s string
	for id, sr := range m.scanResults {
		s += fmt.Sprintf("id=%d\n", id+1)
		s += fmt.Sprintf("bssid=%s\n", sr.bssid)
		s += fmt.Sprintf("freq=%d\n", sr.freq)
		s += fmt.Sprintf("level=%d\n", sr.level)
		s += fmt.Sprintf("tsf=%s\n", sr.tsf)
		s += fmt.Sprintf("flags=%s\n", sr.flags)
		s += fmt.Sprintf("ssid=%s\n", sr.ssid)
		s += "====\n"
	}
	if s == "" {
		s = "\x00"
	} else {
		s += "####"
	}
	log.Debug("getScanResults = %v\n", s)
	return s
}

func (m *Modem) getBatchedScanResults() string {
	// see line 993 of WifiStateMachine.java for expected format
	var s string
	s += fmt.Sprintf("scancount=%d\n", len(m.scanResults))
	//s += fmt.Sprintf("nextcount=%d\n", len(m.scanResults))
	//s += fmt.Sprintf("apcount=%d\n", len(m.scanResults))
	for _, sr := range m.scanResults {
		s += fmt.Sprintf("bssid=%s\n", sr.bssid)
		s += fmt.Sprintf("freq=%d\n", sr.freq)
		s += fmt.Sprintf("level=%d\n", sr.level)
		s += fmt.Sprintf("ssid=%s\n", sr.ssid)
		// TODO: maybe square-log function can go here to dynamically generate distances based on signal strength
		s += "dist=10\n"  // cm
		s += "distSd=1\n" // cm standard deviation
		s += "====\n"
	}
	s += "----"
	log.Debug("getBatchedScanResults = %v\n", s)
	return s
}

func (m *Modem) addNetwork() int {
	id := <-m.netIdChan
	m.networks[id] = make(map[string]string)
	// default values
	m.networks[id]["proto"] = "WPA RSN"
	m.networks[id]["key_mgmt"] = "WPA-PSK WPA-EAP"
	m.networks[id]["pairwise"] = "CCMP TKIP"
	m.networks[id]["scan_ssid"] = "0"
	m.networks[id]["group"] = "CCMP TKIP WEP104 WEP40"
	m.networks[id]["engine"] = "0"
	log.Debug("adding network id %v\n", id)
	return id
}

func (m *Modem) selectNetwork(cmd string) {
	re := regexp.MustCompile("SELECT_NETWORK ([0-9]+)")
	match := re.FindStringSubmatch(cmd)
	if match == nil {
		return
	}
	id, err := strconv.Atoi(match[1])
	if err != nil {
		return
	}
	m.selectedNetwork = id
	log.Debug("selected network %v\n", id)
	m.NetworkNameChan <- m.networks[id]["ssid"]
}

func (m *Modem) removeNetwork(cmd string) {
	re := regexp.MustCompile("REMOVE_NETWORK ([0-9]+)")
	match := re.FindStringSubmatch(cmd)
	if match == nil {
		return
	}
	id, err := strconv.Atoi(match[1])
	if err != nil {
		return
	}
	if id == m.selectedNetwork {
		m.selectedNetwork = -1
	}
	log.Debug("removeNetwork %v\n", id)
	delete(m.networks, id)
}

func (m *Modem) enableNetwork(cmd string) {
	re := regexp.MustCompile("ENABLE_NETWORK ([0-9]+)")
	match := re.FindStringSubmatch(cmd)
	if match == nil {
		return
	}
	id, err := strconv.Atoi(match[1])
	if err != nil {
		return
	}
	m.networks[id]["enabled"] = "true"
}

func (m *Modem) disableNetwork(cmd string) {
	re := regexp.MustCompile("DISABLE_NETWORK ([0-9]+)")
	match := re.FindStringSubmatch(cmd)
	if match == nil {
		return
	}
	id, err := strconv.Atoi(match[1])
	if err != nil {
		return
	}
	//if id == selectedNetwork {
	//	selectedNetwork = -1
	//}
	delete(m.networks[id], "enabled")
}

func (m *Modem) setNetworkProperty(cmd string) bool { // TODO: need special rule for "ssid" property to go in and default other properties, like "proto" and "key_mgmt"
	re := regexp.MustCompile("SET_NETWORK ([0-9]+) ([[:word:]]+) ([[:print:]]+)")
	fmt.Println(cmd)
	match := re.FindStringSubmatch(cmd)
	if match == nil {
		return false
	}
	id, err := strconv.Atoi(match[1])
	if err != nil {
		return false
	}
	key := match[2]
	value := match[3]
	m.networks[id][key] = value
	return true
}

func (m *Modem) getNetworkProperty(cmd string) string {
	re := regexp.MustCompile("GET_NETWORK ([0-9]+) ([[:word:]]+)")
	match := re.FindStringSubmatch(cmd)
	if match == nil {
		return ""
	}
	id, err := strconv.Atoi(match[1])
	if err != nil {
		return ""
	}
	key := match[2]
	value := m.networks[id][key]
	if value == "" {
		return "\x00"
	}
	return value
}

func (m *Modem) getStatus() (string, error) {
	if m.selectedNetwork < 0 || m.networks[m.selectedNetwork] == nil {
		return "wpa_state=DISCONNECTED\naddress=00:00:00:00:00:00\nuuid=00000000-0000-0000-0000-000000000000\n", nil
		//return "wpa_state=DISCONNECTED\nip_address=10.0.0.2\naddress=00:00:00:00:00:00\nuuid=00000000-0000-0000-0000-000000000000\n"
	}
	/*
		bssid=00:00:00:00:00:01
		freq=2412
		ssid=minimobile
		id=0
		mode=station
		pairwise_cipher=CCMP
		group_cipher=CCMP
		key_mgmt=WPA2-PSK
		wpa_state=COMPLETED
		ip_address=10.0.0.2
		address=00:00:00:00:00:00
		uuid=00000000-0000-0000-0000-000000000000
	*/
	s := ""
	ssid := m.networks[m.selectedNetwork]["ssid"]
	if ssid == "" { // this network does not have an ssid set, can't continue to match against a scan result
		return "", errors.New("No ssid set")
	}

	for _, sr := range m.scanResults {
		if sr.ssid == strings.Trim(ssid, "\"") { // network ssid may be in quotes - remove them for comparison
			s += fmt.Sprintf("bssid=%s\n", sr.bssid)
			s += fmt.Sprintf("freq=%d\n", sr.freq)
			s += fmt.Sprintf("ssid=%s\n", ssid)
			s += fmt.Sprintf("id=%d\n", m.selectedNetwork)
			s += "mode=station\n"
			s += "wpa_state=COMPLETED\n"
			break
		}
	}

	return s, nil
}

func (m *Modem) getSignal() (string, error) {
	if m.selectedNetwork < 0 || m.networks[m.selectedNetwork] == nil {
		return "", errors.New(fmt.Sprintf("No network selected"))
	}

	ssid := m.networks[m.selectedNetwork]["ssid"]
	if ssid == "" { // this network does not have an ssid set, can't continue to match against a scan result
		return "", errors.New("No ssid set")
	}
	level := -43 // default to good quality level
	freq := 2412 // default to channel 1
	for _, sr := range m.scanResults {
		if sr.ssid == strings.Trim(ssid, "\"") { // network ssid may be in quotes - remove them for comparison
			level = sr.level
			freq = sr.freq
			break
		}
	}
	return fmt.Sprintf("RSSI=%d\nLINKSPEED=54\nNOISE=9999\nFREQUENCY=%d", level, freq), nil
}

func (m *Modem) getNetworks() (string, error) {
	s := "network id / ssid / bssid / flags\n"
	for i, network := range m.networks {
		ssid := network["ssid"]
		if ssid == "" {
			continue // this network hasn't been set up yet
		}
		s += fmt.Sprintf("%d\t%s\tany", i, network["ssid"])
		if m.selectedNetwork == i {
			s += "\t[CURRENT]"
		}
		s += "\n"
	}
	return s, nil
}

func (m *Modem) getConnectedString() (string, error) {
	if m.selectedNetwork < 0 || m.networks[m.selectedNetwork] == nil {
		return "", errors.New("No network selected")
	}

	ssid := m.networks[m.selectedNetwork]["ssid"]
	if ssid == "" { // this network does not have an ssid set, can't continue to match against a scan result
		return "", errors.New("No ssid set")
	}
	bssid := "00:00:00:00:00:00" // silly default
	for _, sr := range m.scanResults {
		if sr.ssid == strings.Trim(ssid, "\"") { // network ssid may be in quotes - remove them for comparison
			bssid = sr.bssid
			break
		}
	}
	return fmt.Sprintf("IFNAME=eth0 CTRL-EVENT-CONNECTED Connection to %s completed (reauth) [id=%d id_str=]\x00", bssid, m.selectedNetwork), nil
}

func (m *Modem) getStateChangeString() (string, error) {
	if m.selectedNetwork < 0 || m.networks[m.selectedNetwork] == nil {
		return "", errors.New("No network selected")
	}

	ssid := m.networks[m.selectedNetwork]["ssid"]
	if ssid == "" { // this network does not have an ssid set, can't continue to match against a scan result
		return "", errors.New("No ssid set")
	}
	bssid := "00:00:00:00:00:00" // silly default
	for _, sr := range m.scanResults {
		if sr.ssid == strings.Trim(ssid, "\"") { // network ssid may be in quotes - remove them for comparison
			bssid = sr.bssid
			break
		}
	}

	return fmt.Sprintf("IFNAME=eth0 CTRL-EVENT-STATE-CHANGE SSID=%s BSSID=%s id=%d state=3\x00", ssid, bssid, m.selectedNetwork), nil // state=3 should be "connected"
}

func (m *Modem) sayOk() {
	writeString(m.cOutput, "OK")
}

func (m *Modem) sayNull() {
	writeString(m.cOutput, "NULL")
}

func (m *Modem) sayFail() {
	writeString(m.cOutput, "FAIL")
}
