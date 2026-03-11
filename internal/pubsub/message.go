// Package pubsub implements a Redis-compatible Pub/Sub broker.
// Messages are delivered in RESP v2 wire format using the same conventions
// as the existing protocol/serializer.go — string-based, no []byte.
package pubsub

import "fmt"

// EncodePubSubMessage returns a RESP-encoded push frame delivered to exact-
// channel subscribers (SUBSCRIBE).
//
// Wire format:
//
//	*3\r\n$7\r\nmessage\r\n$<len>\r\n<channel>\r\n$<len>\r\n<payload>\r\n
func EncodePubSubMessage(channel, payload string) string {
	return fmt.Sprintf(
		"*3\r\n$7\r\nmessage\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n",
		len(channel), channel, len(payload), payload,
	)
}

// EncodePMessage returns a RESP-encoded push frame delivered to pattern
// subscribers (PSUBSCRIBE). It carries an extra field — the matched pattern —
// so the client can determine which PSUBSCRIBE triggered the delivery.
//
// Wire format:
//
//	*4\r\n$8\r\npmessage\r\n$<len>\r\n<pattern>\r\n$<len>\r\n<channel>\r\n$<len>\r\n<payload>\r\n
func EncodePMessage(pattern, channel, payload string) string {
	return fmt.Sprintf(
		"*4\r\n$8\r\npmessage\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n",
		len(pattern), pattern, len(channel), channel, len(payload), payload,
	)
}

// EncodeSubscribeAck returns a RESP-encoded subscribe confirmation sent to
// the client immediately after each successful SUBSCRIBE registration.
//
// Wire format:
//
//	*3\r\n$9\r\nsubscribe\r\n$<len>\r\n<channel>\r\n:<activeSubscriptions>\r\n
func EncodeSubscribeAck(channel string, activeSubscriptions int) string {
	return fmt.Sprintf(
		"*3\r\n$9\r\nsubscribe\r\n$%d\r\n%s\r\n:%d\r\n",
		len(channel), channel, activeSubscriptions,
	)
}

// EncodeUnsubscribeAck returns a RESP-encoded unsubscribe confirmation sent
// to the client after each channel is removed.
//
// Wire format:
//
//	*3\r\n$11\r\nunsubscribe\r\n$<len>\r\n<channel>\r\n:<remainingSubscriptions>\r\n
func EncodeUnsubscribeAck(channel string, remainingSubscriptions int) string {
	return fmt.Sprintf(
		"*3\r\n$11\r\nunsubscribe\r\n$%d\r\n%s\r\n:%d\r\n",
		len(channel), channel, remainingSubscriptions,
	)
}

// EncodePSubscribeAck returns a RESP-encoded psubscribe confirmation.
//
// Wire format:
//
//	*3\r\n$10\r\npsubscribe\r\n$<len>\r\n<pattern>\r\n:<activeSubscriptions>\r\n
func EncodePSubscribeAck(pattern string, activeSubscriptions int) string {
	return fmt.Sprintf(
		"*3\r\n$10\r\npsubscribe\r\n$%d\r\n%s\r\n:%d\r\n",
		len(pattern), pattern, activeSubscriptions,
	)
}

// EncodePUnsubscribeAck returns a RESP-encoded punsubscribe confirmation.
//
// Wire format:
//
//	*3\r\n$12\r\npunsubscribe\r\n$<len>\r\n<pattern>\r\n:<remainingSubscriptions>\r\n
func EncodePUnsubscribeAck(pattern string, remainingSubscriptions int) string {
	return fmt.Sprintf(
		"*3\r\n$12\r\npunsubscribe\r\n$%d\r\n%s\r\n:%d\r\n",
		len(pattern), pattern, remainingSubscriptions,
	)
}
