/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package webui

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func spawnWorkers(wg *sync.WaitGroup, count int, fn func()) {
	wg.Add(count)

	for range count {
		go func() {
			defer wg.Done()
			fn()
		}()
	}
}

func TestSSEBroker_ConcurrentSafety(t *testing.T) {
	t.Parallel()

	// PROOF: SSE broker was suspected of having a race condition under
	// concurrent register/unregister/broadcast. This test proves it's safe.
	// Run with -race flag: go test -race ./pkg/webui/
	broker := NewSSEBroker()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go broker.Start(ctx)
	time.Sleep(10 * time.Millisecond)

	const goroutines = 20
	const iterations = 50

	var wg sync.WaitGroup

	spawnWorkers(&wg, goroutines, func() {
		for range iterations {
			client := &SSEClient{ID: generateClientID(), Messages: make(chan []byte, 10)}
			broker.register <- client
			time.Sleep(time.Microsecond)
			broker.unregister <- client
		}
	})

	spawnWorkers(&wg, goroutines, func() {
		for range iterations {
			broker.Broadcast([]byte("data: test\n\n"))
		}
	})

	spawnWorkers(&wg, goroutines, func() {
		for range iterations {
			_ = broker.ClientCount()
		}
	})

	wg.Wait()

	// After all goroutines complete, client count should be 0
	// (all registered clients were unregistered)
	// Give event loop time to process remaining messages
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, broker.ClientCount(),
		"All clients should be unregistered after concurrent operations")
}

func TestSSEBroker_BroadcastToMultipleClients(t *testing.T) {
	t.Parallel()

	broker := NewSSEBroker()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go broker.Start(ctx)
	time.Sleep(10 * time.Millisecond)

	// Register 3 clients
	clients := make([]*SSEClient, 3)
	for i := range clients {
		clients[i] = &SSEClient{
			ID:       generateClientID(),
			Messages: make(chan []byte, 10),
		}
		broker.register <- clients[i]
	}
	time.Sleep(10 * time.Millisecond)

	require.Equal(t, 3, broker.ClientCount())

	// Broadcast a message
	broker.Broadcast([]byte("data: hello\n\n"))
	time.Sleep(10 * time.Millisecond)

	// All clients should receive the message
	for i, client := range clients {
		select {
		case msg := <-client.Messages:
			assert.Equal(t, "data: hello\n\n", string(msg), "Client %d should receive broadcast", i)
		default:
			t.Errorf("Client %d did not receive broadcast message", i)
		}
	}
}

func TestSSEBroker_ShutdownClosesClients(t *testing.T) {
	t.Parallel()

	broker := NewSSEBroker()

	ctx, cancel := context.WithCancel(context.Background())

	go broker.Start(ctx)
	time.Sleep(10 * time.Millisecond)

	client := &SSEClient{
		ID:       generateClientID(),
		Messages: make(chan []byte, 10),
	}
	broker.register <- client
	time.Sleep(10 * time.Millisecond)

	require.Equal(t, 1, broker.ClientCount())

	// Cancel context to shut down broker
	cancel()
	time.Sleep(50 * time.Millisecond)

	// Client channel should be closed
	_, ok := <-client.Messages
	assert.False(t, ok, "Client Messages channel should be closed after broker shutdown")
	assert.Equal(t, 0, broker.ClientCount())
}
