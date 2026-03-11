package pubsub

import (
	"sync"
	"sync/atomic"
	"testing"
)

// ── Broker unit tests ─────────────────────────────────────────────────────────

func TestBroker_PublishNoSubscribers(t *testing.T) {
	b := NewBroker()
	n := b.Publish("ch", "hello")
	if n != 0 {
		t.Errorf("got %d, want 0", n)
	}
}

func TestBroker_SubscribeAndPublish(t *testing.T) {
	b := NewBroker()
	sub := NewSubscriber()
	b.Subscribe("ch", sub)

	n := b.Publish("ch", "hello")
	if n != 1 {
		t.Fatalf("publish count: got %d, want 1", n)
	}

	select {
	case msg := <-sub.Inbox():
		want := EncodePubSubMessage("ch", "hello")
		if msg != want {
			t.Errorf("message: got %q, want %q", msg, want)
		}
	default:
		t.Fatal("inbox empty after publish")
	}
}

func TestBroker_MultipleSubscribers(t *testing.T) {
	b := NewBroker()
	sub1 := NewSubscriber()
	sub2 := NewSubscriber()
	b.Subscribe("ch", sub1)
	b.Subscribe("ch", sub2)

	n := b.Publish("ch", "hi")
	if n != 2 {
		t.Errorf("publish count: got %d, want 2", n)
	}
}

func TestBroker_Unsubscribe(t *testing.T) {
	b := NewBroker()
	sub := NewSubscriber()
	b.Subscribe("ch", sub)
	b.Unsubscribe("ch", sub)

	n := b.Publish("ch", "msg")
	if n != 0 {
		t.Errorf("expected 0 after unsubscribe, got %d", n)
	}
}

func TestBroker_UnsubscribeAll(t *testing.T) {
	b := NewBroker()
	sub := NewSubscriber()
	b.Subscribe("a", sub)
	b.Subscribe("b", sub)
	b.Subscribe("c", sub)

	removed := b.UnsubscribeAll(sub)
	if len(removed) != 3 {
		t.Errorf("removed count: got %d, want 3", len(removed))
	}
	if b.Publish("a", "x") != 0 || b.Publish("b", "x") != 0 || b.Publish("c", "x") != 0 {
		t.Error("subscriber still receiving after UnsubscribeAll")
	}
}

func TestBroker_IdempotentSubscribe(t *testing.T) {
	b := NewBroker()
	sub := NewSubscriber()
	b.Subscribe("ch", sub)
	b.Subscribe("ch", sub) // second call must be a no-op

	n := b.Publish("ch", "msg")
	if n != 1 {
		t.Errorf("duplicate subscribe counted twice: got %d, want 1", n)
	}
}

func TestBroker_EmptyChannelRemovedAfterUnsubscribe(t *testing.T) {
	b := NewBroker()
	sub := NewSubscriber()
	b.Subscribe("ch", sub)
	b.Unsubscribe("ch", sub)

	b.mu.RLock()
	_, exists := b.channels["ch"]
	b.mu.RUnlock()

	if exists {
		t.Error("empty channel should be removed from registry")
	}
}

func TestBroker_ConcurrentPublishSubscribe(t *testing.T) {
	// Hammer the broker with concurrent subscribe/publish/unsubscribe to
	// surface any data races (run with -race).
	b := NewBroker()
	const goroutines = 20
	const rounds = 50

	var received atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sub := NewSubscriber()
			for range rounds {
				b.Subscribe("stress", sub)
				b.Publish("stress", "msg")
				// drain inbox
				for {
					select {
					case <-sub.Inbox():
						received.Add(1)
					default:
						goto next
					}
				}
			next:
				b.Unsubscribe("stress", sub)
			}
			sub.Close()
		}()
	}
	wg.Wait()
	// We only assert that it didn't race or panic; exact count varies.
	t.Logf("total messages received: %d", received.Load())
}

// ── Subscriber unit tests ─────────────────────────────────────────────────────

func TestSubscriber_SendAndReceive(t *testing.T) {
	s := NewSubscriber()
	if !s.Send("hello") {
		t.Fatal("Send returned false on empty inbox")
	}
	select {
	case msg := <-s.Inbox():
		if msg != "hello" {
			t.Errorf("got %q, want %q", msg, "hello")
		}
	default:
		t.Fatal("inbox empty")
	}
}

func TestSubscriber_DropWhenFull(t *testing.T) {
	s := NewSubscriber()
	// Fill the entire buffer.
	for range inboxSize {
		s.Send("x")
	}
	// Next send must be dropped non-blocking.
	if s.Send("overflow") {
		t.Error("expected Send to return false when inbox is full")
	}
}

func TestSubscriber_CloseIdempotent(t *testing.T) {
	s := NewSubscriber()
	s.Close()
	s.Close() // must not panic
}

func TestSubscriber_DoneClosedAfterClose(t *testing.T) {
	// Close() must signal Done(); inbox is never closed (no range possible).
	s := NewSubscriber()
	s.Send("a")
	s.Send("b")
	s.Close()

	select {
	case <-s.Done():
		// correct
	default:
		t.Fatal("Done() channel not closed after Close()")
	}

	// Messages enqueued before Close must still be readable.
	var got []string
	for {
		select {
		case msg := <-s.Inbox():
			got = append(got, msg)
		default:
			goto done
		}
	}
done:
	if len(got) != 2 {
		t.Errorf("got %d buffered messages, want 2", len(got))
	}
}

// ── Message encoding tests ────────────────────────────────────────────────────

func TestEncodePubSubMessage(t *testing.T) {
	got := EncodePubSubMessage("habit.completed", "hello")
	want := "*3\r\n$7\r\nmessage\r\n$15\r\nhabit.completed\r\n$5\r\nhello\r\n"
	if got != want {
		t.Errorf("EncodePubSubMessage:\ngot  %q\nwant %q", got, want)
	}
}

func TestEncodeSubscribeAck(t *testing.T) {
	got := EncodeSubscribeAck("news", 1)
	want := "*3\r\n$9\r\nsubscribe\r\n$4\r\nnews\r\n:1\r\n"
	if got != want {
		t.Errorf("EncodeSubscribeAck:\ngot  %q\nwant %q", got, want)
	}
}

func TestEncodeUnsubscribeAck(t *testing.T) {
	got := EncodeUnsubscribeAck("news", 0)
	want := "*3\r\n$11\r\nunsubscribe\r\n$4\r\nnews\r\n:0\r\n"
	if got != want {
		t.Errorf("EncodeUnsubscribeAck:\ngot  %q\nwant %q", got, want)
	}
}
