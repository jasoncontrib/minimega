// Copyright (2016) Sandia Corporation.
// Under the terms of Contract DE-AC04-94AL85000 with Sandia Corporation,
// the U.S. Government retains certain rights in this software.

package bridge

import (
	"fmt"
	log "minilog"
)

// TunnelType is used to specify the type of tunnel for `AddTunnel`.
type TunnelType int

const (
	TunnelVXLAN TunnelType = 1
	TunnelGRE              = 2
)

func (t TunnelType) String() string {
	switch t {
	case TunnelVXLAN:
		return "vxlan"
	case TunnelGRE:
		return "gre"
	}

	return "invalid"
}

// AddTunnel adds a new vxlan or GRE tunnel to a bridge.
func (b *Bridge) AddTunnel(typ TunnelType, remoteIP string) error {
	bridgeLock.Lock()
	defer bridgeLock.Unlock()

	log.Info("adding tunnel on bridge %v: %v %v", b.Name, typ, remoteIP)

	tap := <-b.nameChan

	args := []string{
		"add-port",
		b.Name,
		tap,
		"--",
		"set",
		"interface",
		tap,
		fmt.Sprintf("type=%v", typ),
		fmt.Sprintf("options:remote_ip=%v", remoteIP),
	}
	if _, err := ovsCmdWrapper(args); err != nil {
		return fmt.Errorf("add tunnel failed: %v", err)
	}

	b.tunnels[tap] = true

	return nil
}

// RemoveTunnel removes a tunnel from the bridge.
func (b *Bridge) RemoveTunnel(iface string) error {
	bridgeLock.Lock()
	defer bridgeLock.Unlock()

	return b.removeTunnel(iface)
}

func (b *Bridge) removeTunnel(iface string) error {
	log.Info("removing tunnel on bridge %v: %v", b.Name, iface)

	if !b.tunnels[iface] {
		return fmt.Errorf("unknown tunnel: %v", iface)
	}

	err := ovsDelPort(b.Name, iface)
	if err == nil {
		delete(b.tunnels, iface)
	}

	// TODO: Need to destroy the interface?

	return err
}
