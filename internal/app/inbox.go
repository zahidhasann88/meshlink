package app

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

const (
	maxListeners  = 4
	listenerQueue = 256
)

type inboxMessage struct {
	Time    time.Time `json:"time"`
	From    string    `json:"from"`
	Payload []byte    `json:"payload"`
}

type inbox struct {
	mu   sync.Mutex
	subs map[chan inboxMessage]struct{}
}

func newInbox() *inbox { return &inbox{subs: map[chan inboxMessage]struct{}{}} }

func (b *inbox) publish(from string, p []byte) {
	msg := inboxMessage{Time: time.Now(), From: from, Payload: append([]byte(nil), p...)}
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (b *inbox) subscribe() (chan inboxMessage, func(), bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.subs) >= maxListeners {
		return nil, nil, false
	}
	ch := make(chan inboxMessage, listenerQueue)
	b.subs[ch] = struct{}{}
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}, true
}

func (b *inbox) serve(w http.ResponseWriter, r *http.Request, done <-chan struct{}) {
	ch, unsubscribe, ok := b.subscribe()
	if !ok {
		http.Error(w, "too many listeners", http.StatusServiceUnavailable)
		return
	}
	defer unsubscribe()
	rc := http.NewResponseController(w)
	rc.SetReadDeadline(time.Time{})
	rc.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	if rc.Flush() != nil {
		return
	}
	enc := json.NewEncoder(w)
	for {
		select {
		case msg := <-ch:
			rc.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if enc.Encode(msg) != nil || rc.Flush() != nil {
				return
			}
		case <-r.Context().Done():
			return
		case <-done:
			return
		}
	}
}
