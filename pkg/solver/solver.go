// Package solver implements constraint solving for Minecraft plugin version compatibility.
package solver

// Solver defines the interface for finding compatible versions.
// Implementations include simple linear search and future SAT-based solvers.
type Solver interface {
	// Solve finds the best compatible versions for plugins and server.
	// Returns error if no solution exists.
	Solve() error
}
