package meta

import "errors"

var (
	ErrUnmarshalConfig = errors.New("failed to unmarshal config")
	ErrConfigNotFound  = errors.New("config not found")
	ErrStaleState      = errors.New("state is stale")
	ErrMissingSock     = errors.New("container_socket not set")
)

// Handler-directive errors control how main shuts down. Wrap one into an error
// (with errors.Join or fmt.Errorf's %w) when a command has already shown the
// user everything they need and only wants to control the exit. The wrapped
// message is never printed on its own; it is only debug-logged (VIOLET_DEBUG).
var (
	// ErrFailQuietly makes main exit with status 1 without user-facing output.
	ErrFailQuietly = errors.New("fail quietly")
)
