// Copyright (2015) Sandia Corporation.
// Under the terms of Contract DE-AC04-94AL85000 with Sandia Corporation,
// the U.S. Government retains certain rights in this software.

package minimodem

import (
	"testing"
)

const (
	src         = 1234567
	dst         = 7654321
	pdu         = "0001000781685703f9000011f4f29c9e769f63b219ad66bbe17230"
	jennyNumber = 8675309
	jennyMsg    = "testing1234567890"
)

func TestPdu(t *testing.T) {
	messages := []string{
		"",
		"a",
		"bb",
		"ccc",
		"dddd",
		"eeeee",
		"ffffff",
		"ggggggg",
		"hhhhhhhh",
		"iiiiiiiii",
		"jjjjjjjjjj",
		"kkkkkkkkkkk",
		"llllllllllll",
		"mmmmmmmmmmmmm",
		"nnnnnnnnnnnnnn",
		"ooooooooooooooo",
		"pppppppppppppppp",
		"qqqqqqqqqqqqqqqqq",
		"rrrrrrrrrrrrrrrrrr",
		"sssssssssssssssssss",
		"tttttttttttttttttttt",
		"uuuuuuuuuuuuuuuuuuuuu",
		"vvvvvvvvvvvvvvvvvvvvvv",
		"wwwwwwwwwwwwwwwwwwwwwww",
		"yyyyyyyyyyyyyyyyyyyyyyyy",
		"zzzzzzzzzzzzzzzzzzzzzzzzz",
		"abcdefghijklmno  pqrst uvwxy z/*2'\"^!@#%^&_+)39(",
		"aaaaaaaaaaaaaaaaaaaaaabbbbbbbbbbbbbbbbbbbbbbbbbbcccccccccccccccccdddd" +
			"dddddddddddeeeeeeeeffffggggggggggggggggghhhhhhhhhhiiiiiiiiijjjjjjjjjk" +
			"kkkkkkkkkkkllllllmmmmmnnnnnnnnnoooooooopppppppppqqqqqqrrrrrrrrrssssss" +
			"ttttttttuuuuuvvvvvvvvwwwwwwwwxxxxxyyyyyyyyyyzzzzzzzzzz"}

	// test 1: unpack a known message from a raw pdu
	{
		msg, err := pduSmsUnpack(pdu)
		if err != nil {
			t.Fatalf("%v", err)
		}
		if msg.Dst != jennyNumber {
			t.Logf("expected: %d", jennyNumber)
			t.Logf("unpacked: %d", msg.Src)
			t.Fatalf("number unpacked incorrectly")
		}
		if msg.Message != jennyMsg {
			t.Logf("expected: %s", jennyMsg)
			t.Logf("unpacked: %s", msg.Message)
			t.Fatalf("message unpacked incorrectly")
		}
	}

	// test 2: pack and unpack a series of messages
	{
		for _, message := range messages {
			msgs := NewMessage(src, dst, message)
			var pdus []string
			for _, m := range msgs {
				pdu, err := pduSmsPack(m)
				if err != nil {
					t.Fatalf("%v", err)
				}
				pdus = append(pdus, pdu)
			}
			resultMessage := ""
			for _, pdu := range pdus {
				msg, err := pduSmsUnpack(pdu)
				if err != nil {
					t.Fatalf("%v", err)
				}
				resultMessage += msg.Message
			}
			if resultMessage != message {
				t.Logf("original:      %x", message)
				t.Logf("reconstructed: %x", resultMessage)
				t.Fatalf("multi-part message did not pack/unpack/reconstruct successfully")
			}
		}
	}
}
