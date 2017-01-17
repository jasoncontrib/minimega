package main

import (
	"math/rand"
	"minicli"
	log "minilog"
	"net"
	"os"
	"os/exec"
	"strconv"
	"text/template"
	"time"
)

const (
	BIRD_CONFIG  = "/etc/bird.conf"
	BIRD6_CONFIG = "/etc/bird6.conf"
)

type Bird struct {
	Static   map[string]string
	Static6  map[string]string
	OSPF     map[string]*OSPF
	RouterID string
}

var (
	birdData *Bird
	birdCmd  *exec.Cmd
	bird6Cmd *exec.Cmd
	birdID   string
)

type OSPF struct {
	Area       string
	Interfaces map[string]bool // bool placeholder for later options
}

func init() {
	minicli.Register(&minicli.Handler{
		Patterns: []string{
			"bird <flush,>",
			"bird <commit,>",
			"bird <static,> <network> <nh>",
			"bird <ospf,> <area> <network>",
		},
		Call: handleBird,
	})
	birdID = getRouterID()
	birdData = &Bird{
		Static:   make(map[string]string),
		Static6:  make(map[string]string),
		OSPF:     make(map[string]*OSPF),
		RouterID: birdID,
	}

}

func handleBird(c *minicli.Command, r chan<- minicli.Responses) {
	defer func() {
		r <- nil
	}()

	if c.BoolArgs["flush"] {
		birdData = &Bird{
			Static:   make(map[string]string),
			Static6:  make(map[string]string),
			OSPF:     make(map[string]*OSPF),
			RouterID: birdID,
		}
	} else if c.BoolArgs["commit"] {
		birdConfig()
		birdRestart()
	} else if c.BoolArgs["static"] {
		network := c.StringArgs["network"]
		nh := c.StringArgs["nh"]
		if isIPv4(nh) {
			birdData.Static[network] = nh
		} else if isIPv6(nh) {
			birdData.Static6[network] = nh
		}
	} else if c.BoolArgs["ospf"] {
		area := c.StringArgs["area"]
		network := c.StringArgs["network"]

		// get an interface from the index
		idx, err := strconv.Atoi(network)
		if err != nil {
			log.Errorln(err)
			return
		}

		iface, err := findEth(idx)
		if err != nil {
			log.Errorln(err)
			return
		}

		o := OSPFFindOrCreate(area)
		o.Interfaces[iface] = true
	}
}

func birdConfig() {
	t, err := template.New("bird").Parse(birdTmpl)
	if err != nil {
		log.Errorln(err)
		return
	}

	// First, IPv4
	f, err := os.Create(BIRD_CONFIG)
	if err != nil {
		log.Errorln(err)
		return
	}

	err = t.Execute(f, birdData)
	if err != nil {
		log.Errorln(err)
		return
	}

	// Now, IPv6
	f, err = os.Create(BIRD6_CONFIG)
	if err != nil {
		log.Errorln(err)
		return
	}

	// Weird hack: copy birdData and move Static6
	// into Static so we can use the same template
	bd2 := &Bird{Static: birdData.Static6, OSPF: birdData.OSPF, RouterID: birdData.RouterID}
	err = t.Execute(f, bd2)
	if err != nil {
		log.Errorln(err)
		return
	}
}

func birdRestart() {
	if birdCmd != nil {
		err := birdCmd.Process.Kill()
		if err != nil {
			log.Errorln(err)
			return
		}
		_, err = birdCmd.Process.Wait()
		if err != nil {
			log.Errorln(err)
			return
		}
	}

	birdCmd = exec.Command("bird", "-f", "-s", "/bird.sock", "-P", "/bird.pid", "-c", BIRD_CONFIG)
	err := birdCmd.Start()
	if err != nil {
		log.Errorln(err)
		birdCmd = nil
	}

	if bird6Cmd != nil {
		err := bird6Cmd.Process.Kill()
		if err != nil {
			log.Errorln(err)
			return
		}
		_, err = bird6Cmd.Process.Wait()
		if err != nil {
			log.Errorln(err)
			return
		}
	}

	bird6Cmd = exec.Command("bird6", "-f", "-s", "/bird6.sock", "-P", "/bird6.pid", "-c", BIRD6_CONFIG)
	err = bird6Cmd.Start()
	if err != nil {
		log.Errorln(err)
		bird6Cmd = nil
	}
}

func OSPFFindOrCreate(area string) *OSPF {
	if o, ok := birdData.OSPF[area]; ok {
		return o
	}
	o := &OSPF{
		Area:       area,
		Interfaces: make(map[string]bool),
	}
	birdData.OSPF[area] = o
	return o
}

func getRouterID() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	p := make([]byte, 4)
	_, err := r.Read(p)
	if err != nil {
		log.Fatalln(err)
	}
	ip := net.IPv4(p[0], p[1], p[2], p[3])
	return ip.String()
}

var birdTmpl = `
# minirouter bird template

router id {{ .RouterID }};

protocol kernel {
        scan time 60;
        import none;
        export all;   # Actually insert routes into the kernel routing table
}

# The Device protocol is not a real routing protocol. It doesn't generate any
# routes and it only serves as a module for getting information about network
# interfaces from the kernel.
protocol device {
        scan time 60;
}

{{ $DOSTATIC := len .Static }}
{{ if ne $DOSTATIC 0 }}
protocol static {
	check link;
{{ range $network, $nh := .Static }}
	route {{ $network }} via {{ $nh }};
{{ end }}
}
{{ end }}

{{ $DOOSPF := len .OSPF }}
{{ if ne $DOOSPF 0 }}
protocol ospf {
{{ range $v := .OSPF }} 
	area {{ $v.Area }} {
		{{ range $int, $options := $v.Interfaces }}
		interface "{{ $int }}";
		{{ end }}
	};
{{ end }}
}
{{ end }}
`
