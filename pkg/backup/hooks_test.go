package backup_test

import (
	"context"
	"testing"
	"time"

	"github.com/lexfrei/minecraft-operator/pkg/backup"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRCONClient tracks commands sent via SendCommand for testing hooks.
type mockRCONClient struct {
	commands  []string
	responses map[string]string
	errors    map[string]error
}

func newMockRCONClient() *mockRCONClient {
	return &mockRCONClient{
		commands:  make([]string, 0),
		responses: make(map[string]string),
		errors:    make(map[string]error),
	}
}

func (m *mockRCONClient) SendCommand(_ context.Context, command string) (string, error) {
	m.commands = append(m.commands, command)

	if err, ok := m.errors[command]; ok {
		return "", err
	}

	if resp, ok := m.responses[command]; ok {
		return resp, nil
	}

	return "", nil
}

func TestPreSnapshotHook(t *testing.T) {
	t.Run("sends save-all then save-off in correct order", func(t *testing.T) {
		mock := newMockRCONClient()
		ctx := context.Background()

		err := backup.PreSnapshotHook(ctx, mock)
		require.NoError(t, err)

		require.Len(t, mock.commands, 2)
		assert.Equal(t, "save-all", mock.commands[0])
		assert.Equal(t, "save-off", mock.commands[1])
	})

	t.Run("returns error when save-all fails", func(t *testing.T) {
		mock := newMockRCONClient()
		mock.errors["save-all"] = assert.AnError
		ctx := context.Background()

		err := backup.PreSnapshotHook(ctx, mock)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "save-all")
		assert.Len(t, mock.commands, 1, "should not proceed to save-off after save-all failure")
	})

	t.Run("returns error when save-off fails", func(t *testing.T) {
		mock := newMockRCONClient()
		mock.errors["save-off"] = assert.AnError
		ctx := context.Background()

		err := backup.PreSnapshotHook(ctx, mock)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "save-off")
		assert.Len(t, mock.commands, 2, "save-all should have been called before save-off failed")
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		mock := newMockRCONClient()
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err := backup.PreSnapshotHook(ctx, mock)
		require.Error(t, err)
	})
}

func TestPostSnapshotHook(t *testing.T) {
	t.Run("sends save-on", func(t *testing.T) {
		mock := newMockRCONClient()
		ctx := context.Background()

		err := backup.PostSnapshotHook(ctx, mock)
		require.NoError(t, err)

		require.Len(t, mock.commands, 1)
		assert.Equal(t, "save-on", mock.commands[0])
	})

	t.Run("returns error when save-on fails", func(t *testing.T) {
		mock := newMockRCONClient()
		mock.errors["save-on"] = assert.AnError
		ctx := context.Background()

		err := backup.PostSnapshotHook(ctx, mock)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "save-on")
	})

	t.Run("always sends save-on even when context is cancelled", func(t *testing.T) {
		mock := newMockRCONClient()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := backup.PostSnapshotHook(ctx, mock)
		require.NoError(t, err, "PostSnapshotHook must succeed to re-enable auto-save")

		require.Len(t, mock.commands, 1, "save-on must be sent even with cancelled context")
		assert.Equal(t, "save-on", mock.commands[0])
	})
}

func TestPostSnapshotHookTimeout(t *testing.T) {
	t.Run("returns error when save-on exceeds timeout", func(t *testing.T) {
		// The mock blocks on save-on longer than the 10s saveOnTimeout.
		// PostSnapshotHook should return a context deadline exceeded error.
		mock := &blockingRCONClient{blockDuration: 15 * time.Second}

		start := time.Now()
		err := backup.PostSnapshotHook(context.Background(), mock)
		elapsed := time.Since(start)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "save-on")
		// Should have returned within ~10s (the saveOnTimeout), not 15s.
		assert.Less(t, elapsed, 12*time.Second,
			"PostSnapshotHook should timeout within saveOnTimeout, not block indefinitely")
	})
}

// blockingRCONClient blocks SendCommand for a configured duration, respecting context.
type blockingRCONClient struct {
	blockDuration time.Duration
}

func (b *blockingRCONClient) SendCommand(ctx context.Context, _ string) (string, error) {
	select {
	case <-time.After(b.blockDuration):
		return "", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestPreSnapshotHookWithSaveWait(t *testing.T) {
	t.Run("waits for save to flush before disabling autosave", func(t *testing.T) {
		mock := newMockRCONClient()
		ctx := context.Background()

		start := time.Now()
		err := backup.PreSnapshotHook(ctx, mock)
		elapsed := time.Since(start)

		require.NoError(t, err)
		// PreSnapshotHook should include a brief wait after save-all
		// to allow the server to flush data to disk.
		// The minimum wait is backup.SaveFlushDelay (2 seconds).
		assert.GreaterOrEqual(t, elapsed, backup.SaveFlushDelay,
			"should wait at least SaveFlushDelay after save-all")
	})
}
