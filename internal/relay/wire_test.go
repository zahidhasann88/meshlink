package relay

import (
	"bytes"
	"strings"
	"testing"
)

const (
	testID      = "66687aadf862bd776c8fc18b8e9f8e20089714856ee233b3902a591d0d5f2925"
	testCircuit = "00000000000000000000000000000001"
)

func TestMessageRoundTrip(t *testing.T) {
	for _, m := range []Message{{Type: "reserve"}, {Type: "ping"}, {Type: "pong"}, {Type: "ok"}, {Type: "connect", Target: testID}, {Type: "accept", Circuit: testCircuit}, {Type: "incoming", From: testID, Circuit: testCircuit}, {Type: "error", Error: "nope"}} {
		var b bytes.Buffer
		if e := Write(&b, m); e != nil {
			t.Fatal(m, e)
		}
		got, e := Read(&b)
		if e != nil || got != m {
			t.Fatal(m, got, e)
		}
	}
}

func TestMessageRejectsInvalid(t *testing.T) {
	for _, m := range []Message{{}, {Type: "unknown"}, {Type: "ok", Target: testID}, {Type: "connect"}, {Type: "connect", Target: "BAD"}, {Type: "accept", Circuit: "xyz"}, {Type: "incoming", From: testID}, {Type: "error"}, {Type: "error", Error: strings.Repeat("x", 201)}} {
		if e := Write(&bytes.Buffer{}, m); e == nil {
			t.Fatal("accepted", m)
		}
	}
	for _, raw := range []string{`{"type":"ok","extra":1}`, `{"type":"ok"}{"type":"ok"}`, `[]`, `{"type":"ping"} x`} {
		var b bytes.Buffer
		b.Write([]byte{0, 0, 0, byte(len(raw))})
		b.WriteString(raw)
		if _, e := Read(&b); e == nil {
			t.Fatal("accepted", raw)
		}
	}
	if _, e := Read(bytes.NewReader([]byte{0, 1, 0, 0})); e == nil {
		t.Fatal("accepted oversized frame")
	}
}

func FuzzRelayMessage(f *testing.F) {
	f.Add([]byte(`{"type":"incoming","from":"` + testID + `","circuit":"` + testCircuit + `"}`))
	f.Add([]byte(`{"type":"error","error":"x"}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		var in bytes.Buffer
		in.Write([]byte{byte(len(b) >> 24), byte(len(b) >> 16), byte(len(b) >> 8), byte(len(b))})
		in.Write(b)
		m, e := Read(&in)
		if e != nil {
			return
		}
		var out bytes.Buffer
		if e = Write(&out, m); e != nil {
			t.Fatal("valid message failed to re-encode", m, e)
		}
	})
}
