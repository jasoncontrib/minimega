package bridge

import (
	"errors"
	"fmt"
	log "minilog"
	"strconv"
)

// Used to calulate burst rate for the token bucket filter qdisc
const (
	KERNEL_TIMER_FREQ uint64 = 250
	MIN_BURST_SIZE    uint64 = 2048
	DEFAULT_LATENCY   string = "100ms"
)

// Traffic control actions
const (
	tcAdd    string = "add"
	tcDel    string = "del"
	tcUpdate string = "change"
)

// Qos option types
type QosType int

const (
	Rate QosType = iota
	Loss
	Delay
)

type QosOption struct {
	Type  QosType
	Value string
}

type tbfParams struct {
	rate  string
	burst string
}

type netemParams struct {
	loss  string
	delay string
}

// Tap field enumerating qos parameters
type qos struct {
	*tbfParams   // embed
	*netemParams // embed
}

func newQos() *qos {
	return &qos{netemParams: &netemParams{},
		tbfParams: &tbfParams{}}
}

// Set the initial qdisc namespace
func (t *Tap) initializeQos() error {
	var cmd []string
	var ns []string
	var err error
	t.Qos = newQos()

	cmd = []string{"tc", "qdisc", tcAdd, "dev", t.Name}
	rate := "1000gbit"
	burst := getQosBurst(rate)
	ns = []string{"root", "handle", "1:", "tbf", "rate", rate, "latency", DEFAULT_LATENCY, "burst", burst}
	err = t.qosCmd(append(cmd, ns...))
	if err != nil {
		return err
	}

	cmd = []string{"tc", "qdisc", tcAdd, "dev", t.Name}
	ns = []string{"parent", "1:", "handle", "2:", "netem", "loss", "0"}
	err = t.qosCmd(append(cmd, ns...))
	return err
}

func (t *Tap) destroyQos() error {
	if t.Qos == nil {
		return nil
	}
	t.Qos = nil
	cmd := []string{"tc", "qdisc", tcDel, "dev", t.Name, "root"}
	return t.qosCmd(cmd)
}

func (t *Tap) setQos(op QosOption) error {
	var action string
	var ns []string

	if t.Qos == nil {
		err := t.initializeQos()
		if err != nil {
			return err
		}
		log.Debug("initialized qos")
	}

	switch op.Type {
	case Loss:
		action = tcUpdate
		ns = []string{"parent", "1:12", "handle", "2:", "netem", "loss", op.Value}
		t.Qos.netemParams.loss = op.Value
	case Delay:
		action = tcUpdate
		ns = []string{"parent", "1:", "handle", "2:", "netem", "delay", op.Value}
		t.Qos.netemParams.delay = op.Value
	case Rate:
		action = tcUpdate
		burst := getQosBurst(op.Value)
		ns = []string{"root", "handle", "1:", "tbf", "rate", op.Value,
			"latency", DEFAULT_LATENCY, "burst", burst}
		t.Qos.tbfParams.rate = op.Value
		t.Qos.tbfParams.burst = burst
	}

	cmd := []string{"tc", "qdisc", action, "dev", t.Name}
	return t.qosCmd(append(cmd, ns...))
}

// Execute a qos command string
func (t *Tap) qosCmd(cmd []string) error {
	log.Debug("received qos command %v", cmd)
	out, err := processWrapper(cmd...)
	if err != nil {
		// Clean up
		err = errors.New(out)
		t.destroyQos()
	}
	return err
}

func (b *Bridge) ClearQos(tap string) error {
	bridgeLock.Lock()
	defer bridgeLock.Unlock()

	log.Info("clearing qos for tap %s", tap)

	t, ok := b.taps[tap]
	if !ok {
		return fmt.Errorf("tap %s not found", tap)
	}
	return t.destroyQos()
}

func (b *Bridge) UpdateQos(tap string, op QosOption) error {
	bridgeLock.Lock()
	defer bridgeLock.Unlock()

	log.Info("updating qos for tap %s", tap)

	t, ok := b.taps[tap]
	if !ok {
		return fmt.Errorf("tap %s not found", tap)
	}

	return t.setQos(op)
}

func (b *Bridge) GetQos(tap string) []QosOption {
	bridgeLock.Lock()
	defer bridgeLock.Unlock()

	t, ok := b.taps[tap]
	if !ok {
		return nil
	}
	if t.Qos == nil {
		return nil
	}
	return b.getQos(t)
}

func (b *Bridge) getQos(t *Tap) []QosOption {
	var ops []QosOption

	if t.Qos.tbfParams.rate != "" {
		ops = append(ops, QosOption{Rate, t.Qos.tbfParams.rate})
	}
	if t.Qos.netemParams.loss != "" {
		ops = append(ops, QosOption{Loss, t.Qos.netemParams.loss})
	}
	if t.Qos.netemParams.delay != "" {
		ops = append(ops, QosOption{Delay, t.Qos.netemParams.delay})
	}
	return ops
}

func getQosBurst(rate string) string {
	r := rate[:len(rate)-4]
	unit := rate[len(rate)-4:]
	var bps uint64

	switch unit {
	case "kbit":
		bps = 1 << 10
	case "mbit":
		bps = 1 << 20
	case "gbit":
		bps = 1 << 30
	}
	burst, _ := strconv.ParseUint(r, 10, 64)

	// Burst size is in bytes
	burst = ((burst * bps) / KERNEL_TIMER_FREQ) / 8
	if burst < MIN_BURST_SIZE {
		burst = MIN_BURST_SIZE
	}
	return fmt.Sprintf("%db", burst)
}
