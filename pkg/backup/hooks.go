// Package backup provides VolumeSnapshot backup functionality with RCON hooks.
package backup

import (
	"context"
	"time"

	"github.com/cockroachdb/errors"
)

// SaveFlushDelay is the time to wait after save-all for data to flush to disk.
const SaveFlushDelay = 2 * time.Second

// RCONCommander is the interface for sending RCON commands.
// This is a subset of the full RCON client interface, used by backup hooks.
type RCONCommander interface {
	SendCommand(ctx context.Context, command string) (string, error)
}

// PreSnapshotHook prepares the Minecraft server for a consistent snapshot.
// It sends save-all to flush all data to disk, waits for the flush,
// then sends save-off to disable auto-save during the snapshot.
func PreSnapshotHook(ctx context.Context, rcon RCONCommander) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Flush all chunks and player data to disk
	if _, err := rcon.SendCommand(ctx, "save-all"); err != nil {
		return errors.Wrap(err, "failed to execute save-all")
	}

	// Wait for save to complete before disabling auto-save
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(SaveFlushDelay):
	}

	// Disable auto-save to prevent writes during snapshot
	if _, err := rcon.SendCommand(ctx, "save-off"); err != nil {
		return errors.Wrap(err, "failed to execute save-off")
	}

	return nil
}

// saveOnTimeout is the maximum time to wait for the save-on command.
const saveOnTimeout = 10 * time.Second

// PostSnapshotHook re-enables auto-save after a snapshot is taken.
// This function always attempts to send save-on, even if the parent
// context is cancelled. Leaving auto-save disabled would cause data loss.
func PostSnapshotHook(_ context.Context, rcon RCONCommander) error {
	saveOnCtx, cancel := context.WithTimeout(context.Background(), saveOnTimeout)
	defer cancel()

	if _, err := rcon.SendCommand(saveOnCtx, "save-on"); err != nil {
		return errors.Wrap(err, "failed to execute save-on")
	}

	return nil
}
