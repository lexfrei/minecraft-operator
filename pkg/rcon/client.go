// Package rcon provides RCON client for graceful Minecraft server shutdown.
package rcon

import (
	"context"
	"fmt"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/gorcon/rcon"
)

// Client is an interface for RCON client operations.
type Client interface {
	Connect(ctx context.Context) error
	GracefulShutdown(ctx context.Context, warnings []string, warningInterval time.Duration) error
	Close() error
}

// rconConn is an internal interface for dependency injection in tests.
// It abstracts the gorcon/rcon.Conn type.
type rconConn interface {
	Execute(cmd string) (string, error)
	Close() error
}

// RCONClient wraps gorcon/rcon for Minecraft server communication.
type RCONClient struct {
	conn     rconConn
	host     string
	port     int
	password string
}

// NewRCONClient creates a new RCON client.
// The client is not connected until Connect() is called.
func NewRCONClient(host string, port int, password string) (*RCONClient, error) {
	if host == "" {
		return nil, errors.New("host cannot be empty")
	}
	if port <= 0 || port > 65535 {
		return nil, errors.Newf("invalid port: %d", port)
	}
	if password == "" {
		return nil, errors.New("password cannot be empty")
	}

	return &RCONClient{
		host:     host,
		port:     port,
		password: password,
	}, nil
}

// Connect establishes connection to the RCON server.
func (c *RCONClient) Connect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	address := fmt.Sprintf("%s:%d", c.host, c.port)

	conn, err := rcon.Dial(address, c.password)
	if err != nil {
		return errors.Wrapf(err, "failed to connect to RCON at %s", address)
	}

	c.conn = conn
	return nil
}

// SendCommand sends a command to the server and returns the response.
func (c *RCONClient) SendCommand(ctx context.Context, command string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if c.conn == nil {
		return "", errors.New("not connected to RCON server")
	}

	response, err := c.conn.Execute(command)
	if err != nil {
		return "", errors.Wrapf(err, "failed to execute command: %s", command)
	}

	return response, nil
}

// GracefulShutdown performs a graceful shutdown of the Minecraft server.
// It sends warning messages to players before shutting down.
func (c *RCONClient) GracefulShutdown(
	ctx context.Context,
	warnings []string,
	warningInterval time.Duration,
) error {
	if c.conn == nil {
		return errors.New("not connected to RCON server")
	}

	// Send warning messages with intervals
	for i, warning := range warnings {
		select {
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), "shutdown cancelled")
		default:
		}

		// Send warning to all players
		cmd := fmt.Sprintf("say %s", warning)
		_, err := c.SendCommand(ctx, cmd)
		if err != nil {
			return errors.Wrap(err, "failed to send warning")
		}

		// Wait before next warning (except for the last one)
		if i < len(warnings)-1 {
			select {
			case <-ctx.Done():
				return errors.Wrap(ctx.Err(), "shutdown cancelled")
			case <-time.After(warningInterval):
			}
		}
	}

	// Save all chunks and player data
	_, err := c.SendCommand(ctx, "save-all")
	if err != nil {
		return errors.Wrap(err, "failed to save world")
	}

	// Wait a moment for save to complete, but respect context cancellation
	select {
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "shutdown cancelled during save wait")
	case <-time.After(2 * time.Second):
	}

	// Send stop command
	_, err = c.SendCommand(ctx, "stop")
	if err != nil {
		return errors.Wrap(err, "failed to stop server")
	}

	return nil
}

// Close closes the RCON connection.
func (c *RCONClient) Close() error {
	if c.conn == nil {
		return nil
	}

	err := c.conn.Close()
	c.conn = nil
	if err != nil {
		return errors.Wrap(err, "failed to close RCON connection")
	}

	return nil
}

// IsConnected returns true if the client is connected.
func (c *RCONClient) IsConnected() bool {
	return c.conn != nil
}
