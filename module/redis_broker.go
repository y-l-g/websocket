package websocket

import (
	"context"
	"crypto/tls"
	"fmt"
	"math"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const RedisChannelName = "frankenphp:cluster:broadcast"

type RedisBroker struct {
	client      *redis.Client
	logger      *zap.Logger
	appID       string
	channelName string
	scope       string
	queueSize   int
}

func NewRedisBroker(logger *zap.Logger, appID, addr, password string, db int, useTLS bool, queueSize ...int) *RedisBroker {
	if addr == "" {
		addr = "localhost:6379"
	}
	size := DefaultBrokerQueueSize
	if len(queueSize) > 0 && queueSize[0] > 0 {
		size = queueSize[0]
	}

	opts := &redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}

	if useTLS {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	rdb := redis.NewClient(opts)
	channelName := redisChannelName(appID)

	return &RedisBroker{
		client:      rdb,
		logger:      logger,
		appID:       appID,
		channelName: channelName,
		scope:       fmt.Sprintf("redis:%s:%d:%s", addr, db, channelName),
		queueSize:   size,
	}
}

func redisChannelName(appID string) string {
	return RedisChannelName + ":" + appID
}

func (r *RedisBroker) Publish(ctx context.Context, msg *BroadcastMessage) error {
	if msg.AppID == "" {
		copy := *msg
		copy.AppID = r.appID
		msg = &copy
	}

	data, err := SerializeBroadcast(msg)
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 0; attempt <= 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * 100 * time.Millisecond
			if backoff > 2*time.Second {
				backoff = 2 * time.Second
			}
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		err = r.client.Publish(ctx, r.channelName, data).Err()
		if err == nil {
			return nil
		}
		lastErr = err
	}

	return fmt.Errorf("redis publish failed after 4 attempts: %w", lastErr)
}

func (r *RedisBroker) Subscribe(ctx context.Context) (<-chan *BroadcastMessage, error) {
	out := make(chan *BroadcastMessage, r.queueSize)

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

			pubsub := r.client.Subscribe(ctx, r.channelName)
			if _, err := pubsub.Receive(ctx); err != nil {
				_ = pubsub.Close()

				sleepDuration := time.Duration(math.Pow(2, float64(attempt))) * time.Second
				if sleepDuration > 30*time.Second {
					sleepDuration = 30 * time.Second
				}

				r.logger.Error("Redis: connection failed, retrying...",
					zap.Error(err),
					zap.Duration("backoff", sleepDuration))

				timer := time.NewTimer(sleepDuration)
				select {
				case <-timer.C:
				case <-ctx.Done():
					timer.Stop()
					return
				}
				attempt++
				continue
			}

			// Connection established, reset attempts
			attempt = 0
			r.logger.Info("Redis: subscribed to broadcast channel", zap.String("channel", r.channelName))

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
					if msg.AppID != "" && msg.AppID != r.appID {
						continue
					}
					if msg.AppID == "" {
						msg.AppID = r.appID
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

func (r *RedisBroker) PublishScope() string {
	return r.scope
}
