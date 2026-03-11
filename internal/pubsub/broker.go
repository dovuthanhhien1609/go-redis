package pubsub

import "sync"

// Broker manages the channel-to-subscriber registry and routes messages from
// publishers to all active subscribers on a channel.
//
// It is safe for concurrent use — multiple handler goroutines call Subscribe
// and Unsubscribe, and multiple publisher goroutines call Publish simultaneously.
type Broker struct {
	mu       sync.RWMutex
	channels map[string]map[*Subscriber]struct{}
}

// NewBroker creates an empty Broker.
func NewBroker() *Broker {
	return &Broker{
		channels: make(map[string]map[*Subscriber]struct{}),
	}
}

// Subscribe registers sub as a listener on channel.
// If sub is already subscribed to channel, this is a no-op.
func (b *Broker) Subscribe(channel string, sub *Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.channels[channel] == nil {
		b.channels[channel] = make(map[*Subscriber]struct{})
	}
	b.channels[channel][sub] = struct{}{}
}

// Unsubscribe removes sub from channel.
// If the channel becomes empty after removal it is deleted from the registry,
// keeping memory usage proportional to active subscriptions.
func (b *Broker) Unsubscribe(channel string, sub *Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.channels[channel]
	if subs == nil {
		return
	}
	delete(subs, sub)
	if len(subs) == 0 {
		delete(b.channels, channel)
	}
}

// UnsubscribeAll removes sub from every channel it is registered on.
// Returns the names of the channels it was removed from.
// Called by the handler on client disconnect to guarantee no dangling references.
func (b *Broker) UnsubscribeAll(sub *Subscriber) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var removed []string
	for ch, subs := range b.channels {
		if _, ok := subs[sub]; ok {
			delete(subs, sub)
			removed = append(removed, ch)
			if len(subs) == 0 {
				delete(b.channels, ch)
			}
		}
	}
	return removed
}

// Publish sends message to all current subscribers of channel.
// Returns the number of subscribers that actually received the message.
//
// Non-blocking guarantee: the read lock is held only for snapshotting the
// subscriber set. The actual channel sends happen after the lock is released,
// so a slow subscriber's full inbox never stalls the publisher or other subs.
func (b *Broker) Publish(channel, message string) int {
	// Snapshot subscribers under a read lock so the lock is held as briefly
	// as possible and never during channel sends.
	b.mu.RLock()
	subs := b.channels[channel]
	snapshot := make([]*Subscriber, 0, len(subs))
	for sub := range subs {
		snapshot = append(snapshot, sub)
	}
	b.mu.RUnlock()

	if len(snapshot) == 0 {
		return 0
	}

	msg := EncodePubSubMessage(channel, message)
	count := 0
	for _, sub := range snapshot {
		if sub.Send(msg) {
			count++
		}
	}
	return count
}
