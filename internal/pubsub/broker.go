package pubsub

import (
	"path/filepath"
	"sync"
)

// Broker manages the channel-to-subscriber registry and routes messages from
// publishers to all active subscribers on a channel.
//
// It supports both exact-channel subscriptions (SUBSCRIBE) and glob-pattern
// subscriptions (PSUBSCRIBE). When a message is published on a channel, the
// broker delivers it to:
//   - All subscribers of that exact channel.
//   - All pattern subscribers whose pattern matches the channel name.
//
// It is safe for concurrent use.
type Broker struct {
	mu       sync.RWMutex
	channels map[string]map[*Subscriber]struct{} // channel → subscribers
	patterns map[string]map[*Subscriber]struct{} // pattern → subscribers
}

// NewBroker creates an empty Broker.
func NewBroker() *Broker {
	return &Broker{
		channels: make(map[string]map[*Subscriber]struct{}),
		patterns: make(map[string]map[*Subscriber]struct{}),
	}
}

// Subscribe registers sub as a listener on the exact channel name.
// Idempotent: subscribing twice to the same channel is a no-op.
func (b *Broker) Subscribe(channel string, sub *Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.channels[channel] == nil {
		b.channels[channel] = make(map[*Subscriber]struct{})
	}
	b.channels[channel][sub] = struct{}{}
}

// Unsubscribe removes sub from channel. Empty channels are deleted.
func (b *Broker) Unsubscribe(channel string, sub *Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.removeFromMap(b.channels, channel, sub)
}

// UnsubscribeAll removes sub from every exact channel it is registered on.
// Returns the names of the channels it was removed from.
func (b *Broker) UnsubscribeAll(sub *Subscriber) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.removeSubFromAllChannels(b.channels, sub)
}

// PSubscribe registers sub as a listener for all channels matching pattern.
// pattern uses the same glob syntax as KEYS (*, ?, [ranges]).
// Idempotent: registering the same pattern twice is a no-op.
func (b *Broker) PSubscribe(pattern string, sub *Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.patterns[pattern] == nil {
		b.patterns[pattern] = make(map[*Subscriber]struct{})
	}
	b.patterns[pattern][sub] = struct{}{}
}

// PUnsubscribe removes sub from a specific pattern subscription.
func (b *Broker) PUnsubscribe(pattern string, sub *Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.removeFromMap(b.patterns, pattern, sub)
}

// PUnsubscribeAll removes sub from every pattern it is subscribed to.
// Returns the patterns it was removed from.
func (b *Broker) PUnsubscribeAll(sub *Subscriber) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.removeSubFromAllChannels(b.patterns, sub)
}

// Publish sends message to all current subscribers of channel (exact match)
// and to all pattern subscribers whose pattern matches channel.
// Returns the total number of subscribers that received the message.
//
// Non-blocking guarantee: the read lock is held only for snapshotting the
// subscriber sets. Actual sends happen after the lock is released, so a slow
// subscriber's full inbox never stalls the publisher or other subscribers.
func (b *Broker) Publish(channel, message string) int {
	b.mu.RLock()

	// Snapshot exact-channel subscribers.
	chanSubs := b.channels[channel]
	exactSnapshot := make([]*Subscriber, 0, len(chanSubs))
	for sub := range chanSubs {
		exactSnapshot = append(exactSnapshot, sub)
	}

	// Snapshot pattern subscribers whose pattern matches this channel.
	type patternMatch struct {
		pattern string
		sub     *Subscriber
	}
	var patternMatches []patternMatch
	for pat, subs := range b.patterns {
		if matched, _ := filepath.Match(pat, channel); matched {
			for sub := range subs {
				patternMatches = append(patternMatches, patternMatch{pat, sub})
			}
		}
	}

	b.mu.RUnlock()

	count := 0

	// Deliver to exact-channel subscribers.
	msg := EncodePubSubMessage(channel, message)
	for _, sub := range exactSnapshot {
		if sub.Send(msg) {
			count++
		}
	}

	// Deliver to pattern subscribers (with the pmessage frame type).
	for _, pm := range patternMatches {
		pmsg := EncodePMessage(pm.pattern, channel, message)
		if pm.sub.Send(pmsg) {
			count++
		}
	}

	return count
}

// removeFromMap removes sub from a channel/pattern map entry and cleans up
// empty entries. Must be called with b.mu held for writing.
func (b *Broker) removeFromMap(m map[string]map[*Subscriber]struct{}, key string, sub *Subscriber) {
	subs := m[key]
	if subs == nil {
		return
	}
	delete(subs, sub)
	if len(subs) == 0 {
		delete(m, key)
	}
}

// removeSubFromAllChannels removes sub from every entry in m and returns the
// list of keys it was removed from. Must be called with b.mu held for writing.
func (b *Broker) removeSubFromAllChannels(m map[string]map[*Subscriber]struct{}, sub *Subscriber) []string {
	var removed []string
	for key, subs := range m {
		if _, ok := subs[sub]; ok {
			delete(subs, sub)
			removed = append(removed, key)
			if len(subs) == 0 {
				delete(m, key)
			}
		}
	}
	return removed
}
