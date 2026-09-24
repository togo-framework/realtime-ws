// Package ws is a channel-based WebSocket transport for togo realtime. It supports
// public and private channels, signed connection tickets (so private channels are
// authenticated and scoped to the channels the ticket was granted), and
// channel-scoped broadcasting. It implements togo.Broker and overrides the default
// SSE broker when installed.
//
// Publish uses an event of the form "channel:event" to target a channel (private
// channels are prefixed "private-"); an event with no ":" broadcasts to every
// connection that presented a valid ticket (unauthenticated connections never see
// unchanneled broadcasts).
//
// Install: `togo install togo-framework/realtime-ws`. Env: REALTIME_SECRET,
// REALTIME_ORIGINS.
package ws

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/togo-framework/togo"
)

// maxChannelBytes caps channel-name length accepted from clients and grants.
const maxChannelBytes = 200

// defaultTicketTTL is how long an issued ticket remains valid. Tests may
// override it via ticketTTL.
const defaultTicketTTL = 60 * time.Second

var (
	secret           []byte
	originPatterns   []string
	logger           *slog.Logger
	ticketTTL        = defaultTicketTTL
	ticketChannelFn  func(r *http.Request) []string
	ticketFnMu       sync.RWMutex
	warnUnconfigured sync.Once
)

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
		logger = k.Log

		originPatterns = nil
		if o := os.Getenv("REALTIME_ORIGINS"); o != "" {
			for _, p := range strings.Split(o, ",") {
				if p = strings.TrimSpace(p); p != "" {
					originPatterns = append(originPatterns, p)
				}
			}
		}

		k.Realtime = NewBroker()
		return nil
	})
}

// MountTicket mounts IssueTicket at /events/ticket wrapped in the middleware you
// pass — which should be your auth, so only authenticated users get a ticket.
//
// The provider deliberately does not do this for you. A provider that mounts a
// route makes chi's mux "have routes", and any plugin that later calls
// Router.Use — togo's own auth plugin does, for CORS — then panics with
// "chi: all middlewares must be defined before routes on a mux". Leaving the
// mount to the app means this package cannot break another plugin's boot, at
// any provider priority, in any order.
func MountTicket(k *togo.Kernel, mw ...func(http.Handler) http.Handler) {
	k.Router.With(mw...).Get("/events/ticket", IssueTicket)
}

// SetTicketChannels registers the function IssueTicket (and so MountTicket) uses
// to decide which channels the requesting user's ticket is granted. f is called
// per request and should return the exact channel names and/or "prefix*" globs
// the caller may subscribe to (typically their own private-* channels).
//
// If this is never called, IssueTicket grants no private channels — secure by
// default — and logs a one-time warning explaining that SetTicketChannels needs
// to be configured to unlock private-channel access.
func SetTicketChannels(f func(r *http.Request) []string) {
	ticketFnMu.Lock()
	ticketChannelFn = f
	ticketFnMu.Unlock()
}

type client struct {
	ch       chan string
	mu       sync.RWMutex
	channels map[string]struct{}
	authed   bool
	grants   []string
}

func (c *client) subscribed(channel string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.channels[channel]
	return ok
}

// granted reports whether the client's ticket grants access to channel, either
// via an exact match or a "prefix*" glob.
func (c *client) granted(channel string) bool {
	for _, g := range c.grants {
		if strings.HasSuffix(g, "*") {
			if strings.HasPrefix(channel, g[:len(g)-1]) {
				return true
			}
		} else if g == channel {
			return true
		}
	}
	return false
}

type broker struct {
	mu      sync.RWMutex
	clients map[*client]struct{}
}

// NewBroker creates a channel-aware WebSocket broker.
func NewBroker() togo.Broker { return &broker{clients: map[*client]struct{}{}} }

// Publish sends to a channel's subscribers. event "channel:name" targets a
// channel; a plain event (no ":") broadcasts to every authenticated client
// (connections that never presented a valid ticket do not receive it).
func (b *broker) Publish(event, data string) {
	channel, name := splitChannel(event)
	frame, _ := json.Marshal(map[string]string{"channel": channel, "event": name, "data": data})
	msg := string(frame)
	b.mu.RLock()
	for c := range b.clients {
		deliver := false
		switch {
		case channel != "":
			deliver = c.subscribed(channel)
		case c.authed:
			deliver = true
		}
		if deliver {
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
// authenticated and grants the channels (exact names or "prefix*" globs) the
// ticket was issued for, permitting subscriptions to matching private-*
// channels.
func (b *broker) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		opts := &websocket.AcceptOptions{}
		if len(originPatterns) > 0 {
			opts.OriginPatterns = originPatterns
		}
		conn, err := websocket.Accept(w, r, opts)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		authed, grants := parseTicket(r.URL.Query().Get("ticket"))

		// The per-client buffer is sized for high-frequency streams (e.g. driver
		// location updates). When it fills — a slow or stalled reader — new
		// frames are dropped rather than blocking the broker; the client should
		// treat gaps as expected and resync on reconnect.
		c := &client{ch: make(chan string, 256), channels: map[string]struct{}{}, authed: authed, grants: grants}
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
			if len(in.Channel) > maxChannelBytes {
				c.ch <- `{"event":"subscription_error","data":"channel name too long"}`
				continue
			}
			if strings.HasPrefix(in.Channel, "private-") && !c.granted(in.Channel) {
				c.ch <- `{"event":"subscription_error","data":"private channel requires a matching ticket grant"}`
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

// --- tickets (HMAC-signed, short-lived, channel-scoped) ---------------------

// ticketPayload is the JSON payload embedded in every ticket: the channels it
// grants (exact names or "prefix*" globs), its expiry (unix seconds), and a
// nonce to keep otherwise-identical tickets from being byte-for-byte equal.
type ticketPayload struct {
	C []string `json:"c"`
	E int64    `json:"e"`
	N string   `json:"n"`
}

// IssueTicket returns a signed ticket for private-channel access. Mount it behind
// the app's auth middleware — see MountTicket — so only authenticated users
// receive one.
//
// The channels it grants come from SetTicketChannels, called with this request.
// If SetTicketChannels was never configured, the ticket grants no private
// channels (secure by default) and a one-time warning is logged.
func IssueTicket(w http.ResponseWriter, r *http.Request) {
	ticketFnMu.RLock()
	fn := ticketChannelFn
	ticketFnMu.RUnlock()

	var channels []string
	if fn != nil {
		channels = fn(r)
	} else {
		warnUnconfigured.Do(func() {
			msg := "realtime-ws: SetTicketChannels is not configured — IssueTicket will grant no " +
				"private channels. Call ws.SetTicketChannels(func(r *http.Request) []string { ... }) " +
				"during boot to grant the caller's private-* channels (for example " +
				"[]string{\"private-driver.\" + userID})."
			if logger != nil {
				logger.Warn(msg)
			}
		})
	}
	writeTicket(w, channels)
}

// IssueTicketFor writes a ticket granting exactly the given channels. Use it to
// hand-roll ticket issuance instead of going through SetTicketChannels.
func IssueTicketFor(w http.ResponseWriter, channels []string) {
	writeTicket(w, channels)
}

func writeTicket(w http.ResponseWriter, channels []string) {
	if channels == nil {
		channels = []string{}
	}
	p := ticketPayload{C: channels, E: time.Now().Add(ticketTTL).Unix(), N: randHex()}
	raw, _ := json.Marshal(p)
	payload := base64.RawURLEncoding.EncodeToString(raw)
	ticket := payload + "." + sign(payload)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ticket":"` + ticket + `"}`))
}

// parseTicket verifies ticket and, if valid and unexpired, returns (true, its
// granted channels). An invalid, tampered, or expired ticket returns (false, nil)
// — the connection is treated as anonymous.
func parseTicket(ticket string) (authed bool, grants []string) {
	i := strings.LastIndexByte(ticket, '.')
	if i < 0 {
		return false, nil
	}
	payload, sig := ticket[:i], ticket[i+1:]
	if subtle.ConstantTimeCompare([]byte(sig), []byte(sign(payload))) != 1 {
		return false, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return false, nil
	}
	var p ticketPayload
	if json.Unmarshal(raw, &p) != nil {
		return false, nil
	}
	if time.Now().Unix() >= p.E {
		return false, nil
	}
	return true, p.C
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
