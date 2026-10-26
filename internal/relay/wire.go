package relay

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/zahidhasann88/meshlink/internal/identity"
	"github.com/zahidhasann88/meshlink/internal/protocol"
)

const maxMessage = 1024

var ErrRejected = errors.New("relay rejected request")

type Message struct {
	Type    string `json:"type"`
	Target  string `json:"target,omitempty"`
	From    string `json:"from,omitempty"`
	Circuit string `json:"circuit,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (m Message) Err() error { return fmt.Errorf("%w: %s", ErrRejected, m.Error) }

func validCircuit(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 16 && s == hex.EncodeToString(b)
}

func (m Message) validate() error {
	empty := m.Target == "" && m.From == "" && m.Circuit == "" && m.Error == ""
	ok := false
	switch m.Type {
	case "reserve", "ping", "pong", "ok":
		ok = empty
	case "connect":
		ok = identity.ValidID(m.Target) && m.From == "" && m.Circuit == "" && m.Error == ""
	case "accept":
		ok = validCircuit(m.Circuit) && m.Target == "" && m.From == "" && m.Error == ""
	case "incoming":
		ok = identity.ValidID(m.From) && validCircuit(m.Circuit) && m.Target == "" && m.Error == ""
	case "error":
		ok = m.Error != "" && len(m.Error) <= 200 && m.Target == "" && m.From == "" && m.Circuit == ""
	}
	if !ok {
		return errors.New("invalid relay message")
	}
	return nil
}

func Write(w io.Writer, m Message) error {
	if e := m.validate(); e != nil {
		return e
	}
	b, e := json.Marshal(m)
	if e != nil {
		return e
	}
	return protocol.WriteFrame(w, b, maxMessage)
}

func Read(r io.Reader) (Message, error) {
	var m Message
	b, e := protocol.ReadFrame(r, maxMessage)
	if e != nil {
		return m, e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(&m); e != nil {
		return m, e
	}
	if d.More() {
		return m, errors.New("trailing relay data")
	}
	return m, m.validate()
}
