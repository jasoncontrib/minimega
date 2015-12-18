// Copyright (2015) Sandia Corporation.
// Under the terms of Contract DE-AC04-94AL85000 with Sandia Corporation,
// the U.S. Government retains certain rights in this software.

package minimodem

import (
	"encoding/hex"
	"fmt"
	"math"
	log "minilog"
	"strconv"
	"time"
)

type PDU struct {
	// See http://www.smartposition.nl/resources/sms_pdu.html and http://www.activexperts.com/xmstoolkit/sms/technical/
	SMSCLen   byte   // Length of SMSC information
	SMSCType  byte   // Type of address of SMSC
	SMSCNum   []byte // SMSC number -- swizzled
	PDUType   byte   // Either SMS-DELIVER (00) or SMS-SUBMIT (01) - Deliver is for received msgs, Submit for sent msgs (submit to cell tower, deliver to device)
	TPMsgRef  byte   // SMS-SUBMIT ONLY - The message reference number
	SenAdrLen byte   // Length of the sender address
	SenAdrTyp byte   // Type of address of the sender number
	SenderNum []byte // Sender number -- swizzled
	ProtoIden byte   // Protocol identifier
	DataEnc   byte   // Data encoding scheme
	Timestamp []byte // SMS-DELIVER ONLY - Timestamp -- swizzled
	Validity  byte   // SMS-SUBMIT ONLY - Validity period - only present if PDUType has (10 or 18) set
	UsrDatLen byte   // Length of User data (SMS Message)
	UserData  []byte // User data -- 8-bit octets representing 7-bit data
}

func (p *PDU) toString() string {
	var ret string
	ret += fmt.Sprintf("Raw: %v\n", *p)
	ret += fmt.Sprintf("SMSCLen: %x\n", p.SMSCLen)
	ret += fmt.Sprintf("SMSCType: %x\n", p.SMSCType)
	ret += fmt.Sprintf("SMSCNum: %x\n", p.SMSCNum)
	ret += fmt.Sprintf("PDUType: %x\n", p.PDUType)
	ret += fmt.Sprintf("TPMsgRef: %x\n", p.TPMsgRef)
	ret += fmt.Sprintf("SenAdrLen: %x\n", p.SenAdrLen)
	ret += fmt.Sprintf("SenAdrTyp: %x\n", p.SenAdrTyp)
	ret += fmt.Sprintf("SenderNum: %x\n", p.SenderNum)
	ret += fmt.Sprintf("ProtoIden: %x\n", p.ProtoIden)
	ret += fmt.Sprintf("DataEnc: %x\n", p.DataEnc)
	ret += fmt.Sprintf("Timestamp: %x\n", p.Timestamp)
	ret += fmt.Sprintf("Validity: %x\n", p.Validity)
	ret += fmt.Sprintf("UsrDatLen: %x\n", p.UsrDatLen)
	ret += fmt.Sprintf("UserData: %x\n", p.UserData)
	return ret
}

func pduSmsUnpack(pdu string) (msg Message, err error) {
	/*
		Sample SMS-DELIVER pdu: 00000b815155667677f700000000000000000009c834888a2ecbcb21
	*/
	o := 0 // offset
	var p PDU
	SMSCLen, _ := hex.DecodeString(pdu[o : o+2])
	p.SMSCLen = SMSCLen[0]
	if int(p.SMSCLen) > 0 { // if an SMSC follows
		SMSCType, _ := hex.DecodeString(pdu[o+2 : o+4]) // grab next byte for SMSC type
		p.SMSCType = SMSCType[0]
		p.SMSCNum, _ = hex.DecodeString(pdu[o+4 : o+2+(2*int(p.SMSCLen))]) // grab next len-1 bytes for SMSC number
	}
	o += 2 + int(p.SMSCLen) // offset now points to PDUType field
	PDUType, _ := hex.DecodeString(pdu[o : o+2])
	p.PDUType = PDUType[0]
	o += 2
	if (p.PDUType & byte('\x01')) != byte('\x00') { // if SMS-SUBMIT Type (it should be if Android sent this to us)
		TPMsgRef, _ := hex.DecodeString(pdu[o : o+2])
		p.TPMsgRef = TPMsgRef[0]
		o += 2
	}
	SenAdrLen, _ := hex.DecodeString(pdu[o : o+2])
	p.SenAdrLen = SenAdrLen[0]
	SenAdrTyp, _ := hex.DecodeString(pdu[o+2 : o+4])
	p.SenAdrTyp = SenAdrTyp[0]
	var sender_address_length int
	if int(p.SenAdrLen)%2 == 1 {
		sender_address_length = int(p.SenAdrLen) + 1
	} else {
		sender_address_length = int(p.SenAdrLen)
	}
	p.SenderNum, _ = hex.DecodeString(pdu[o+4 : o+4+(sender_address_length)]) // this number is still swizzled
	o += 4 + (sender_address_length)
	ProtoIden, _ := hex.DecodeString(pdu[o : o+2])
	p.ProtoIden = ProtoIden[0]
	DataEnc, _ := hex.DecodeString(pdu[o+2 : o+4])
	p.DataEnc = DataEnc[0]
	o += 4
	// TODO: Check to make sure DataEnc is \x00 and if not, error - we can only decode 7-bit data right now
	if (p.PDUType & byte('\x03')) == byte('\x00') { // if this is a SMS-DELIVER Type pdu (and it shouldn't be if Android sent this to us)
		p.Timestamp, _ = hex.DecodeString(pdu[o : o+14])
		o += 14
	}
	if (p.PDUType & byte('\x10')) != byte('\x00') { // if there is a Validity period byte
		Validity, _ := hex.DecodeString(pdu[o : o+2])
		p.Validity = Validity[0]
		o += 2
	}
	UsrDatLen, _ := hex.DecodeString(pdu[o : o+2])
	p.UsrDatLen = UsrDatLen[0]
	p.UserData, _ = hex.DecodeString(pdu[o+2:]) // grab the rest
	// now unpack the data types we care about
	msg = PduUserDataDecode(p.UsrDatLen, p.UserData)
	msg.Time = time.Now()
	num, err := strconv.Atoi(unswizzle(p.SenderNum))
	if err != nil {
		return Message{}, err
	}
	if (p.PDUType & byte('\x03')) == byte('\x00') { // if this is a SMS-DELIVER Type pdu (only for testing - not generally used)
		msg.Src = num
	} else if (p.PDUType & byte('\x03')) == byte('\x01') { // if this is a SMS-SUBMIT type pdu (we're unpacking something we packed and plan to send elsewhere)
		msg.Dst = num
	}
	return msg, nil
}

// if you're sending a single-part sms message, leave CSMSNum=nil, totNum=0, myNum=0
func pduSmsPack(msg Message) (string, error) {
	var p PDU
	p.SMSCLen = byte('\x00') // no SMSC specified, so 0 bytes used for SMSC
	//p.SMSCType = nil // no SMSC, so no type is necessary
	//p.SMSCNum = nil // no SMSC
	if msg.RefLen != 0 { // if this is a multi-part message
		p.PDUType = byte('\x40') // 01000000 - bit 6 turns on the UDHI (multi-message) indicator, bits 0,1 set to 00 means SMS-DELIVER type
	} else {
		p.PDUType = byte('\x00') // SMS-DELIVER
	}
	//p.TPMsgRef = nil // doesn't exist for SMS-DELIVER type
	p.SenAdrLen = byte(len(strconv.Itoa(msg.Src)))
	p.SenAdrTyp = byte('\x81')                           // Non-international number - use '\x91' for international number (starts with +)
	p.SenderNum = swizzle(strconv.Itoa(msg.Src))         // returns []byte
	p.ProtoIden = byte('\x00')                           // SMS protocol
	p.DataEnc = byte('\x00')                             // Data encoded using 7-bit default alphabet encoding scheme - more options explained in activexperts.com link above
	p.Timestamp = []byte("\x00\x00\x00\x00\x00\x00\x00") // Fix this if it gives issues, it's just a blank timestamp
	//p.Validity = nil // not present in SNS-DELIVER type
	if msg.RefLen == 0 {
		p.UsrDatLen = byte(len(msg.Message)) // number of septets, not number of octets after encoding
	} else if msg.RefLen == 1 {
		p.UsrDatLen = byte(len(msg.Message) + 7) // number of septets, not number of octets after encoding
	} else if msg.RefLen == 2 {
		p.UsrDatLen = byte(len(msg.Message) + 8) // number of septets, not number of octets after encoding
	}
	ud, err := PduUserDataEncode(msg) // converts message octets to septets
	if err != nil {
		return "", err
	}
	p.UserData = ud

	var pdu_bytes []byte
	pdu_bytes = append(pdu_bytes, p.SMSCLen)
	pdu_bytes = append(pdu_bytes, p.PDUType)
	pdu_bytes = append(pdu_bytes, p.SenAdrLen)
	pdu_bytes = append(pdu_bytes, p.SenAdrTyp)
	pdu_bytes = append(pdu_bytes, p.SenderNum...)
	pdu_bytes = append(pdu_bytes, p.ProtoIden)
	pdu_bytes = append(pdu_bytes, p.DataEnc)
	pdu_bytes = append(pdu_bytes, p.Timestamp...)
	pdu_bytes = append(pdu_bytes, p.UsrDatLen)
	pdu_bytes = append(pdu_bytes, p.UserData...)

	return hex.EncodeToString(pdu_bytes), nil
}

func PduUserDataEncode(msg Message) ([]byte, error) {
	s := msg.Message
	CSMSNum := msg.RefId
	totNum := msg.TotParts
	myNum := msg.PartId
	// if we have a multi-part message, we need to encode a User Data Header into the message
	var header []byte   // nil if we don't have a header
	if msg.RefLen > 0 { // we have a CSMSNum, so we have a header
		if msg.RefLen == 1 { // total length is 6 bytes, 7 septets - 1 bit worth, so we'll prepend 7 zero-chars
			header = append(header, byte('\x05'), byte('\x00'), byte('\x03'), CSMSNum[0], byte(totNum), byte(myNum))
			s = "\x00\x00\x00\x00\x00\x00\x00" + s
		} else if msg.RefLen == 2 { // total length is 7 bytes, 8 septets worth, so we'll prepend 8 zero-chars
			header = append(header, byte('\x06'), byte('\x08'), byte('\x04'), CSMSNum[0], CSMSNum[1], byte(totNum), byte(myNum))
			s = "\x00\x00\x00\x00\x00\x00\x00\x00" + s
		} else { // invalid CSMS number
			return nil, fmt.Errorf("invalid CSMS length: %d\n", msg.RefLen)
		}
	}
	// do some length checking on the string we're encoding
	if len(s) > 160 { // if the message is longer than 160 characters (140 bytes after encoding), that's bad
		return nil, fmt.Errorf("message too long. max length is 160 characters, or 154/153 if a multi-part message")
	}

	/* Walkthrough
		   Sample string: "hellohello"
			 Hex: 68 65 6c 6c 6f 68 65 6c 6c 6f
			 68: 0|110 1000
			 65: 0|110 0101
			 6c: 0|110 1100
			 6c: 0|110 1100
			 6f: 0|110 1111
			 Remove 8th bit (zeros) and take least-significant bits from subsequent bytes and add them to previous bytes
			 See https://mobileforensics.files.wordpress.com/2007/06/understanding_sms.pdf for good visual for this
			 68:                                                                         1101000
			 65:                                                                 110010|1
			 6c:                                                         11011|00
			 6c:                                                 1101|100
			 6f:                                         110|1111
			 68:                                 11|01000
			 65:                         1|100101
			 6c:                 |1101100
			 6c:          1101100
			 6f:  110111|1
	 filler:00
		   So, final bytes are:
			 11101000|00110010|10011011|11111101|01000110|10010111|11011001|11101100|00110111
			 e8 32 9b fd 46 97 d9 ec 37 -> final hex string
	*/

	length := int(math.Ceil(float64(len(s))*7.0/8.0)) + 1 // 7/8 the length of s, rounded up, plus one we'll throw away at the end
	ret := make([]byte, length)                           // make the empty byte array we will eventually return
	for idx, char := range []byte(s) {                    // iterate over all bytes of s
		row := idx - (idx / 8)
		prevrow := row - 1 + btoi(idx%8 == 0) // prevrow is same row on first row and every 8th thereafter
		// add least significant bits
		lsb := char << uint(8-(idx%8)) // take least-significant bits and shift them left to line up with above row
		ret[prevrow] += lsb            // add least significant bits to previous row
		// add most significant bits
		msb := char >> uint(idx%8)
		ret[row] += msb
	}
	userData := ret[:length-1] // throw away last extra byte
	if header != nil {         // if we have a header
		userData = append(header, userData[len(header):]...) // prepend the header
	}
	log.Debug("msg: %v", msg)
	log.Debug("userdata header: %x", header)
	log.Debug("userdata:        %x", userData)
	return userData, nil
}

func PduUserDataDecode(usrDatLen byte, b []byte) (msg Message) {
	if len(b) == 0 {
		return
	}
	// we need to check if the UserData has a UDH field (6 or 7 bytes): http://en.wikipedia.org/wiki/Concatenated_SMS#PDU_Mode_SMS
	msg = Message{}
	UDHLen := byte('\xFF')
	if b[0] <= byte('\x06') { // first byte less than or equal to 0x06 means we have a header
		UDHLen = b[0]             // number of following bytes, not including this one, so we'll use UDHLen+1 below
		if b[1] == byte('\x00') { // one-byte CSMS reference number
			msg.RefLen = 1
			msg.RefId = [2]byte{b[3], byte('\x00')}
			msg.TotParts = int(b[4])
			msg.PartId = int(b[5])
		} else if b[1] == byte('\x08') { // two-byte CSMS reference number
			msg.RefLen = 2
			msg.RefId = [2]byte{b[3], b[4]} // grab bytes 3 and 4
			msg.TotParts = int(b[5])
			msg.PartId = int(b[6])
		}
		b = append(make([]byte, UDHLen+1), b[UDHLen+1:]...) // zero out the first UDHLen+1 bytes
	}

	/*
	                                                                         1|1101000
	                                                                00|110010
	                                                       100|11011
	                                              1111|1101
	                                     01000|110
	                            100101|11
	                   1101100|1
	          1|1101100
	 00|110111
	*/
	length := int(math.Floor(float64(len(b))*8.0/7.0)) + 1 // 8/7 the length of b, rounded down, plus one extra byte we'll discard at the end
	s := make([]byte, length)                              // make the empty byte array we will eventually return )to be converted to a string at end
	for i := 0; i < len(b); i++ {
		idx := i + (i / 7)                        // find result byte index
		lsb := (b[i] << uint(i%7)) & byte('\x7f') // left-shift the least-significant bits, then mask the most significant bit
		s[idx] += lsb                             // write the lsb into the result byte
		msb := b[i] >> uint(7-(i%7))              // right shift the most-significant bits back to the least-significant bits location, no need to mask
		s[idx+1] += msb                           // write the msb into the next byte - on the last byte, this will be filler 0's, which is why we have the extra byte in s
	}
	s = s[:length-1]            // strip off extra byte used for filler 0's
	if UDHLen != byte('\xFF') { // if we had a header
		s = s[UDHLen+2:] // strip off UDHLen+1 bytes, plus one more for the extra 7->8 byte unpack. If it was a 6-byte UDH, there would have been an extra 0-bit appended, so no worries.
	}
	// if the message is len%8 == 7, then the extra 0000000 padding is indistinguishable from an 8th byte that is all 0's
	if len(s) > int(usrDatLen) {
		s = s[:int(usrDatLen)] // we use usrDatLen to check the expected length, and if necessary, strip the unintended null byte from the end
	}
	msg.Message = string(s)
	return
}

func btoi(b bool) (i int) {
	if b {
		i = 1
	} else {
		i = 0
	}
	return
}

func unswizzle(s []byte) string {
	a := hex.EncodeToString(s)
	pn := ""
	for i := 0; i < len(a); i += 2 {
		pn += string(a[i+1]) + string(a[i])
	}
	if pn[len(pn)-1] == 'f' {
		pn = pn[:len(pn)-1]
	}
	return pn
}

func swizzle(pn string) (s []byte) {
	if len(pn)%2 == 1 {
		pn = pn + "F"
	}
	temp_str := ""
	for i := 0; i < len(pn); i += 2 {
		temp_str += string(pn[i+1]) + string(pn[i])
	}
	s, _ = hex.DecodeString(temp_str)
	return
}
