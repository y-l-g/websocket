package websocket

import (
	"context"
	"math"
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
	out := make(chan *BroadcastMessage, 256)

	go func() {
		defer close(out)

		attempt := 0

		for {
			// Check context before reconnecting
			select {
			case <-ctx.Done():
				return
			default:
			}

			pubsub := r.client.Subscribe(ctx, RedisChannelName)
			if _, err := pubsub.Receive(ctx); err != nil {
				_ = pubsub.Close()

				sleepDuration := time.Duration(math.Pow(2, float64(attempt))) * time.Second
				if sleepDuration > 30*time.Second {
					sleepDuration = 30 * time.Second
				}

				r.logger.Error("Redis: connection failed, retrying...",
					zap.Error(err),
					zap.Duration("backoff", sleepDuration))

				time.Sleep(sleepDuration)
				attempt++
				continue
			}

			// Connection established, reset attempts
			attempt = 0
			r.logger.Info("Redis: subscribed to broadcast channel")

			ch := pubsub.Channel()

			// Inner loop: Read messages
		readLoop:
			for {
				select {
				case redisMsg, ok := <-ch:
					if !ok {
						break readLoop
					}

					msg, err := DeserializeBroadcast([]byte(redisMsg.Payload))
					if err != nil {
						r.logger.Error("Redis: deserialize error", zap.Error(err))
						continue
					}

					select {
					case out <- msg:
					case <-ctx.Done():
						_ = pubsub.Close()
						return
					}
				case <-ctx.Done():
					_ = pubsub.Close()
					return
				}
			}

			_ = pubsub.Close()
			r.logger.Warn("Redis: connection lost, reconnecting")
		}
	}()

	return out, nil
}

func (r *RedisBroker) Close() error {
	return r.client.Close()
}
