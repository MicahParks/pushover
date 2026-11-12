package pushover

import (
	"context"
)

// Sender sends messages. *Client implements Sender. Accepting a Sender instead of a *Client makes it possible to
// disable notifications with NewNop or to substitute a fake in tests.
type Sender interface {
	Send(ctx context.Context, msg Message) (SendResponse, error)
}

var _ Sender = (*Client)(nil)

type nopSender struct{}

// NewNop returns a Sender that discards every message without validating or sending it. This is useful when
// notifications are disabled by configuration.
func NewNop() Sender {
	return nopSender{}
}

func (nopSender) Send(context.Context, Message) (SendResponse, error) {
	return SendResponse{}, nil
}
