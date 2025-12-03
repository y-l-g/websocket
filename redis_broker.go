package websocket

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const RedisChannelName = "frankenphp:cluster:broadcast"

type RedisBroker struct {
	client *redis.Client
	logger *zap.Logger
}

func NewRedisBroker(logger *zap.Logger, addr string) *RedisBroker {
	if addr == "" {
		addr = "localhost:6379"
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	return &RedisBroker{
		client: rdb,
		logger: logger,
	}
}

func (r *RedisBroker) Publish(ctx context.Context, msg *BroadcastMessage) error {
	data, err := SerializeBroadcast(msg)
	if err != nil {
		return err
	}
	return r.client.Publish(ctx, RedisChannelName, data).Err()
}

func (r *RedisBroker) Subscribe(ctx context.Context) (<-chan *BroadcastMessage, error) {
	// Subscribe to Redis
	pubsub := r.client.Subscribe(ctx, RedisChannelName)

	// Wait for confirmation that we are subscribed
	if _, err := pubsub.Receive(ctx); err != nil {
		return nil, err
	}

	out := make(chan *BroadcastMessage, 256)

	// Pump Redis messages into the channel
	go func() {
		defer close(out)
		defer pubsub.Close()

		ch := pubsub.Channel()
		for {
			select {
			case redisMsg, ok := <-ch:
				if !ok {
					return
				}

				msg, err := DeserializeBroadcast([]byte(redisMsg.Payload))
				if err != nil {
					r.logger.Error("Redis: deserialize error", zap.Error(err))
					continue
				}

				select {
				case out <- msg:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, nil
}

func (r *RedisBroker) Close() error {
	return r.client.Close()
}
