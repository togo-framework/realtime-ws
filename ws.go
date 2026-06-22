// Package ws is a channel-based WebSocket transport for togo realtime. It supports
// public and private channels, signed connection tickets (so private channels are
// authenticated), and channel-scoped broadcasting. It implements togo.Broker and
// overrides the default SSE broker when installed.
//
// Publish uses an event of the form "channel:event" to target a channel (private
// channels are prefixed "private-"); an event with no ":" broadcasts to everyone.
//
// Install: `togo install togo-framework/realtime-ws`. Env: REALTIME_SECRET.
package ws

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/togo-framework/togo"
)

var secret []byte

func init() {
	togo.RegisterProviderFunc("realtime-ws", togo.PriorityService+10, func(k *togo.Kernel) error {
		s := os.Getenv("REALTIME_SECRET")
		if s == "" {
			b := make([]byte, 32)
			_, _ = rand.Read(b)
			s = hex.EncodeToString(b)
			if k.Log != nil {
				k.Log.Warn("REALTIME_SECRET not set — using an ephemeral secret (private-channel tickets won't survive a restart)")
			}
		}
		secret = []byte(s)
		b := NewBroker()
		k.Realtime = b
		// Ticket endpoint: the app should mount this behind its auth so only
		// authenticated users get a ticket for private channels.
		k.Router.Get("/events/ticket", issueTicket)
		return nil
	})
}

type client struct {
	ch       chan string
	mu       sync.RWMutex
	channels map[string]struct{}
	authed   bool
}

func (c *client) subscribed(channel string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.channels[channel]
	return ok
}

type broker struct {
	mu      sync.RWMutex
	clients map[*client]struct{}
}

// NewBroker creates a channel-aware WebSocket broker.
func NewBroker() togo.Broker { return &broker{clients: map[*client]struct{}{}} }

// Publish sends to a channel's subscribers. event "channel:name" targets a
// channel; a plain event (no ":") broadcasts to every client.
func (b *broker) Publish(event, data string) {
	channel, name := splitChannel(event)
	frame, _ := json.Marshal(map[string]string{"channel": channel, "event": name, "data": data})
	msg := string(frame)
	b.mu.RLock()
	for c := range b.clients {
		if channel == "" || c.subscribed(channel) {
			select {
			case c.ch <- msg:
			default:
			}
		}
	}
	b.mu.RUnlock()
}

func splitChannel(event string) (channel, name string) {
	if i := strings.IndexByte(event, ':'); i >= 0 {
		return event[:i], event[i+1:]
	}
	return "", event
}

type inbound struct {
	Action  string `json:"action"`
	Channel string `json:"channel"`
}

// Handler upgrades to WebSocket. A valid ?ticket= marks the connection as
// authenticated, permitting subscriptions to private-* channels.
func (b *broker) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		c := &client{ch: make(chan string, 32), channels: map[string]struct{}{}, authed: validTicket(r.URL.Query().Get("ticket"))}
		b.mu.Lock()
		b.clients[c] = struct{}{}
		b.mu.Unlock()
		defer func() {
			b.mu.Lock()
			delete(b.clients, c)
			b.mu.Unlock()
		}()

		ctx := r.Context()
		go b.readLoop(ctx, conn, c)
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

// readLoop handles subscribe/unsubscribe messages from the client.
func (b *broker) readLoop(ctx context.Context, conn *websocket.Conn, c *client) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var in inbound
		if json.Unmarshal(data, &in) != nil {
			continue
		}
		switch in.Action {
		case "subscribe":
			if strings.HasPrefix(in.Channel, "private-") && !c.authed {
				c.ch <- `{"event":"subscription_error","data":"private channel requires a ticket"}`
				continue
			}
			c.mu.Lock()
			c.channels[in.Channel] = struct{}{}
			c.mu.Unlock()
		case "unsubscribe":
			c.mu.Lock()
			delete(c.channels, in.Channel)
			c.mu.Unlock()
		}
	}
}

// --- tickets (HMAC-signed, short-lived) ----------------------------------

// issueTicket returns a signed ticket for private-channel access. Mount behind
// the app's auth middleware so only authenticated users receive one.
func issueTicket(w http.ResponseWriter, r *http.Request) {
	exp := strconv.FormatInt(time.Now().Add(60*time.Second).Unix(), 10)
	nonce := randHex()
	payload := nonce + "." + exp
	ticket := payload + "." + sign(payload)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ticket":"` + ticket + `"}`))
}

func validTicket(ticket string) bool {
	parts := strings.Split(ticket, ".")
	if len(parts) != 3 {
		return false
	}
	payload, sig := parts[0]+"."+parts[1], parts[2]
	if subtle.ConstantTimeCompare([]byte(sig), []byte(sign(payload))) != 1 {
		return false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	return err == nil && time.Now().Unix() < exp
}

func sign(payload string) string {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}

func randHex() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
