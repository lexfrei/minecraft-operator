/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package rcon

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock implementation ---

// mockRCONConn implements rconConn interface for testing.
type mockRCONConn struct {
	mu             sync.Mutex
	executeCalls   []string
	executeResults map[string]string
	executeError   error
	executeFunc    func(cmd string) (string, error) // Optional custom handler
	closeError     error
	closeCalled    bool
}

func newMockRCONConn() *mockRCONConn {
	return &mockRCONConn{
		executeResults: make(map[string]string),
	}
}

func (m *mockRCONConn) Execute(cmd string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.executeCalls = append(m.executeCalls, cmd)

	// Use custom function if provided
	if m.executeFunc != nil {
		return m.executeFunc(cmd)
	}

	if m.executeError != nil {
		return "", m.executeError
	}

	if result, ok := m.executeResults[cmd]; ok {
		return result, nil
	}

	return "OK", nil
}

func (m *mockRCONConn) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closeCalled = true
	return m.closeError
}

func (m *mockRCONConn) getExecuteCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	calls := make([]string, len(m.executeCalls))
	copy(calls, m.executeCalls)

	return calls
}

// --- NewRCONClient tests ---

func TestNewRCONClient_ValidParams(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")

	require.NoError(t, err)
	assert.NotNil(t, client)
	assert.Equal(t, "localhost", client.host)
	assert.Equal(t, 25575, client.port)
	assert.Equal(t, "secret", client.password)
}

func TestNewRCONClient_EmptyHost(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("", 25575, "secret")

	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "host cannot be empty")
}

func TestNewRCONClient_InvalidPort_Zero(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 0, "secret")

	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "invalid port")
}

func TestNewRCONClient_InvalidPort_Negative(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", -1, "secret")

	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "invalid port")
}

func TestNewRCONClient_InvalidPort_TooHigh(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 65536, "secret")

	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "invalid port")
}

func TestNewRCONClient_ValidPort_MaxValue(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 65535, "secret")

	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNewRCONClient_EmptyPassword(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "")

	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "password cannot be empty")
}

// --- Connect context tests ---

func TestRCONClient_Connect_ShouldRespectCancelledContext(t *testing.T) {
	t.Parallel()

	// BUG: Connect accepts ctx parameter but rcon.Dial() ignores it entirely.
	// A cancelled context should prevent the dial attempt.
	client, err := NewRCONClient("127.0.0.1", 19132, "testpass")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err = client.Connect(ctx)
	// Should return context.Canceled, not "connection refused"
	require.ErrorIs(t, err, context.Canceled,
		"Connect should respect cancelled context and return context.Canceled, "+
			"but currently ignores ctx and tries to dial regardless")
}

// --- IsConnected tests ---

func TestRCONClient_IsConnected_NotConnected(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	assert.False(t, client.IsConnected())
}

func TestRCONClient_IsConnected_Connected(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	// Inject mock connection
	client.conn = newMockRCONConn()

	assert.True(t, client.IsConnected())
}

// --- Close tests ---

func TestRCONClient_Close_NotConnected(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	// Should be safe to call when not connected
	err = client.Close()

	require.NoError(t, err)
}

func TestRCONClient_Close_Connected(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	mock := newMockRCONConn()
	client.conn = mock

	err = client.Close()

	require.NoError(t, err)
	assert.True(t, mock.closeCalled)
	assert.Nil(t, client.conn)
}

func TestRCONClient_Close_Error(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	mock := newMockRCONConn()
	mock.closeError = errors.New("connection reset")
	client.conn = mock

	err = client.Close()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to close RCON connection")
}

func TestRCONClient_Close_Idempotent(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	mock := newMockRCONConn()
	client.conn = mock

	// First close
	err = client.Close()
	require.NoError(t, err)

	// Second close should also succeed
	err = client.Close()
	require.NoError(t, err)
}

// --- SendCommand context tests ---

func TestRCONClient_SendCommand_ShouldRespectCancelledContext(t *testing.T) {
	t.Parallel()

	// BUG: SendCommand accepts ctx parameter but conn.Execute() ignores it.
	// A cancelled context should prevent the command from executing.
	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	mock := newMockRCONConn()
	mock.executeResults["list"] = "There are 0 players online"
	client.conn = mock

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = client.SendCommand(ctx, "list")
	// Should return context.Canceled, not execute the command
	require.ErrorIs(t, err, context.Canceled,
		"SendCommand should respect cancelled context and return context.Canceled, "+
			"but currently ignores ctx and executes command regardless")
}

// --- SendCommand tests ---

func TestRCONClient_SendCommand_NotConnected(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	response, err := client.SendCommand(context.Background(), "list")

	require.Error(t, err)
	assert.Empty(t, response)
	assert.Contains(t, err.Error(), "not connected")
}

func TestRCONClient_SendCommand_Success(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	mock := newMockRCONConn()
	mock.executeResults["list"] = "There are 5 players online"
	client.conn = mock

	response, err := client.SendCommand(context.Background(), "list")

	require.NoError(t, err)
	assert.Equal(t, "There are 5 players online", response)
	assert.Equal(t, []string{"list"}, mock.getExecuteCalls())
}

func TestRCONClient_SendCommand_Error(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	mock := newMockRCONConn()
	mock.executeError = errors.New("connection timeout")
	client.conn = mock

	response, err := client.SendCommand(context.Background(), "list")

	require.Error(t, err)
	assert.Empty(t, response)
	assert.Contains(t, err.Error(), "failed to execute command")
}

// --- GracefulShutdown tests ---

func TestRCONClient_GracefulShutdown_NotConnected(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	err = client.GracefulShutdown(context.Background(), []string{"Shutdown"}, time.Second)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestRCONClient_GracefulShutdown_SendsWarningsInOrder(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	mock := newMockRCONConn()
	client.conn = mock

	warnings := []string{
		"Server restarting in 30 seconds",
		"Server restarting in 10 seconds",
		"Server restarting NOW",
	}

	// Use very short interval for test speed
	err = client.GracefulShutdown(context.Background(), warnings, 1*time.Millisecond)

	require.NoError(t, err)

	calls := mock.getExecuteCalls()
	// Should have: 3 warnings + save-all + stop = 5 commands
	require.Len(t, calls, 5)

	// Check warnings are sent with "say" prefix
	assert.Equal(t, "say Server restarting in 30 seconds", calls[0])
	assert.Equal(t, "say Server restarting in 10 seconds", calls[1])
	assert.Equal(t, "say Server restarting NOW", calls[2])
}

func TestRCONClient_GracefulShutdown_SendsSaveAll(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	mock := newMockRCONConn()
	client.conn = mock

	err = client.GracefulShutdown(context.Background(), []string{"Bye"}, 1*time.Millisecond)

	require.NoError(t, err)

	calls := mock.getExecuteCalls()
	// Command 2 should be save-all (after 1 warning)
	assert.Contains(t, calls, "save-all")
}

func TestRCONClient_GracefulShutdown_SendsStop(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	mock := newMockRCONConn()
	client.conn = mock

	err = client.GracefulShutdown(context.Background(), []string{"Bye"}, 1*time.Millisecond)

	require.NoError(t, err)

	calls := mock.getExecuteCalls()
	// Last command should be stop
	assert.Equal(t, "stop", calls[len(calls)-1])
}

func TestRCONClient_GracefulShutdown_NoWarnings(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	mock := newMockRCONConn()
	client.conn = mock

	// Empty warnings slice
	err = client.GracefulShutdown(context.Background(), []string{}, 1*time.Millisecond)

	require.NoError(t, err)

	calls := mock.getExecuteCalls()
	// Should only have save-all and stop
	require.Len(t, calls, 2)
	assert.Equal(t, "save-all", calls[0])
	assert.Equal(t, "stop", calls[1])
}

func TestRCONClient_GracefulShutdown_ContextCancellation_BeforeWarnings(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	mock := newMockRCONConn()
	client.conn = mock

	// Cancel context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = client.GracefulShutdown(ctx, []string{"Warning1", "Warning2"}, time.Second)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "shutdown cancelled")
}

func TestRCONClient_GracefulShutdown_ContextCancellation_DuringInterval(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	mock := newMockRCONConn()
	client.conn = mock

	// Cancel after short delay
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	// Use long interval to ensure cancellation during wait
	err = client.GracefulShutdown(ctx, []string{"Warning1", "Warning2"}, time.Second)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "shutdown cancelled")
}

func TestRCONClient_GracefulShutdown_WarningError(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	mock := newMockRCONConn()
	mock.executeError = errors.New("connection lost")
	client.conn = mock

	err = client.GracefulShutdown(context.Background(), []string{"Warning"}, 1*time.Millisecond)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send warning")
}

func TestRCONClient_GracefulShutdown_SaveAllError(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	mock := newMockRCONConn()
	// First call (say warning) succeeds, second call (save-all) fails
	callCount := 0
	mock.executeFunc = func(cmd string) (string, error) {
		callCount++
		if callCount > 1 { // After first warning
			return "", errors.New("disk full")
		}

		return "OK", nil
	}
	client.conn = mock

	err = client.GracefulShutdown(context.Background(), []string{"Warning"}, 1*time.Millisecond)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save world")
}

func TestRCONClient_GracefulShutdown_StopError(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	mock := newMockRCONConn()
	// First two calls succeed (say + save-all), stop fails
	callCount := 0
	mock.executeFunc = func(cmd string) (string, error) {
		callCount++
		if callCount > 2 { // After warning and save-all
			return "", errors.New("permission denied")
		}

		return "OK", nil
	}
	client.conn = mock

	err = client.GracefulShutdown(context.Background(), []string{"Warning"}, 1*time.Millisecond)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stop server")
}

// --- Context cancellation during save wait ---

func TestRCONClient_GracefulShutdown_ContextCancellation_DuringSaveWait(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	mock := newMockRCONConn()
	client.conn = mock

	// Use a context that expires after 100ms — well before the 2s sleep after save-all
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()

	// No warnings: save-all → sleep(2s) → stop
	// BUG: time.Sleep(2 * time.Second) on line 126 does NOT respect context.
	// With 100ms timeout, save-all succeeds, then sleep blocks for 2 full seconds
	// instead of returning immediately when context expires.
	err = client.GracefulShutdown(ctx, []string{}, time.Millisecond)
	elapsed := time.Since(start)

	// If context is properly respected during the save wait,
	// the function should return within ~200ms (context timeout + some margin).
	// BUG: Currently takes ~2s because time.Sleep ignores context.
	assert.Less(t, elapsed, 500*time.Millisecond,
		"GracefulShutdown should respect context during save wait, not block for 2 seconds")

	// Should return an error (either context cancelled or shutdown cancelled)
	require.Error(t, err, "Should return error when context expires during save wait")
}

// --- Concurrency tests ---

func TestRCONClient_SendCommand_Concurrent(t *testing.T) {
	t.Parallel()

	client, err := NewRCONClient("localhost", 25575, "secret")
	require.NoError(t, err)

	mock := newMockRCONConn()
	client.conn = mock

	var waitGroup sync.WaitGroup
	numGoroutines := 10

	for range numGoroutines {

		waitGroup.Go(func() {

			_, err := client.SendCommand(context.Background(), "list")
			assert.NoError(t, err)
		})
	}

	waitGroup.Wait()

	// All calls should have been recorded
	assert.Len(t, mock.getExecuteCalls(), numGoroutines)
}
