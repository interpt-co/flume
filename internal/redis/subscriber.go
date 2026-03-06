package redis

import (
	"context"
	"encoding/json"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/interpt-co/flume/internal/models"
)

// Subscriber listens to Redis pub/sub for live message delivery.
type Subscriber struct {
	client *Client
}

// NewSubscriber creates a new Subscriber.
func NewSubscriber(client *Client) *Subscriber {
	return &Subscriber{client: client}
}

// Subscribe returns a channel that receives message batches for the given pattern.
// The channel is closed when the context is cancelled. On reconnection, recent
// messages are backfilled from the sorted set so no gap appears.
func (s *Subscriber) Subscribe(ctx context.Context, pattern string) <-chan []models.LogMessage {
	ch := make(chan []models.LogMessage, 64)
	go s.subscribeLoop(ctx, pattern, ch)
	return ch
}

func (s *Subscriber) subscribeLoop(ctx context.Context, pattern string, out chan<- []models.LogMessage) {
	defer close(out)

	channel := s.client.channelKey(pattern)
	backoff := 100 * time.Millisecond
	maxBackoff := 5 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}

		pubsub := s.client.rdb.Subscribe(ctx, channel)
		// Wait for subscription confirmation.
		_, err := pubsub.Receive(ctx)
		if err != nil {
			log.WithError(err).WithField("pattern", pattern).Warn("redis subscribe failed, retrying")
			pubsub.Close()
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				backoff = min(backoff*2, maxBackoff)
				continue
			}
		}
		backoff = 100 * time.Millisecond

		// Backfill recent messages from sorted set on (re)connection.
		s.backfill(ctx, pattern, out)

		// Read from pub/sub channel.
		msgCh := pubsub.Channel()
		disconnected := false
		for !disconnected {
			select {
			case <-ctx.Done():
				pubsub.Close()
				return
			case redisMsg, ok := <-msgCh:
				if !ok {
					disconnected = true
					break
				}
				msgs, err := decodePubSubPayload(redisMsg.Payload)
				if err != nil {
					log.WithError(err).Warn("redis pubsub decode error")
					continue
				}
				select {
				case out <- msgs:
				case <-ctx.Done():
					pubsub.Close()
					return
				}
			}
		}
		pubsub.Close()
		log.WithField("pattern", pattern).Info("redis pubsub disconnected, reconnecting")
	}
}

// backfill sends the most recent messages from the sorted set so the subscriber
// doesn't miss anything between reconnections.
func (s *Subscriber) backfill(ctx context.Context, pattern string, out chan<- []models.LogMessage) {
	key := s.client.msgsKey(pattern)
	// Get latest 100 messages for backfill.
	vals, err := s.client.rdb.ZRevRange(ctx, key, 0, 99).Result()
	if err != nil || len(vals) == 0 {
		return
	}
	// Reverse to oldest-first order.
	for i, j := 0, len(vals)-1; i < j; i, j = i+1, j-1 {
		vals[i], vals[j] = vals[j], vals[i]
	}
	msgs, err := deserializeMessages(vals)
	if err != nil {
		log.WithError(err).Warn("redis backfill decode error")
		return
	}
	select {
	case out <- msgs:
	case <-ctx.Done():
	}
}

func decodePubSubPayload(payload string) ([]models.LogMessage, error) {
	var rawMsgs []json.RawMessage
	if err := json.Unmarshal([]byte(payload), &rawMsgs); err != nil {
		return nil, err
	}
	msgs := make([]models.LogMessage, 0, len(rawMsgs))
	for _, raw := range rawMsgs {
		var msg models.LogMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			return nil, err
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}
