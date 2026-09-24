package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func wsURL(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func testTicket(t *testing.T, channels []string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	IssueTicketFor(rec, channels)
	var body struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode ticket response: %v", err)
	}
	return body.Ticket
}

func dial(t *testing.T, url string, headers http.Header) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

func subscribe(t *testing.T, conn *websocket.Conn, channel string) {
	t.Helper()
	msg, _ := json.Marshal(map[string]string{"action": "subscribe", "channel": channel})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
		t.Fatalf("subscribe write: %v", err)
	}
	// give the server a moment to process the subscribe before the test
	// publishes or asserts anything against it.
	time.Sleep(100 * time.Millisecond)
}

func readFrame(t *testing.T, conn *websocket.Conn) map[string]string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var frame map[string]string
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatalf("decode frame %q: %v", data, err)
	}
	return frame
}

func expectNoFrame(t *testing.T, conn *websocket.Conn, wait time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err == nil {
		t.Fatalf("expected no frame, got %q", data)
	}
}

func TestTicketCannotAccessUngrantedPrivateChannel(t *testing.T) {
	secret = []byte("test-secret")
	b := NewBroker().(*broker)
	srv := httptest.NewServer(b.Handler())
	defer srv.Close()

	ticket := testTicket(t, []string{"private-a"})
	conn := dial(t, wsURL(t, srv)+"?ticket="+ticket, nil)
	defer conn.Close(websocket.StatusNormalClosure, "")

	subscribe(t, conn, "private-b")
	frame := readFrame(t, conn)
	if frame["event"] != "subscription_error" {
		t.Fatalf("expected subscription_error, got %+v", frame)
	}
}

func TestTicketPrefixGrantAllowsMatchingChannel(t *testing.T) {
	secret = []byte("test-secret")
	b := NewBroker().(*broker)
	srv := httptest.NewServer(b.Handler())
	defer srv.Close()

	ticket := testTicket(t, []string{"private-driver.42*"})
	conn := dial(t, wsURL(t, srv)+"?ticket="+ticket, nil)
	defer conn.Close(websocket.StatusNormalClosure, "")

	subscribe(t, conn, "private-driver.42.location")

	b.Publish("private-driver.42.location:update", `{"lat":1}`)
	frame := readFrame(t, conn)
	if frame["event"] != "update" {
		t.Fatalf("expected update frame via prefix grant, got %+v", frame)
	}
}

func TestTamperedTicketRejected(t *testing.T) {
	secret = []byte("test-secret")
	b := NewBroker().(*broker)
	srv := httptest.NewServer(b.Handler())
	defer srv.Close()

	ticket := testTicket(t, []string{"private-a"})
	last := ticket[len(ticket)-1]
	flip := byte('0')
	if last == '0' {
		flip = '1'
	}
	tampered := ticket[:len(ticket)-1] + string(flip)

	conn := dial(t, wsURL(t, srv)+"?ticket="+tampered, nil)
	defer conn.Close(websocket.StatusNormalClosure, "")

	subscribe(t, conn, "private-a")
	frame := readFrame(t, conn)
	if frame["event"] != "subscription_error" {
		t.Fatalf("expected subscription_error for tampered ticket, got %+v", frame)
	}
}

func TestExpiredTicketRejected(t *testing.T) {
	secret = []byte("test-secret")
	orig := ticketTTL
	ticketTTL = 10 * time.Millisecond
	defer func() { ticketTTL = orig }()

	ticket := testTicket(t, []string{"private-a"})
	time.Sleep(50 * time.Millisecond)

	b := NewBroker().(*broker)
	srv := httptest.NewServer(b.Handler())
	defer srv.Close()

	conn := dial(t, wsURL(t, srv)+"?ticket="+ticket, nil)
	defer conn.Close(websocket.StatusNormalClosure, "")

	subscribe(t, conn, "private-a")
	frame := readFrame(t, conn)
	if frame["event"] != "subscription_error" {
		t.Fatalf("expected subscription_error for expired ticket, got %+v", frame)
	}
}

func TestAnonymousPublicChannelNoUnchanneled(t *testing.T) {
	secret = []byte("test-secret")
	b := NewBroker().(*broker)
	srv := httptest.NewServer(b.Handler())
	defer srv.Close()

	conn := dial(t, wsURL(t, srv), nil) // no ticket at all
	defer conn.Close(websocket.StatusNormalClosure, "")

	subscribe(t, conn, "public-lobby")

	b.Publish("public-lobby:hello", `"hi"`)
	frame := readFrame(t, conn)
	if frame["event"] != "hello" {
		t.Fatalf("expected public channel event, got %+v", frame)
	}

	b.Publish("broadcast-all", `"x"`) // no channel prefix: unchanneled
	expectNoFrame(t, conn, 300*time.Millisecond)
}

func TestAuthenticatedReceivesUnchanneled(t *testing.T) {
	secret = []byte("test-secret")
	b := NewBroker().(*broker)
	srv := httptest.NewServer(b.Handler())
	defer srv.Close()

	ticket := testTicket(t, nil)
	conn := dial(t, wsURL(t, srv)+"?ticket="+ticket, nil)
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Give the server a moment to register the (authenticated) client.
	time.Sleep(50 * time.Millisecond)

	b.Publish("broadcast-all", `"x"`)
	frame := readFrame(t, conn)
	if frame["event"] != "broadcast-all" {
		t.Fatalf("expected unchanneled broadcast, got %+v", frame)
	}
}

func TestIssueTicketGrantsNothingWithoutConfiguredFunc(t *testing.T) {
	secret = []byte("test-secret")
	ticketFnMu.Lock()
	ticketChannelFn = nil
	ticketFnMu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/events/ticket", nil)
	rec := httptest.NewRecorder()
	IssueTicket(rec, req)

	var body struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode ticket response: %v", err)
	}

	authed, grants := parseTicket(body.Ticket)
	if !authed {
		t.Fatalf("expected ticket to be structurally valid")
	}
	if len(grants) != 0 {
		t.Fatalf("expected no private grants when SetTicketChannels is unset, got %v", grants)
	}
}

func TestIssueTicketUsesConfiguredFunc(t *testing.T) {
	secret = []byte("test-secret")
	SetTicketChannels(func(r *http.Request) []string { return []string{"private-configured"} })
	defer SetTicketChannels(nil)

	req := httptest.NewRequest(http.MethodGet, "/events/ticket", nil)
	rec := httptest.NewRecorder()
	IssueTicket(rec, req)

	var body struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode ticket response: %v", err)
	}

	_, grants := parseTicket(body.Ticket)
	if len(grants) != 1 || grants[0] != "private-configured" {
		t.Fatalf("expected configured grant, got %v", grants)
	}
}

func TestOverlongChannelNameRejected(t *testing.T) {
	secret = []byte("test-secret")
	b := NewBroker().(*broker)
	srv := httptest.NewServer(b.Handler())
	defer srv.Close()

	conn := dial(t, wsURL(t, srv), nil)
	defer conn.Close(websocket.StatusNormalClosure, "")

	subscribe(t, conn, strings.Repeat("a", 201))
	frame := readFrame(t, conn)
	if frame["event"] != "subscription_error" {
		t.Fatalf("expected subscription_error for overlong channel name, got %+v", frame)
	}
}

func TestDisallowedOriginRefused(t *testing.T) {
	secret = []byte("test-secret")
	prev := originPatterns
	originPatterns = []string{"allowed.example.com"}
	defer func() { originPatterns = prev }()

	b := NewBroker().(*broker)
	srv := httptest.NewServer(b.Handler())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	headers := http.Header{"Origin": []string{"http://evil.example.com"}}
	_, _, err := websocket.Dial(ctx, wsURL(t, srv), &websocket.DialOptions{HTTPHeader: headers})
	if err == nil {
		t.Fatalf("expected dial to be refused for a disallowed Origin")
	}
}

func TestAllowedOriginSucceeds(t *testing.T) {
	secret = []byte("test-secret")
	prev := originPatterns
	originPatterns = []string{"allowed.example.com"}
	defer func() { originPatterns = prev }()

	b := NewBroker().(*broker)
	srv := httptest.NewServer(b.Handler())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	headers := http.Header{"Origin": []string{"http://allowed.example.com"}}
	conn, _, err := websocket.Dial(ctx, wsURL(t, srv), &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatalf("expected dial to succeed for an allowed Origin: %v", err)
	}
	conn.Close(websocket.StatusNormalClosure, "")
}
