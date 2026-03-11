package pubsub

import "sync"

// inboxSize is the number of messages that can be buffered before a slow
// subscriber starts dropping incoming messages. Publishers are never blocked.
const inboxSize = 256

// Subscriber represents one client's message mailbox in the pub/sub system.
//
// Concurrency design — why we never close the inbox channel:
//
// Closing a channel while another goroutine may be sending to it causes a
// panic. Because Publish snapshots subscribers and then sends outside the
// broker lock, a concurrent Close (triggered by client disconnect) can race
// with an in-flight Send. To eliminate this panic, the inbox channel is kept
// open for the lifetime of the Subscriber; shutdown is signalled via a
// separate done channel that the write loop selects on.
//
// Ownership rules:
//   - The handler goroutine is the only caller of Send during ACK delivery.
//   - Broker goroutines call Send for PUBLISH deliveries (concurrent).
//   - The write loop goroutine is the only reader of Inbox and Done.
//   - Close is called exactly once by the handler goroutine on disconnect.
type Subscriber struct {
	inbox chan string
	done  chan struct{}
	once  sync.Once
}

// NewSubscriber creates a Subscriber with a buffered inbox and a done signal.
func NewSubscriber() *Subscriber {
	return &Subscriber{
		inbox: make(chan string, inboxSize),
		done:  make(chan struct{}),
	}
}

// Send delivers msg to the subscriber's inbox without blocking.
// Returns false immediately if the subscriber is closed or the inbox is full.
//
// The inbox channel itself is never closed, so this method never panics — even
// if Close() races with Send().
func (s *Subscriber) Send(msg string) bool {
	// Fast path: check done before attempting the send.
	select {
	case <-s.done:
		return false
	default:
	}
	// inbox is never closed, so this select cannot panic.
	select {
	case s.inbox <- msg:
		return true
	default:
		return false // inbox full — drop message
	}
}

// Inbox returns a receive-only view of the inbox channel.
// Only the write loop goroutine should read from this.
func (s *Subscriber) Inbox() <-chan string {
	return s.inbox
}

// Done returns a channel that is closed when Close() is called.
// The write loop selects on this to know when to drain and exit.
func (s *Subscriber) Done() <-chan struct{} {
	return s.done
}

// Close signals the write loop to drain remaining inbox messages and exit.
// Safe to call concurrently; subsequent calls are no-ops.
// The inbox channel itself is never closed.
func (s *Subscriber) Close() {
	s.once.Do(func() { close(s.done) })
}
