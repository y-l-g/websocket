package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// Broker handles distributing messages to the Hub.
// In a cluster, it handles inter-node communication.
type Broker interface {
	// Publish sends a message to the broadcast stream
	Publish(ctx context.Context, msg *BroadcastMessage) error

	// Subscribe returns a read-only channel of messages from the broadcast stream
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

func NewMemoryBroker() *MemoryBroker {
	ctx, cancel := context.WithCancel(context.Background())
	return &MemoryBroker{
		bus:    make(chan *BroadcastMessage, 256), // Buffer to prevent blocking
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
	default:
		// If buffer is full, we drop the message to prevent deadlocks in the caller
		return errors.New("memory broker buffer full")
	}
}

func (b *MemoryBroker) Subscribe(ctx context.Context) (<-chan *BroadcastMessage, error) {
	// For MemoryBroker, we just return the shared channel.
	// Note: In a real pub/sub, we might need fan-out, but here the Hub is the only consumer.
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
