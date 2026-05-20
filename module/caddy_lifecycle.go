package websocket

import (
	frankenphpCaddy "github.com/dunglas/frankenphp/caddy"
	"go.uber.org/zap"
)

func (m *WebsocketModule) setupWorkers() error {
	m.workerHandle = frankenphpCaddy.RegisterWorkers(
		"frankenphp-websocket-auth-"+m.AppID,
		m.AuthScript,
		m.NumWorkers,
	)
	return nil
}

func (m *WebsocketModule) setupBroker() (Broker, error) {
	if m.RedisHost != "" {
		m.logger.Info("Using Redis Broker", zap.String("host", m.RedisHost), zap.Int("db", m.RedisDB), zap.Bool("tls", m.RedisTLS))
		return NewRedisBroker(m.logger, m.RedisHost, m.RedisPassword, m.RedisDB, m.RedisTLS, m.BrokerQueueSize), nil
	}
	m.logger.Info("Using Memory Broker")
	return NewMemoryBroker(m.logger, m.metrics, m.BrokerQueueSize), nil
}

func (m *WebsocketModule) Cleanup() error {
	m.cancel()
	if m.hub != nil {
		m.hub.Wait()
		UnregisterHub(m.AppID, m.hub)
	}
	if m.webhook != nil {
		m.webhook.Close()
	}
	return nil
}
