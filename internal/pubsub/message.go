// Package pubsub implements a Redis-compatible Pub/Sub broker.
// Messages are delivered in RESP v2 wire format using the same conventions
// as the existing protocol/serializer.go — string-based, no []byte.
package pubsub

import "fmt"

// EncodePubSubMessage returns a RESP-encoded push frame delivered to subscribers.
//
// Wire format:
//
//	*3\r\n
//	$7\r\nmessage\r\n
//	$<len>\r\n<channel>\r\n
//	$<len>\r\n<payload>\r\n
func EncodePubSubMessage(channel, payload string) string {
	return fmt.Sprintf(
		"*3\r\n$7\r\nmessage\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n",
		len(channel), channel, len(payload), payload,
	)
}

// EncodeSubscribeAck returns a RESP-encoded subscribe confirmation sent to
// the client immediately after each successful SUBSCRIBE channel registration.
//
// Wire format:
//
//	*3\r\n
//	$9\r\nsubscribe\r\n
//	$<len>\r\n<channel>\r\n
//	:<activeSubscriptions>\r\n
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
//	*3\r\n
//	$11\r\nunsubscribe\r\n
//	$<len>\r\n<channel>\r\n
//	:<remainingSubscriptions>\r\n
func EncodeUnsubscribeAck(channel string, remainingSubscriptions int) string {
	return fmt.Sprintf(
		"*3\r\n$11\r\nunsubscribe\r\n$%d\r\n%s\r\n:%d\r\n",
		len(channel), channel, remainingSubscriptions,
	)
}
