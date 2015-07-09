package minimodem

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/gob"
	//log "minilog"
	"time"
)

const (
	csmsSize = 2
)

type Message struct {
	Src      int
	Dst      int
	Message  string
	RefLen   int // 1 or 2, do we use 1 or 2 of the bytes in RefId
	RefId    [2]byte
	TotParts int
	PartId   int
	Time     time.Time
}

// serializes message to binary and then base64
func (m *Message) Raw() (string, error) {
	buf := new(bytes.Buffer)
	encoder := gob.NewEncoder(buf)
	err := encoder.Encode(m)
	if err != nil {
		return "", err
	}
	b := buf.Bytes()
	b64Text := make([]byte, base64.StdEncoding.EncodedLen(len(b)))
	base64.StdEncoding.Encode(b64Text, b)
	return string(b64Text), nil
}

// deserializes message from base64-encoded binary
func EatMessage(b64Text string) (Message, error) {
	b := make([]byte, base64.StdEncoding.DecodedLen(len(b64Text)))
	base64.StdEncoding.Decode(b, []byte(b64Text))
	buf := bytes.NewBuffer(b)
	decoder := gob.NewDecoder(buf)
	var m Message
	err := decoder.Decode(&m)
	if err != nil {
		return Message{}, err
	}
	return m, nil
}

// NewMessage function takes two phone numbers and a message and properly
// bounds checks it, splitting it into multiple Message structs if necessary
func NewMessage(src int, dst int, msg string) []Message {
	var ret []Message
	if len(msg) < 160 { // if message is less than 160 characters, we can send as one message
		message := Message{
			Src:     src,
			Dst:     dst,
			Message: msg,
			Time:    time.Now()}
		ret = append(ret, message)
	} else { // we have to split it up into messages of size <152
		refId := randomRefId() // generate random two-byte reference id
		totParts := len(msg) / 152
		if len(msg)%152 != 0 {
			totParts++
		}
		var msgPart string
		for i := 0; len(msg) > 0; i++ {
			if len(msg) >= 152 {
				msgPart = msg[:152]
				msg = msg[152:]
			} else {
				msgPart = msg[:]
				msg = msg[len(msg):]
			}
			message := Message{
				Src:      src,
				Dst:      dst,
				Message:  msgPart,
				RefLen:   csmsSize,
				RefId:    refId,
				TotParts: totParts,
				PartId:   i + 1,
				Time:     time.Now()}
			ret = append(ret, message)
		}
	}
	//ret = reverseMessages(ret)
	return ret
}

func reverseMessages(msgs []Message) []Message {
	var ret []Message
	for i := len(msgs) - 1; i >= 0; i-- {
		ret = append(ret, msgs[i])
	}
	return ret
}

func randomRefId() [2]byte {
	buf := make([]byte, 2)
	n, err := rand.Read(buf)
	if n != 2 || err != nil {
		return [2]byte{byte('\x00'), byte('\x00')} // don't want to deal with errors, just use this for the reference id. It'll probably work
	}
	if csmsSize == 1 {
		return [2]byte{buf[0], byte('\x00')}
	} else {
		return [2]byte{buf[0], buf[1]}
	}
}
