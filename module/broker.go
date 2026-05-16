package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	bus    chan *BroadcastMessage
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

func NewMemoryBroker(_ *zap.Logger, _ *Metrics) *MemoryBroker {
	ctx, cancel := context.WithCancel(context.Background())
	return &MemoryBroker{
		bus:    make(chan *BroadcastMessage),
		ctx:    ctx,
		cancel: cancel,
	}
}

func (b *MemoryBroker) Publish(ctx context.Context, msg *BroadcastMessage) error {
	select {
	case b.bus <- msg:
		return nil
	case <-b.ctx.Done():
		return errors.New("broker closed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *MemoryBroker) Subscribe(ctx context.Context) (<-chan *BroadcastMessage, error) {
	return b.bus, nil
}

func (b *MemoryBroker) Close() error {
	b.once.Do(func() {
		b.cancel()
	})
	return nil
}

func (b *MemoryBroker) PublishScope() string {
	return fmt.Sprintf("memory:%p", b)
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
