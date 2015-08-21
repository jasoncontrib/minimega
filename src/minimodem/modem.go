package minimodem

import (
	"bufio"
	"fmt"
	log "minilog"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Status struct {
	regstatus   int  // first parameter, <mode>, of COPS
	regformat   int  // format of <oper> field in COPS
	pin         int  // PIN
	creg, cgreg int  // enable unsolicited registration messages
	cmgf        int  // 0 for pdu mode sms, 1 for text mode sms
	mr          byte // message reference, a cyclic counter
}

type Modem struct {
	Number int // the phone number associated with this modem

	socketPath string        // path to socket for communicating with vm
	conn       net.Conn      // connection to modem socket
	input      *bufio.Reader // modem input socket, derived from socketLoc
	output     *bufio.Writer // modem output socket, derived from socketLoc
	status     Status        // status struct for this modem

	Inbox  []Message
	Outbox []Message

	MessageChan chan Message // messages to be delivered

	done chan bool
}

const (
	querypattern  = ".*\\+.+\\?"   // pattern for commands that query the modem
	assignpattern = ".*\\+.+\\=.*" // pattern for commands that assign a value to the modem
	msgOK         = "\r\nOK\r\n"
	expectedpin   = 1234
)

var crsmTab = map[string]string{
	//176 READ BINARY
	"176,12258,0,0,10": "+CRSM: 144,0,98101430121181157002",
	"176,28433,0,0,1":  "+CRSM: 144,0,55",
	"176,28435,0,0,1":  "+CRSM: 144,0,55",
	"176,28472,0,0,15": "+CRSM: 144,0,ff30ffff3c003c03000c0000f03f00",
	"176,28438,0,0,2":  "+CRSM: 144,0,0233",
	"176,28436,0,0,20": "+CRSM: 144,0,416e64726f6964ffffffffffffffffffffffffff",

	//178 READ RECORD
	"178,28615,1,4,32": "+CRSM: 144,0,566f6963656d61696cffffffffffffffffff07915155125740f9ffffffffffff",
	"178,28480,1,4,32": "+CRSM: 144,0,ffffffffffffffffffffffffffffffffffff07815155255155f4ffffffffffff",
	"178,28617,1,4,4":  "+CRSM: 144,0,01000000",
	"176,28589,0,0,4":  "+CRSM: 144,0,00000003",
	"178,28618,1,4,5":  "+CRSM: 144,0,0000000000",
	"178,28613,1,4,24": "+CRSM: 144,0,43058441aa890affffffffffffffffffffffffffffffffff",

	//192 GET RESPONSE
	"192,28589,0,0,15": "+CRSM: 144,0,000000046fad04000aa0aa01020000",
	"192,28618,0,0,15": "+CRSM: 144,0,0000000a6fca040011a0aa01020105",
	"192,28433,0,0,15": "+CRSM: 144,0,000000016f11040011a0aa01020000",
	//"192,28619,0,0,15": "ERROR: BAD COMMAND",
	"192,28435,0,0,15": "+CRSM: 144,0,000000016f13040011a0aa01020000",
	"192,28486,0,0,15": "+CRSM: 148,4",
	"192,28621,0,0,15": "+CRSM: 148,4",
	"192,28613,0,0,15": "+CRSM: 144,0,000000f06fc504000aa0aa01020118",
	"192,28472,0,0,15": "+CRSM: 144,0,0000000f6f3804001aa0aa01020000",
	"192,28438,0,0,15": "+CRSM: 144,0,000000026f1604001aa0aa01020000",
	//"192,28437,0,0,15": "ERROR: BAD COMMAND",
	"192,28436,0,0,15": "+CRSM: 144,0,000000146f1404001aa0aa01020000",
	"192,28615,0,0,15": "+CRSM: 144,0,000000406fc7040011a0aa01020120",

	//214 UPDATE BINARY
	//220 UPDATE RECORD
	//242 STATUS
}

func (m *Modem) cregmsg() {
	// TODO: Need to stop on shutdown
	var creg int

	for {
		if creg != m.status.creg && m.status.creg == 2 {
			m.output.WriteString("\r\n+CREG: 1,\"036D\",\"58B2\"\r\n" + msgOK)
			//log.Debug("sent unsolicited CREG message")
			creg = m.status.creg
		}
		time.Sleep(time.Second * 10)
	}
}

func (m *Modem) cgregmsg() {
	// TODO: Need to stop on shutdown
	var cgreg int

	for {
		if cgreg != m.status.cgreg && m.status.cgreg == 1 {
			m.output.WriteString("\r\n+CGREG: 1\r\n" + msgOK)
			//log.Debug("sent unsolicited CGREG message")
			cgreg = m.status.cgreg
		}
		time.Sleep(time.Second * 10)
	}
}

// PushSMS simulates taking a PDU from the tower and sending it to the phone
// for delivery to the user's SMS inbox.
func (m *Modem) PushSMS(msg Message) error {
	pdu, err := pduSmsPack(msg)
	if err != nil {
		return err
	}

	m.Inbox = append(m.Inbox, msg)
	// debug
	msg, _ = pduSmsUnpack(pdu)

	m.output.WriteString("\r\n+CMT: 0\r\n" + pdu + "\r\n")
	return m.output.Flush()
}

func (m *Modem) parseQuery(s string) (ret string, waserror bool) {
	// Default response
	ret = ""
	waserror = false

	// Chop off the "AT+", if it is there, or remove an initial "+"
	s = strings.Replace(s, "AT+", "", 1)
	if s[0] == '+' {
		s = s[1:]
	}

	switch s {
	case "CFUN?":
		ret = "\r\n+CFUN: 1\r\n"
	case "CPIN?":
		ret = "\r\n+CPIN: READY\r\n"
	case "CGREG?":
		ret = "\r\n+CGREG: " + strconv.Itoa(m.status.cgreg) + ",1\r\n"
	case "CREG?":
		ret = "\r\n+CREG: " + strconv.Itoa(m.status.creg) + ",1,\"036D\",\"58B2\"\r\n"
	case "COPS?":
		ret = fmt.Sprintf("\r\n+COPS: %d,%d,", m.status.regstatus, m.status.regformat)
		switch m.status.regformat {
		case 0:
			ret = ret + "\"Minimobile Network\""
		case 1:
			ret = ret + "\"MINIMOBILE\""
		case 2:
			ret = ret + "\"00101\""
		}
		ret = ret + "\r\n"
		//log.Debug("> %s", ret)
	}
	return
}

func (m *Modem) parseAssignment(s string) (ret string, waserror bool) {
	// Default response
	ret = ""
	waserror = false

	// Chop off the "AT+", if it is there, or remove an initial "+"
	s = strings.Replace(s, "AT+", "", 1)
	if s[0] == '+' {
		s = s[1:]
	}

	subs := strings.Split(s, "=")
	args := strings.Split(subs[1], ",")

	switch subs[0] {
	case "CGREG":
		//log.Debug("setting CGREG to %s\n", args[0])
		m.status.cgreg, _ = strconv.Atoi(args[0])
		ret = "\r\n+CGREG: " + args[0] + "\r\n"
	case "CREG":
		//log.Debug("setting CREG to %s\n", args[0])
		m.status.creg, _ = strconv.Atoi(args[0])
	case "CPIN":
		//log.Debug(s)
		m.status.pin, _ = strconv.Atoi(args[0])
		//log.Debug("storing %d as the new pin, expected pin = %d\n", m.status.pin, expectedpin)
	case "CMGF":
		//log.Debug("setting CMGF to %s\n", args[0])
		m.status.cmgf, _ = strconv.Atoi(args[0])
	case "COPS":
		if mode, _ := strconv.Atoi(args[0]); mode == 3 {
			m.status.regformat, _ = strconv.Atoi(args[1])
		} else if mode == 0 || mode == 1 {
			m.status.regstatus = mode
		}
	case "CRSM":
		//ret = "\r\n+CRSM: 0,0\r\n"
		if crsmTab[subs[1]] == "" {
			ret = "\r\n+CME ERROR: BAD COMMAND\r\n"
			waserror = true
		} else {
			ret = "\r\n" + crsmTab[subs[1]] + "\r\n"
		}
	case "CSMS":
		ret = "\r\n+CSMS: 1,1,1\r\n"
	case "CMGS": // sending sms message
		if m.status.cmgf == 1 { // if set to text mode
			// TODO decode text sms
		} else { // pdu mode, decode the pdu
			m.output.WriteString("\r\n> ")
			m.output.Flush()
			input, _ := m.input.ReadString('') // ctrl-z
			//log.Debug("read message %v", input)
			input = input[:len(input)-1]
			msg, err := pduSmsUnpack(input)
			if err != nil {
				log.Warn("sms pdu did not unpack successfully: %v", err)
			}
			msg.Src = m.Number

			m.MessageChan <- msg
			m.Outbox = append(m.Outbox, msg)

			log.Debug("New SMS from %s: %s\n", msg.Src, msg.Message)
			ret = "\r\n+CMGS: " + strconv.Itoa(int(m.status.mr)) + "\r\n"
			m.status.mr++
		}
	}

	return
}

func (*Modem) otherMessage(s string) (ret string, waserror bool) {
	ret = ""
	waserror = false

	// Chop off the "AT+", if it is there, or remove an initial "+"
	s = strings.Replace(s, "AT+", "", 1)
	if s[0] == '+' {
		s = s[1:]
	}

	switch s {
	case "CGSN":
		// Get the IMEI
		ret = "\r\n135790248939\r\n"
	case "CIMI":
		// request the IMSI
		// give back the MCC + MNC + IMSI
		ret = "\r\n001011234567890\r\n"
	case "CSQ":
		// request signal strength
		//ret = "\r\n+CSQ: 20,0\r\n"
		ret = "\r\n+CSQ: 31.99\r\n"
	}
	return
}

func NewModem(number int, path string, mChan chan Message) (*Modem, error) {
	log.Debug("creating minimodem: %v %v", number, path)

	modem := &Modem{ // return value
		Number:      number,
		socketPath:  path,
		done:        make(chan bool),
		MessageChan: mChan,
	}

	// check if unix socket is already created
	fi, err := os.Stat(path)
	if err != nil || (fi.Mode()&os.ModeSocket) == 0 {
		return nil, fmt.Errorf("%s is not a valid unix socket: %s", path, err)
	}

	// create socket connection between device and modem
	modem.conn, err = net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("error in connecting to socket %s: %s", path, err)
	}

	// initialize input and output buffers using socket
	modem.input = bufio.NewReader(modem.conn)
	modem.output = bufio.NewWriter(modem.conn)

	// set initial modem format and status
	modem.status.regformat = 2
	modem.status.regstatus = 0

	return modem, nil
}

func (m *Modem) Run() error {
	// launch goroutines for periodic unsolicited messages
	go m.cgregmsg()
	go m.cregmsg()

	// start main loop reading from/parsing/writing to the socket
	var tmp string
	for {
		select {
		case <-m.done:
			return nil
		default:
		}

		// check for read failure
		founderr := false
		// acquire here
		// timeout/nonblocking
		line, err := m.input.ReadString('\r')
		if err != nil {
			return fmt.Errorf("failed to read from socket, exiting")
		}

		// format input for parsing
		line = line[:len(line)-1]                   // strip last character from line
		line = strings.Replace(line, "\r\n", "", 1) // strip off the first carriage return and newline
		if len(line) <= 1 {                         // if this line is blank (or only one character)
			continue // grab another line
		}
		//log.Debug("< " + line + "\n")
		subs := strings.Split(line, ";") // split command into subcommands (if applicable)

		// process all subcommands and build up response string
		out := ""
		waserr := false

		for _, s := range subs { // for each subcommand
			if ok, _ := regexp.MatchString(querypattern, s); ok { // if this subcommand is a query
				tmp, waserr = m.parseQuery(s) // parse the query command
			} else if ok, _ := regexp.MatchString(assignpattern, s); ok { // if this subcommand is an assignment
				tmp, waserr = m.parseAssignment(s) // parse the assignment command
			} else { // if we don't know what this command is
				tmp, waserr = m.otherMessage(s)
			}

			out = out + tmp

			if waserr == true { // if the command generated an error
				founderr = true // set the outer flag
			}
		}
		if founderr == false {
			out = out + msgOK // only say "OK" to commands that don't have an error
		}

		// send response to device
		//log.Debug("> %s\n", out)
		m.output.WriteString(out)
		m.output.Flush()
	}
}

func (m *Modem) Close() error {
	m.done <- true
	close(m.MessageChan)
	return m.conn.Close()
}

func (m *Modem) ClearHistory() {
	m.Inbox = nil
	m.Outbox = nil
}
