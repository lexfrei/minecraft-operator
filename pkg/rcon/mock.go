/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rcon

import (
	"context"
	"sync"
	"time"
)

// MockClient is a mock RCON client for testing.
type MockClient struct {
	mu sync.Mutex

	// Call tracking
	ConnectCalled          bool
	SendCommandCalled      bool
	GracefulShutdownCalled bool
	CloseCalled            bool

	// Commands received via SendCommand
	SentCommands []string

	// Commands received (GracefulShutdown simulation)
	Commands []string

	// Warnings received during graceful shutdown
	Warnings []string

	// Behavior control
	ConnectError          error
	SendCommandError      error
	SendCommandResponses  map[string]string
	GracefulShutdownError error
	ConnectDelay          time.Duration
	ShutdownDelay         time.Duration
}

// NewMockClient creates a new mock RCON client.
func NewMockClient() *MockClient {
	return &MockClient{
		SentCommands:         make([]string, 0),
		Commands:             make([]string, 0),
		Warnings:             make([]string, 0),
		SendCommandResponses: make(map[string]string),
	}
}

// Connect simulates connecting to the RCON server.
func (m *MockClient) Connect(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	m.ConnectCalled = true

	if m.ConnectDelay > 0 {
		select {
		case <-time.After(m.ConnectDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return m.ConnectError
}

// SendCommand simulates sending a command to the server.
func (m *MockClient) SendCommand(ctx context.Context, command string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	m.SendCommandCalled = true
	m.SentCommands = append(m.SentCommands, command)

	if m.SendCommandError != nil {
		return "", m.SendCommandError
	}

	if resp, ok := m.SendCommandResponses[command]; ok {
		return resp, nil
	}

	return "", nil
}

// GracefulShutdown simulates a graceful server shutdown with warnings.
func (m *MockClient) GracefulShutdown(ctx context.Context, warnings []string, warningInterval time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.GracefulShutdownCalled = true
	m.Warnings = append(m.Warnings, warnings...)

	// Simulate sending commands
	for _, warning := range warnings {
		m.Commands = append(m.Commands, "say "+warning)
	}
	m.Commands = append(m.Commands, "save-all")
	m.Commands = append(m.Commands, "stop")

	if m.ShutdownDelay > 0 {
		select {
		case <-time.After(m.ShutdownDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return m.GracefulShutdownError
}

// Close simulates closing the RCON connection.
func (m *MockClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.CloseCalled = true
	return nil
}

// Reset clears all tracking data for reuse.
func (m *MockClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ConnectCalled = false
	m.SendCommandCalled = false
	m.GracefulShutdownCalled = false
	m.CloseCalled = false
	m.SentCommands = make([]string, 0)
	m.Commands = make([]string, 0)
	m.Warnings = make([]string, 0)
	m.ConnectError = nil
	m.SendCommandError = nil
	m.SendCommandResponses = make(map[string]string)
	m.GracefulShutdownError = nil
	m.ConnectDelay = 0
	m.ShutdownDelay = 0
}

// GetCommands returns a copy of recorded commands (thread-safe).
func (m *MockClient) GetCommands() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	commands := make([]string, len(m.Commands))
	copy(commands, m.Commands)
	return commands
}

// GetSentCommands returns a copy of commands sent via SendCommand (thread-safe).
func (m *MockClient) GetSentCommands() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	commands := make([]string, len(m.SentCommands))
	copy(commands, m.SentCommands)
	return commands
}

// GetWarnings returns a copy of recorded warnings (thread-safe).
func (m *MockClient) GetWarnings() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	warnings := make([]string, len(m.Warnings))
	copy(warnings, m.Warnings)
	return warnings
}
