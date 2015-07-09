package minimodem

import (
	"testing"
	"time"
)

const (
	serialPath = "/tmp/minimega/0/serial1"
)

func TestModem(t *testing.T) {
	c := make(chan Message)
	go func() {
		for msg := range c {
			t.Logf("Got new message: %v", msg)
		}
	}()

	m, err := NewModem(1234567, serialPath, c)
	if err != nil {
		t.Log("Must have an Android VM running to test modem")
		t.Fatalf("%v", err)
	}

	go m.Run()

	// TODO: Have to wait, will be improved later
	time.Sleep(time.Second * 30)

	msgs := NewMessage(8675309, 1234567, "Hello, world.")

	for _, msg := range msgs {
		if err := m.PushSMS(msg); err != nil {
			t.Fatalf("%v", err)
		}
	}

	msgs = NewMessage(8675309, 1234567, "Hello again, world.")

	for _, msg := range msgs {
		if err := m.PushSMS(msg); err != nil {
			t.Fatalf("%v", err)
		}
	}

	// TODO: Wait for a while so we can try sending a response (manual)
	time.Sleep(time.Second * 30)

	t.Logf("Inbox: %v", m.Inbox)
	t.Logf("Outbox: %v", m.Outbox)

	if err := m.Close(); err != nil {
		t.Errorf("%v", err)
	}
}
