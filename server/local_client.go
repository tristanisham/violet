package server

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/tristanisham/violet/protocol"
)

type localClient struct {
	engine *Engine
	id     string
	mu     sync.Mutex
	closed bool
}

var _ protocol.Client = (*localClient)(nil)

func NewLocalClient(engine *Engine) protocol.Client {
	return &localClient{engine: engine, id: uuid.NewString()}
}

func (c *localClient) Submit(ctx context.Context, req protocol.Request) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.engine == nil {
		return protocol.ErrDisconnected
	}
	return c.engine.Submit(ctx, c.id, req)
}

func (c *localClient) Subscribe(ctx context.Context) (<-chan protocol.Event, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.engine == nil {
		return nil, protocol.ErrDisconnected
	}
	return c.engine.Subscribe(ctx, c.id)
}

func (c *localClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		if c.engine != nil {
			c.engine.unsubscribe(c.id)
		}
	}
	return nil
}
