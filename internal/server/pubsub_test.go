package server

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/hiendvt/go-redis/internal/commands"
	"github.com/hiendvt/go-redis/internal/config"
	"github.com/hiendvt/go-redis/internal/logger"
	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/pubsub"
	"github.com/hiendvt/go-redis/internal/storage"
)

// newPubSubTestServer starts a server with pub/sub fully enabled.
func newPubSubTestServer(t *testing.T) (addr string, cancel func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr = ln.Addr().String()

	cfg := &config.Config{}
	log := logger.New("error")
	store := storage.NewMemoryStore()
	broker := pubsub.NewBroker()
	router := commands.NewRouter(store, nil, broker)
	srv := New(cfg, log, router, broker)

	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.Serve(ctx, ln) //nolint:errcheck
	}()

	cancel = func() {
		stop()
		<-done
	}
	return addr, cancel
}

// readN reads exactly n RESP values from the parser, failing the test on error.
func readN(t *testing.T, p *protocol.Parser, n int) []protocol.Response {
	t.Helper()
	out := make([]protocol.Response, n)
	for i := range n {
		r, err := p.ReadValue()
		if err != nil {
			t.Fatalf("ReadValue[%d]: %v", i, err)
		}
		out[i] = r
	}
	return out
}

// sendCmd sends a RESP array command on conn.
func sendCmd(t *testing.T, conn net.Conn, args ...string) {
	t.Helper()
	elems := make([]protocol.Response, len(args))
	for i, a := range args {
		elems[i] = protocol.BulkString(a)
	}
	if _, err := fmt.Fprint(conn, protocol.Serialize(protocol.Array(elems))); err != nil {
		t.Fatalf("send %v: %v", args, err)
	}
}

// assertPubSubArray checks that r is a 3-element RESP array matching
// [kind, channel, payload/count].
func assertPubSubArray(t *testing.T, r protocol.Response, kind, channel, third string) {
	t.Helper()
	if r.Type != protocol.TypeArray {
		t.Fatalf("expected Array, got type %d", r.Type)
	}
	if len(r.Array) != 3 {
		t.Fatalf("array length: got %d, want 3", len(r.Array))
	}
	if r.Array[0].Str != kind {
		t.Errorf("Array[0]: got %q, want %q", r.Array[0].Str, kind)
	}
	if r.Array[1].Str != channel {
		t.Errorf("Array[1]: got %q, want %q", r.Array[1].Str, channel)
	}
	// third element may be BulkString (channel/payload) or Integer (count)
	switch r.Array[2].Type {
	case protocol.TypeBulkString, protocol.TypeSimpleString:
		if r.Array[2].Str != third {
			t.Errorf("Array[2]: got %q, want %q", r.Array[2].Str, third)
		}
	case protocol.TypeInteger:
		if fmt.Sprint(r.Array[2].Integer) != third {
			t.Errorf("Array[2] integer: got %d, want %s", r.Array[2].Integer, third)
		}
	default:
		t.Errorf("Array[2]: unexpected type %d", r.Array[2].Type)
	}
}

// ── Integration tests ─────────────────────────────────────────────────────────

func TestPubSub_SubscribeAck(t *testing.T) {
	addr, cancel := newPubSubTestServer(t)
	defer cancel()

	conn, _ := net.Dial("tcp", addr)
	defer conn.Close()
	p := protocol.NewParser(conn)

	sendCmd(t, conn, "SUBSCRIBE", "news")
	acks := readN(t, p, 1)
	assertPubSubArray(t, acks[0], "subscribe", "news", "1")
}

func TestPubSub_SubscribeMultipleChannels(t *testing.T) {
	addr, cancel := newPubSubTestServer(t)
	defer cancel()

	conn, _ := net.Dial("tcp", addr)
	defer conn.Close()
	p := protocol.NewParser(conn)

	sendCmd(t, conn, "SUBSCRIBE", "a", "b", "c")
	acks := readN(t, p, 3)
	assertPubSubArray(t, acks[0], "subscribe", "a", "1")
	assertPubSubArray(t, acks[1], "subscribe", "b", "2")
	assertPubSubArray(t, acks[2], "subscribe", "c", "3")
}

func TestPubSub_PublishDelivered(t *testing.T) {
	addr, cancel := newPubSubTestServer(t)
	defer cancel()

	// Subscriber
	subConn, _ := net.Dial("tcp", addr)
	defer subConn.Close()
	subParser := protocol.NewParser(subConn)

	sendCmd(t, subConn, "SUBSCRIBE", "habit.completed")
	readN(t, subParser, 1) // consume ACK

	// Publisher (separate connection)
	pubConn, _ := net.Dial("tcp", addr)
	defer pubConn.Close()
	pubParser := protocol.NewParser(pubConn)

	sendCmd(t, pubConn, "PUBLISH", "habit.completed", "done!")

	// Publisher receives the count of subscribers that got the message.
	countResp, err := pubParser.ReadValue()
	if err != nil {
		t.Fatalf("read publish count: %v", err)
	}
	if countResp.Type != protocol.TypeInteger || countResp.Integer != 1 {
		t.Errorf("publish count: got %+v, want :1", countResp)
	}

	// Subscriber receives the message within a reasonable time.
	msgCh := make(chan protocol.Response, 1)
	go func() {
		r, err := subParser.ReadValue()
		if err == nil {
			msgCh <- r
		}
	}()

	select {
	case msg := <-msgCh:
		assertPubSubArray(t, msg, "message", "habit.completed", "done!")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for pub/sub message")
	}
}

func TestPubSub_PublishNoSubscribers(t *testing.T) {
	addr, cancel := newPubSubTestServer(t)
	defer cancel()

	c := newTestClient(t, addr)
	r := c.do("PUBLISH", "empty", "msg")
	if r.Type != protocol.TypeInteger || r.Integer != 0 {
		t.Errorf("expected :0, got %+v", r)
	}
}

func TestPubSub_Unsubscribe(t *testing.T) {
	addr, cancel := newPubSubTestServer(t)
	defer cancel()

	conn, _ := net.Dial("tcp", addr)
	defer conn.Close()
	p := protocol.NewParser(conn)

	sendCmd(t, conn, "SUBSCRIBE", "ch")
	readN(t, p, 1) // consume subscribe ACK

	sendCmd(t, conn, "UNSUBSCRIBE", "ch")
	acks := readN(t, p, 1)
	assertPubSubArray(t, acks[0], "unsubscribe", "ch", "0")
}

func TestPubSub_UnsubscribeAll(t *testing.T) {
	addr, cancel := newPubSubTestServer(t)
	defer cancel()

	conn, _ := net.Dial("tcp", addr)
	defer conn.Close()
	p := protocol.NewParser(conn)

	sendCmd(t, conn, "SUBSCRIBE", "a", "b")
	readN(t, p, 2) // consume ACKs

	sendCmd(t, conn, "UNSUBSCRIBE") // no args = unsubscribe all
	acks := readN(t, p, 2)
	// Both channels should be unsubscribed; count goes 2→1→0.
	counts := map[int64]bool{}
	for _, a := range acks {
		if a.Type != protocol.TypeArray || len(a.Array) != 3 {
			t.Fatalf("expected 3-element array, got %+v", a)
		}
		counts[a.Array[2].Integer] = true
	}
	if !counts[0] || !counts[1] {
		t.Errorf("expected remaining counts 0 and 1, got %v", counts)
	}
}

func TestPubSub_MultipleSubscribers_AllReceive(t *testing.T) {
	addr, cancel := newPubSubTestServer(t)
	defer cancel()

	const n = 5
	subs := make([]net.Conn, n)
	parsers := make([]*protocol.Parser, n)

	for i := range n {
		c, _ := net.Dial("tcp", addr)
		defer c.Close()
		subs[i] = c
		parsers[i] = protocol.NewParser(c)
		sendCmd(t, c, "SUBSCRIBE", "broadcast")
		readN(t, parsers[i], 1) // consume ACK
	}

	// Give subscriptions a moment to fully register.
	time.Sleep(10 * time.Millisecond)

	pubConn, _ := net.Dial("tcp", addr)
	defer pubConn.Close()
	pubParser := protocol.NewParser(pubConn)
	sendCmd(t, pubConn, "PUBLISH", "broadcast", "hello-all")

	countResp, _ := pubParser.ReadValue()
	if countResp.Integer != int64(n) {
		t.Errorf("publish count: got %d, want %d", countResp.Integer, n)
	}

	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			ch := make(chan protocol.Response, 1)
			go func() {
				r, _ := parsers[i].ReadValue()
				ch <- r
			}()
			select {
			case msg := <-ch:
				assertPubSubArray(t, msg, "message", "broadcast", "hello-all")
			case <-time.After(2 * time.Second):
				t.Errorf("subscriber %d timed out", i)
			}
		}(i)
	}
	wg.Wait()
}

func TestPubSub_RegularCommandsBlockedInSubMode(t *testing.T) {
	addr, cancel := newPubSubTestServer(t)
	defer cancel()

	conn, _ := net.Dial("tcp", addr)
	defer conn.Close()
	p := protocol.NewParser(conn)

	sendCmd(t, conn, "SUBSCRIBE", "ch")
	readN(t, p, 1) // consume ACK

	// SET is not allowed in subscription mode.
	sendCmd(t, conn, "SET", "k", "v")
	errResp, err := p.ReadValue()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if errResp.Type != protocol.TypeError {
		t.Errorf("expected Error response for SET in sub mode, got type %d", errResp.Type)
	}
}

func TestPubSub_DisconnectCleansUp(t *testing.T) {
	// Verify that a disconnected subscriber no longer receives messages
	// and the publish count reflects the removal.
	addr, cancel := newPubSubTestServer(t)
	defer cancel()

	subConn, _ := net.Dial("tcp", addr)
	subParser := protocol.NewParser(subConn)
	sendCmd(t, subConn, "SUBSCRIBE", "ch")
	readN(t, subParser, 1)

	// Give subscription a moment to register.
	time.Sleep(10 * time.Millisecond)

	pubConn, _ := net.Dial("tcp", addr)
	defer pubConn.Close()
	pubParser := protocol.NewParser(pubConn)

	// Close subscriber before publishing.
	subConn.Close()
	time.Sleep(20 * time.Millisecond) // let server process the disconnect

	sendCmd(t, pubConn, "PUBLISH", "ch", "ghost")
	r, _ := pubParser.ReadValue()
	if r.Type != protocol.TypeInteger || r.Integer != 0 {
		t.Errorf("expected 0 receivers after disconnect, got %+v", r)
	}
}
