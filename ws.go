// Package ws is a WebSocket transport for togo realtime. It implements
// togo.Broker and registers with higher priority than the default SSE broker, so
// installing it upgrades the app to WebSockets. Install: `togo install togo-framework/realtime-ws`.
package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/togo-framework/togo"
)

func init() {
	// PriorityService+10 so this overrides the built-in SSE realtime when both
	// are installed.
	togo.RegisterProviderFunc("realtime-ws", togo.PriorityService+10, func(k *togo.Kernel) error {
		k.Realtime = NewBroker()
		return nil
	})
}

type client struct{ ch chan string }

type broker struct {
	mu      sync.RWMutex
	clients map[*client]struct{}
}

// NewBroker creates a WebSocket broker.
func NewBroker() togo.Broker { return &broker{clients: map[*client]struct{}{}} }

// Publish broadcasts an event as a JSON frame to all connected clients.
func (b *broker) Publish(event, data string) {
	frame, _ := json.Marshal(map[string]string{"event": event, "data": data})
	msg := string(frame)
	b.mu.RLock()
	for c := range b.clients {
		select {
		case c.ch <- msg:
		default: // drop for slow clients
		}
	}
	b.mu.RUnlock()
}

// Handler upgrades the connection to a WebSocket and streams published frames.
func (b *broker) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		c := &client{ch: make(chan string, 32)}
		b.mu.Lock()
		b.clients[c] = struct{}{}
		b.mu.Unlock()
		defer func() {
			b.mu.Lock()
			delete(b.clients, c)
			b.mu.Unlock()
		}()

		ctx := r.Context()
		// Reader: detect client close.
		go func() {
			for {
				if _, _, err := conn.Read(ctx); err != nil {
					return
				}
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-c.ch:
				wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := conn.Write(wctx, websocket.MessageText, []byte(msg))
				cancel()
				if err != nil {
					return
				}
			}
		}
	}
}
