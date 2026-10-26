package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

const fixture = `{"version":1,"type":"request","id":"00000000000000000000000000000001","payload":"aGVsbG8="}`

func TestFixture(t *testing.T) {
	m, e := Decode([]byte(fixture))
	if e != nil || string(m.Payload) != "hello" {
		t.Fatal(m, e)
	}
	b, e := Encode(m, 1024)
	if e != nil || string(b) != fixture {
		t.Fatal(string(b), e)
	}
}
func TestPingRequiresV2(t *testing.T) {
	for _, typ := range []string{"ping", "pong"} {
		m := Message{Version: 2, Type: typ, ID: "00000000000000000000000000000001"}
		b, e := Encode(m, 1024)
		if e != nil {
			t.Fatal(e)
		}
		if got, e := Decode(b); e != nil || got.Type != typ {
			t.Fatal(got, e)
		}
	}
}

func TestMalformed(t *testing.T) {
	for _, s := range []string{`{}`, `null`, fixture + ` {}`, `{"version":3,"type":"request","id":"00000000000000000000000000000001"}`, `{"version":1,"type":"ping","id":"00000000000000000000000000000001"}`, `{"version":2,"type":"ping","id":"00000000000000000000000000000001","payload":"eA=="}`, `{"version":1,"version":1}`, `{"version":1,"type":"wrong","id":"00000000000000000000000000000001"}`, `{"x":1}`, `{"id":"bad"}`} {
		if _, e := Decode([]byte(s)); e == nil {
			t.Errorf("accepted %s", s)
		}
	}
}

type shortWriter struct{ bytes.Buffer }

func (w *shortWriter) Write(b []byte) (int, error) {
	if len(b) > 2 {
		b = b[:2]
	}
	return w.Buffer.Write(b)
}
func TestFrames(t *testing.T) {
	w := &shortWriter{}
	if e := WriteFrame(w, []byte(fixture), 1024); e != nil {
		t.Fatal(e)
	}
	got, e := ReadFrame(&w.Buffer, 1024)
	if e != nil || string(got) != fixture {
		t.Fatal(e)
	}
	for _, n := range []uint32{0, 1025, 0xffffffff} {
		var h [4]byte
		binary.BigEndian.PutUint32(h[:], n)
		if _, e := ReadFrame(bytes.NewReader(h[:]), 1024); e == nil {
			t.Fatal(n)
		}
	}
	if _, e := ReadFrame(bytes.NewReader([]byte{0, 0, 0, 3, 1}), 1024); !errors.Is(e, io.ErrUnexpectedEOF) {
		t.Fatal(e)
	}
}
func FuzzProtocol(f *testing.F) {
	f.Add([]byte(fixture))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<20 {
			t.Skip()
		}
		m, e := Decode(b)
		if e == nil {
			encoded, e := Encode(m, 2<<20)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = Decode(encoded); e != nil {
				t.Fatal(e)
			}
		}
	})
}
func FuzzFrame(f *testing.F) {
	f.Add([]byte{0, 0, 0, 1, 1})
	f.Add([]byte{255, 255, 255, 255})
	f.Fuzz(func(t *testing.T, b []byte) { ReadFrame(bytes.NewReader(b), 4096) })
}
func BenchmarkFrame(b *testing.B) {
	payload := bytes.Repeat([]byte("a"), 1024)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		WriteFrame(&buf, payload, 4096)
		ReadFrame(&buf, 4096)
	}
}
func BenchmarkProtocol(b *testing.B) {
	m, _ := Decode([]byte(fixture))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p, _ := Encode(m, 1024)
		Decode(p)
	}
}
