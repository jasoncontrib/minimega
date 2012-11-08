// minimega
// 
// Copyright (2012) Sandia Corporation. 
// Under the terms of Contract DE-AC04-94AL85000 with Sandia Corporation, 
// the U.S. Government retains certain rights in this software.
//
// David Fritz <djfritz@sandia.gov>

// command line interface for minimega
// 
// The command line interface wraps a number of commands listed in the 
// cli_commands map. Each entry to the map defines a function that is called
// when the command is invoked on the command line, as well as short and long
// form help. The record parameter instructs the cli to put the command in the
// command history.
//
// The cli uses the readline library for command history and tab completion.
// A separate command history is kept and used for writing the buffer out to
// disk. 
package main

// TODO: vm_info command to list current info
// TODO: bridge_info or something like it 

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	log "minilog"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

var (
	command_buf []string // command history for the write command

	// incoming commands for the cli to parse. these can come from the cli
	// proper (readline), or from a network source, etc. the cli will parse
	// them all as if they were typed locally.
	command_chan_local  chan cli_command
	command_chan_socket chan cli_command

	ack_chan_local  chan cli_response // acknowledgements from the cli, one per incoming command
	ack_chan_socket chan cli_response

	cli_commands map[string]*command
)

type cli_command struct {
	Command  string
	Args     []string
	ack_chan chan cli_response
}

type cli_response struct {
	Response string
	Error    error
	More     bool // more is set if the called command will be sending multiple responses
}

type command struct {
	Call      func(c cli_command) cli_response // callback function
	Helpshort string                           // short form help test, one line only
	Helplong  string                           // long form help text
	Record    bool                             // record in the command history
	Clear     func() error                     // clear/restore to default state
}

func init() {
	command_chan_local = make(chan cli_command)
	command_chan_socket = make(chan cli_command)
	ack_chan_local = make(chan cli_response)
	ack_chan_socket = make(chan cli_response)

	// list of commands the cli supports. some commands have small callbacks, which
	// are defined inline.
	cli_commands = map[string]*command{
		"rate": &command{
			Call: func(c cli_command) cli_response {
				if len(c.Args) == 0 {
					return cli_response{
						Response: fmt.Sprintf("%v", launch_rate),
					}
				} else if len(c.Args) != 1 {
					return cli_response{
						Error: errors.New("rate takes one argument"),
					}
				} else {
					r, err := strconv.Atoi(c.Args[0])
					if err != nil {
						return cli_response{
							Error: err,
						}
					}
					launch_rate = time.Millisecond * time.Duration(r)
				}
				return cli_response{}
			},
			Helpshort: "set the launch/kill rate in milliseconds",
			Helplong: `
Set the launch and kill rate in milliseconds. Some calls to external tools can
take some time to respond, causing errors if you try to launch or kill VMs too
quickly. The default value is 100 milliseconds.`,
			Record: true,
			Clear: func() error {
				launch_rate = time.Millisecond * 100
				return nil
			},
		},

		"log_level": &command{
			Call:      cli_log_level,
			Helpshort: "set the log level",
			Helplong: `
Usage: log_level <level>
Set the log level to one of [debug, info, warn, error, fatal, off]. Log levels
inherit lower levels, so setting the level to error will also log fatal, and
setting the mode to debug will log everything.`,
			Record: true,
			Clear: func() error {
				*f_loglevel = "error"
				log.SetLevel("stdio", log.ERROR)
				log.SetLevel("file", log.ERROR)
				return nil
			},
		},

		"log_stderr": &command{
			Call:      cli_log_stderr,
			Helpshort: "enable/disable logging to stderr",
			Helplong: `
Enable or disable logging to stderr. Valid options are [true, false].`,
			Record: true,
			Clear: func() error {
				_, err := log.GetLevel("stdio")
				if err == nil {
					log.DelLogger("stdio")
				}
				return nil
			},
		},

		"log_file": &command{
			Call:      cli_log_file,
			Helpshort: "enable logging to a file",
			Helplong: `
Usage log_file <filename>
Log to a file. To disable file logging, call "log_file false".`,
			Record: true,
			Clear: func() error {
				_, err := log.GetLevel("file")
				if err == nil {
					log.DelLogger("file")
				}
				return nil
			},
		},

		"check": &command{
			Call:      external_check,
			Helpshort: "check for the presence of all external executables minimega uses",
			Helplong: `
Minimega maintains a list of external packages that it depends on, such as qemu.
Calling check will attempt to find each of these executables in the avaiable
path, and returns an error on the first one not found.`,
			Record: true,
			Clear: func() error {
				return nil
			},
		},

		"nuke": &command{
			Call:      nuke,
			Helpshort: "attempt to clean up after a crash",
			Helplong: `
After a crash, the VM state on the machine can be difficult to recover from.
Nuke attempts to kill all instances of QEMU, remove all taps and bridges, and
removes the temporary minimega state on the harddisk.`,
			Record: true,
			Clear: func() error {
				return nil
			},
		},

		"write": &command{
			Call: func(c cli_command) cli_response {
				if len(c.Args) != 1 {
					return cli_response{
						Error: errors.New("write takes a single argument"),
					}
				}
				file, err := os.Create(c.Args[0])
				if err != nil {
					return cli_response{
						Error: err,
					}
				}
				for _, i := range command_buf {
					_, err = file.WriteString(i + "\n")
					if err != nil {
						return cli_response{
							Error: err,
						}
					}
				}
				return cli_response{}
			},
			Helpshort: "write the command history to a file",
			Helplong: `
Usage: write <file>
Write the command history to <file>. This is useful for handcrafting configs
on the minimega command line and then saving them for later use. Argss that
failed, as well as some commands that do not impact the VM state, such as
'help', do not get recorded.`,
			Record: false,
			Clear: func() error {
				return nil
			},
		},

		"read": &command{
			Call: func(c cli_command) cli_response {
				if len(c.Args) != 1 {
					return cli_response{
						Error: errors.New("read takes a single argument"),
					}
				}
				file, err := os.Open(c.Args[0])
				if err != nil {
					return cli_response{
						Error: err,
					}
				}
				r := bufio.NewReader(file)
				for {
					l, _, err := r.ReadLine()
					if err != nil {
						if err == io.EOF {
							break
						}
						log.Error("%v", err)
					}
					log.Info("read command: %v", string(l))
					f := strings.Fields(string(l))
					var command string
					var args []string
					if len(f) > 0 {
						command = f[0]
					}
					if len(f) > 1 {
						args = f[1:]
					}
					resp := cli_exec(cli_command{
						Command: command,
						Args:    args,
					})
					resp.More = true
					c.ack_chan <- resp
					if resp.Error != nil {
						log.Errorln(resp.Error)
						break // stop on errors
					}
				}
				return cli_response{}
			},
			Helpshort: "read and execute a command file",
			Helplong: `
Usage: read <file>
Read a command file and execute it. This has the same behavior as if you typed
the file in manually.`,
			Record: true,
			Clear: func() error {
				return nil
			},
		},

		"vm_status": &command{
			Call: func(c cli_command) cli_response {
				return vms.status(c)
			},
			Helpshort: "print the status of each VM",
			Helplong: `
Usage: vm_status <optional VM id>
Print the status for all or one VM, depending on if you supply the optional VM
id field.`,
			Record: true,
			Clear: func() error {
				return nil
			},
		},

		"quit": &command{
			Call: func(c cli_command) cli_response {
				if len(c.Args) != 0 {
					return cli_response{
						Error: errors.New("quit takes no arguments"),
					}
				}
				teardown()
				return cli_response{}
			},
			Helpshort: "quit",
			Helplong:  "Quit",
			Record:    true, // but how!?
			Clear: func() error {
				return nil
			},
		},

		"exit": &command{ // just an alias to quit
			Call: func(c cli_command) cli_response {
				if len(c.Args) != 0 {
					return cli_response{
						Error: errors.New("exit takes no arguments"),
					}
				}
				teardown()
				return cli_response{}
			},
			Helpshort: "exit",
			Helplong:  "Exit",
			Record:    true, // but how!?
			Clear: func() error {
				return nil
			},
		},

		"vm_launch": &command{
			Call: func(c cli_command) cli_response {
				if len(c.Args) != 1 {
					return cli_response{
						Error: errors.New("vm_launch takes one argument"),
					}
				}
				a, err := strconv.Atoi(c.Args[0])
				if err != nil {
					return cli_response{
						Error: err,
					}
				}
				ksm_enable()
				vms.launch(a)
				return cli_response{}
			},
			Helpshort: "launch virtual machines in a paused state",
			Helplong: `
Usage: vm_launch <number of vms>
Launch <number of vms> virtual machines in a paused state, using the parameters
defined leading up to the launch command. Any changes to the VM parameters 
after launching will have no effect on launched VMs.`,
			Record: true,
			Clear: func() error {
				return nil
			},
		},

		"vm_kill": &command{
			Call: func(c cli_command) cli_response {
				if len(c.Args) != 1 {
					return cli_response{
						Error: errors.New("vm_kill takes one argument"),
					}
				}
				a, err := strconv.Atoi(c.Args[0])
				if err != nil {
					return cli_response{
						Error: err,
					}
				}
				vms.kill(a)
				return cli_response{}
			},
			Helpshort: "kill running virtual machines",
			Helplong: `
Usage: vm_kill <vm id>
Kill a virtual machine by ID. Pass -1 to kill all virtual machines.`,
			Record: true,
			Clear: func() error {
				return nil
			},
		},

		"vm_start": &command{
			Call: func(c cli_command) cli_response {
				return vms.start(c)
			},
			Helpshort: "start paused virtual machines",
			Helplong: `
Usage: vm_start <optional VM id>
Start all or one paused virtual machine. To start all paused virtual machines,
call start without the optional VM id.`,
			Record: true,
			Clear: func() error {
				return nil
			},
		},

		"vm_qemu": &command{
			Call: func(c cli_command) cli_response {
				if len(c.Args) == 0 {
					return cli_response{
						Response: process("qemu"),
					}
				} else if len(c.Args) == 1 {
					external_processes["qemu"] = c.Args[0]
				} else {
					return cli_response{
						Error: errors.New("vm_qemu takes only one argument"),
					}
				}
				return cli_response{}
			},
			Helpshort: "set the qemu process to invoke",
			Helplong:  "Set the qemu process to invoke. Relative paths are ok.",
			Record:    true,
			Clear: func() error {
				external_processes["qemu"] = "qemu-system-x86_64"
				return nil
			},
		},

		"vm_memory": &command{
			Call: func(c cli_command) cli_response {
				if len(c.Args) == 0 {
					return cli_response{
						Response: info.Memory,
					}
				} else if len(c.Args) == 1 {
					info.Memory = c.Args[0]
				} else {
					return cli_response{
						Error: errors.New("vm_memory takes only one argument"),
					}
				}
				return cli_response{}
			},
			Helpshort: "set the amount of physical memory for a VM",
			Helplong:  "Set the amount of physical memory to allocate in megabytes.",
			Record:    true,
			Clear: func() error {
				info.Memory = "512"
				return nil
			},
		},

		"vm_vcpus": &command{
			Call: func(c cli_command) cli_response {
				if len(c.Args) == 0 {
					return cli_response{
						Response: info.Vcpus,
					}
				} else if len(c.Args) == 1 {
					info.Vcpus = c.Args[0]
				} else {
					return cli_response{
						Error: errors.New("vm_vcpus takes only one argument"),
					}
				}
				return cli_response{}
			},
			Helpshort: "set the number of virtual CPUs for a VM",
			Helplong:  "Set the number of virtual CPUs to allocate a VM.",
			Record:    true,
			Clear: func() error {
				info.Vcpus = "1"
				return nil
			},
		},

		"vm_disk": &command{
			Call: func(c cli_command) cli_response {
				if len(c.Args) == 0 {
					return cli_response{
						Response: info.Disk_path,
					}
				} else if len(c.Args) == 1 {
					info.Disk_path = c.Args[0]
				} else {
					return cli_response{
						Error: errors.New("vm_disk takes only one argument"),
					}
				}
				return cli_response{}
			},
			Helpshort: "set a disk image to attach to a VM",
			Helplong: `
Attach a disk to a VM. Any disk image supported by QEMU is a valid parameter.
Disk images launched in snapshot mode may safely be used for multiple VMs.`,
			Record: true,
			Clear: func() error {
				info.Disk_path = ""
				return nil
			},
		},

		"vm_cdrom": &command{
			Call: func(c cli_command) cli_response {
				if len(c.Args) == 0 {
					return cli_response{
						Response: info.Cdrom_path,
					}
				} else if len(c.Args) == 1 {
					info.Cdrom_path = c.Args[0]
				} else {
					return cli_response{
						Error: errors.New("vm_cdrom takes only one argument"),
					}
				}
				return cli_response{}
			},
			Helpshort: "set a cdrom image to attach to a VM",
			Helplong: `
Attach a cdrom to a VM. When using a cdrom, it will automatically be set
to be the boot device.`,
			Record: true,
			Clear: func() error {
				info.Cdrom_path = ""
				return nil
			},
		},

		"vm_kernel": &command{
			Call: func(c cli_command) cli_response {
				if len(c.Args) == 0 {
					return cli_response{
						Response: info.Kernel_path,
					}
				} else if len(c.Args) == 1 {
					info.Kernel_path = c.Args[0]
				} else {
					return cli_response{
						Error: errors.New("vm_kernel takes only one argument"),
					}
				}
				return cli_response{}
			},
			Helpshort: "set a kernel image to attach to a VM",
			Helplong: `
Attach a kernel image to a VM. If set, QEMU will boot from this image instead
of any disk image.`,
			Record: true,
			Clear: func() error {
				info.Kernel_path = ""
				return nil
			},
		},

		"vm_initrd": &command{
			Call: func(c cli_command) cli_response {
				if len(c.Args) == 0 {
					return cli_response{
						Response: info.Initrd_path,
					}
				} else if len(c.Args) == 1 {
					info.Initrd_path = c.Args[0]
				} else {
					return cli_response{
						Error: errors.New("vm_initrd takes only one argument"),
					}
				}
				return cli_response{}
			},
			Helpshort: "set a initrd image to attach to a VM",
			Helplong: `
Attach an initrd image to a VM. Passed along with the kernel image at boot time.`,
			Record: true,
			Clear: func() error {
				info.Initrd_path = ""
				return nil
			},
		},

		"vm_append": &command{
			Call: func(c cli_command) cli_response {
				if len(c.Args) == 0 {
					return cli_response{
						Response: info.Append,
					}
				} else {
					info.Append = strings.Join(c.Args, " ")
				}
				return cli_response{}
			},
			Helpshort: "set an append string to pass to a kernel set with vm_kernel",
			Helplong: `
Add an append string to a kernel set with vm_kernel. Setting vm_append without
using vm_kernel will result in an error.

For example, to set a static IP for a linux VM:

vm_append "ip=10.0.0.5 gateway=10.0.0.1 netmask=255.255.255.0 dns=10.10.10.10"
`,
			Record: true,
			Clear: func() error {
				info.Append = ""
				return nil
			},
		},

		"vm_net": &command{
			Call: func(c cli_command) cli_response {
				r := cli_response{}
				if len(c.Args) == 0 {
					return cli_response{
						Response: strings.Join(info.Networks, "\n"),
					}
				} else {
					for _, lan := range c.Args {
						err, ok := current_bridge.Lan_create(lan)
						if !ok {
							return cli_response{
								Error: err,
							}
						}
						if err == nil {
							r.Response = fmt.Sprintln("creating new lan:", lan)
						}
					}
					info.Networks = c.Args
				}
				return r
			},
			Helpshort: "specify the networks the VM is a member of",
			Helplong: `
Usage: vm_net <name> <optional addtional names>
Specify the network(s) that the VM is a member of by name. A corresponding VLAN
will be created for each named network. Like named networks will be joined by
all connected hosts. You may specifiy multiple named networks for a VM. For
example, if you wanted a VM to be on network "monitor" and "test":

vm_net monitor test

Calling vm_net with no parameters will list the current networks for this VM.`,
			Record: true,
			Clear: func() error {
				info.Networks = []string{}
				return nil
			},
		},

		"vnc": &command{
			Call:      cli_vnc,
			Helpshort: "invoke a vnc viewer on a VM or start a vnc pool server",
			Helplong: `
Usage: vnc [VM id, serve <host:port>, novnc <novnc path>]
Launch a vnc viewer for a specific VM or launch a webserver that allows you to
browse the connected minimega hosts and VMs, and connect to any VM in the pool.

This command requires access to an installation of novnc. By default minimega
looks in 'pwd'/novnc. To set a different path, invoke:

vnc novnc <path to novnc>

To start the vnc webserver without launching a vnc session, issue the vnc serve
command with a host and port. For example, if you wanted to serve on localhost,
port 8080, invoke:

vnc serve :8080

:8080 is the default port.

To connect to a particular vm, invoke vnc with the VM id. Note, this will 
invoke the vnc server if it isn't already started, on the default port of 8080.`,
			Record: true,
			Clear: func() error {
				vnc_novnc = "misc/novnc"
				return nil
			},
		},

		"history": &command{
			Call: func(c cli_command) cli_response {
				r := cli_response{}
				if len(c.Args) != 0 {
					r.Error = errors.New("history takes no arguments")
				} else {
					r.Response = strings.Join(command_buf, "\n")

				}
				return r
			},
			Helpshort: "shows the command history",
			Helplong: `
shows the command history`,
			Record: false,
			Clear: func() error {
				return nil
			},
		},

		"clear": &command{
			Call: func (c cli_command) cli_response {
				var r cli_response
				if len(c.Args) != 1 {
					return cli_response{
						Error: errors.New("clear takes one argument"),
					}
				}
				cc := c.Args[0]
				if cli_commands[cc] == nil {
					e := fmt.Sprintf("invalid command: %v", cc)
					r.Error = errors.New(e)
				} else {
					r.Error = cli_commands[cc].Clear()
				}
				return r
			},
			Helpshort: "restore a variable to its default state",
			Helplong: `
Restores a variable to its default state or clears it. For example, 'clear net'
will clear the list of associated networks.`,
			Record: true,
			Clear: func() error {
				return fmt.Errorf("it's unclear how to clear clear")
			},
		},

		"help": &command{
			Call: func (c cli_command) cli_response {
				r := cli_response{}
				if len(c.Args) == 0 { // display help on help, and list the short helps
					r.Response = "Display help on a command. Here is a list of commands:\n"
					var sorted_names []string
					for c, _ := range cli_commands {
						sorted_names = append(sorted_names, c)
					}
					sort.Strings(sorted_names)
					w := new(tabwriter.Writer)
					buf := bytes.NewBufferString(r.Response)
					w.Init(buf, 0, 8, 0, '\t', 0)
					for _, c := range sorted_names {
						fmt.Fprintln(w, c, "\t", ":\t", cli_commands[c].Helpshort, "\t")
					}
					w.Flush()
					r.Response = buf.String()
				} else if len(c.Args) == 1 { // try to display help on args[0]
					if cli_commands[c.Args[0]] != nil {
						r.Response = fmt.Sprintln(c.Args[0], ":", cli_commands[c.Args[0]].Helpshort)
						r.Response += fmt.Sprintln(cli_commands[c.Args[0]].Helplong)
					} else {
						e := fmt.Sprintf("no help on command: %v", c.Args[0])
						r.Error = errors.New(e)
					}
				} else {
					r.Error = errors.New("help takes one argument")
				}
				return r
			},
			Helpshort: "show this help message",
			Helplong: ``,
			Record: false,
			Clear: func() error {
				return nil
			},
		},

		"host_tap": &command{
			Call: host_tap_create,
			Helpshort: "create a host tap for communicating between hosts and VMs",
			Helplong: `
Create host tap on a named vlan for communicating between a host and any VMs on
that vlan. host_tap takes one argument, the named vlan to tap, and returns
successful if a tap was created. The tap will be named 'host_tap_<vlan>', where
<vlan> is the named vlan specified.`,
			Record: true,
			Clear: func() error {
				return nil //perhaps calling this should remove all host taps
			},
		},
	}
}

// local command line interface, wrapping readline
func cli() {
	for {
		prompt := "minimega$ "
		line, err := rlwrap(prompt)
		if err != nil {
			break // EOF
		}
		log.Debug("got from stdin:", line)
		f := strings.Fields(string(line))
		var command string
		var args []string
		if len(f) > 0 {
			command = f[0]
		}
		if len(f) > 1 {
			args = f[1:]
		}
		c := cli_command{
			Command: command,
			Args:    args,
		}
		command_chan_local <- c
		for {
			r := <-ack_chan_local
			if r.Error != nil {
				log.Errorln(r.Error)
			}
			if r.Response != "" {
				if strings.HasSuffix(r.Response, "\n") {
					fmt.Print(r.Response)
				} else {
					fmt.Println(r.Response)
				}
			}
			if !r.More {
				log.Debugln("got last message")
				break
			} else {
				log.Debugln("expecting more data")
			}
		}
	}
}

func cli_mux() {
	for {
		select {
		case c := <-command_chan_local:
			c.ack_chan = ack_chan_local
			ack_chan_local <- cli_exec(c)
		case c := <-command_chan_socket:
			c.ack_chan = ack_chan_socket
			ack_chan_socket <- cli_exec(c)
		}
	}
}

// process commands from the command channel. each command is acknowledged with
// true/false success codes on command_ack.
func cli_exec(c cli_command) cli_response {
	if c.Command == "" {
		return cli_response{}
	}

	// special case, comments. Any line starting with # is a comment and WILL be 
	// recorded.
	if strings.HasPrefix(c.Command, "#") {
		log.Debugln("comment:", c.Command, c.Args)
		s := c.Command
		if len(c.Args) > 0 {
			s += " " + strings.Join(c.Args, " ")
		}
		command_buf = append(command_buf, s)
		return cli_response{}
	}

	if cli_commands[c.Command] == nil {
		e := fmt.Sprintf("invalid command: %v", c.Command)
		return cli_response{
			Error: errors.New(e),
		}
	}
	r := cli_commands[c.Command].Call(c)
	if r.Error == nil {
		if cli_commands[c.Command].Record {
			s := c.Command
			if len(c.Args) > 0 {
				s += " " + strings.Join(c.Args, " ")
			}
			command_buf = append(command_buf, s)
		}
	}
	return r
}
