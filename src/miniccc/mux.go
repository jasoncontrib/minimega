// Copyright (2015) Sandia Corporation.
// Under the terms of Contract DE-AC04-94AL85000 with Sandia Corporation,
// the U.S. Government retains certain rights in this software.

package main

import (
	"fmt"
	"io"
	log "minilog"
	"minitunnel"
	"net"
	"ron"
	"sort"
)

// mux routes incoming messages from the server based on message type
func mux() {
	// start piping data to minitunnel and trunking it over the ron
	local, remote := net.Pipe()
	defer local.Close()
	defer remote.Close()

	go func() {
		if err := minitunnel.ListenAndServe(local); err != nil {
			log.Error("ListenAndServe: %v", err)
		}
	}()

	go ron.Trunk(remote, client.UUID, sendMessage)

	// Read messages from gob, mux message to the correct place
	var err error

	log.Debug("starting mux")

	for err == nil {
		var m ron.Message
		if err = client.dec.Decode(&m); err == io.EOF {
			// server disconnected... try to reconnect
			err = dial()
			continue
		} else if err != nil {
			break
		}

		log.Debug("new message: %v", m.Type)

		switch m.Type {
		case ron.MESSAGE_CLIENT:
			// ACK of the handshake
			setNamespace(m.Client.Namespace)
			log.Info("handshake complete, got namespace %v", m.Client.Namespace)
			go periodic()
			go commandHandler()
		case ron.MESSAGE_COMMAND:
			client.commandChan <- m.Commands
		case ron.MESSAGE_FILE:
			client.fileChan <- &m
		case ron.MESSAGE_TUNNEL:
			_, err = remote.Write(m.Tunnel)
		default:
			err = fmt.Errorf("unknown message type: %v", m.Type)
		}
	}

	log.Info("mux exit: %v", err)
}

// setNamespace sets the global namespace from the message
func setNamespace(namespace string) {
	client.Lock()
	defer client.Unlock()

	client.Namespace = namespace
}

func commandHandler() {
	for commands := range client.commandChan {
		var ids []int
		for k, _ := range commands {
			ids = append(ids, k)
		}
		sort.Ints(ids)

		for _, id := range ids {
			processCommand(commands[id])
		}
	}

	log.Info("command handler exit")
}
