package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"go.uber.org/zap"
)

// Broker handles distributing messages to the Hub.
type Broker interface {
	Publish(ctx context.Context, msg *BroadcastMessage) error
	Subscribe(ctx context.Context) (<-chan *BroadcastMessage, error)
	Close() error
}

// MemoryBroker implements a simple in-process event bus.
type MemoryBroker struct {
	bus     chan *BroadcastMessage
	ctx     context.Context
	cancel  context.CancelFunc
	once    sync.Once
	logger  *zap.Logger
	metrics *Metrics
}

func NewMemoryBroker(logger *zap.Logger, metrics *Metrics) *MemoryBroker {
	ctx, cancel := context.WithCancel(context.Background())
	return &MemoryBroker{
		bus:     make(chan *BroadcastMessage, 256),
		ctx:     ctx,
		cancel:  cancel,
		logger:  logger,
		metrics: metrics,
	}
}

func (b *MemoryBroker) Publish(ctx context.Context, msg *BroadcastMessage) error {
	select {
	case b.bus <- msg:
		return nil
	case <-b.ctx.Done():
		return errors.New("broker closed")
	default:
		// Buffer full: Record metric and error
		if b.metrics != nil {
			b.metrics.BrokerDropped.Inc()
		}
		if b.logger != nil {
			// Rate limiting logic could be added here to prevent log spam
			b.logger.Warn("MemoryBroker: buffer full, dropping message",
				zap.String("channel", msg.Channel),
				zap.String("event", msg.Event))
		}
		return errors.New("memory broker buffer full")
	}
}

func (b *MemoryBroker) Subscribe(ctx context.Context) (<-chan *BroadcastMessage, error) {
	return b.bus, nil
}

func (b *MemoryBroker) Close() error {
	b.once.Do(func() {
		b.cancel()
		close(b.bus)
	})
	return nil
}

// Helper for JSON serialization used by RedisBroker later
func SerializeBroadcast(msg *BroadcastMessage) ([]byte, error) {
	return json.Marshal(msg)
}

func DeserializeBroadcast(data []byte) (*BroadcastMessage, error) {
	var msg BroadcastMessage
	err := json.Unmarshal(data, &msg)
	return &msg, err
}
