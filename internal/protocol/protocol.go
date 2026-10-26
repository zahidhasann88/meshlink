package protocol

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	Version    = 2
	MinVersion = 1
)
const DefaultMaxFrame = 1 << 20

var ErrFrameTooLarge = errors.New("frame exceeds configured limit")

type Message struct {
	Version int    `json:"version"`
	Type    string `json:"type"`
	ID      string `json:"id"`
	Payload []byte `json:"payload,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (m Message) Validate() error {
	if m.Version < MinVersion || m.Version > Version {
		return errors.New("unsupported protocol version")
	}
	b, e := hex.DecodeString(m.ID)
	if e != nil || len(b) != 16 || m.ID != hex.EncodeToString(b) {
		return errors.New("id must be 32 lowercase hexadecimal characters")
	}
	switch m.Type {
	case "data", "request", "response":
		if m.Error != "" {
			return errors.New("unexpected error field")
		}
	case "error":
		if m.Error != "handler_failed" || len(m.Payload) != 0 {
			return errors.New("invalid error response")
		}
	case "ping", "pong":
		if m.Version < 2 {
			return errors.New("unknown message type")
		}
		if m.Error != "" || len(m.Payload) != 0 {
			return errors.New("ping and pong carry no payload")
		}
	default:
		return errors.New("unknown message type")
	}
	return nil
}
func Encode(m Message, max int) ([]byte, error) {
	if e := m.Validate(); e != nil {
		return nil, e
	}
	if len(m.Payload) > max {
		return nil, ErrFrameTooLarge
	}
	b, e := json.Marshal(m)
	if len(b) > max {
		return nil, ErrFrameTooLarge
	}
	return b, e
}
func Decode(b []byte) (Message, error) {
	var m Message
	d := json.NewDecoder(bytes.NewReader(b))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return m, errors.New("expected JSON object")
	}
	seen := map[string]bool{}
	for d.More() {
		t, e = d.Token()
		if e != nil {
			return m, e
		}
		k, ok := t.(string)
		if !ok || seen[k] {
			return m, errors.New("duplicate or invalid field")
		}
		seen[k] = true
		switch k {
		case "version":
			e = d.Decode(&m.Version)
		case "type":
			e = d.Decode(&m.Type)
		case "id":
			e = d.Decode(&m.ID)
		case "payload":
			e = d.Decode(&m.Payload)
		case "error":
			e = d.Decode(&m.Error)
		default:
			return m, errors.New("unknown field")
		}
		if e != nil {
			return m, e
		}
	}
	if _, e = d.Token(); e != nil {
		return m, e
	}
	if _, e = d.Token(); e != io.EOF {
		return m, errors.New("trailing JSON")
	}
	return m, m.Validate()
}
func ReadFrame(r io.Reader, max int) ([]byte, error) {
	var h [4]byte
	if _, e := io.ReadFull(r, h[:]); e != nil {
		return nil, e
	}
	n := binary.BigEndian.Uint32(h[:])
	if n == 0 {
		return nil, errors.New("empty frame")
	}
	if uint64(n) > uint64(max) {
		return nil, ErrFrameTooLarge
	}
	b := make([]byte, int(n))
	_, e := io.ReadFull(r, b)
	return b, e
}
func WriteFrame(w io.Writer, b []byte, max int) error {
	if len(b) == 0 {
		return errors.New("empty frame")
	}
	if len(b) > max {
		return ErrFrameTooLarge
	}
	var h [4]byte
	binary.BigEndian.PutUint32(h[:], uint32(len(b)))
	for _, p := range [][]byte{h[:], b} {
		for len(p) > 0 {
			n, e := w.Write(p)
			if e != nil {
				return e
			}
			if n <= 0 || n > len(p) {
				return fmt.Errorf("write frame: %w", io.ErrShortWrite)
			}
			p = p[n:]
		}
	}
	return nil
}
