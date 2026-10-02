package meta

import "errors"

var (
	ErrUnmarshalConfig = errors.New("failed to unmarshal config")
	ErrConfigNotFound  = errors.New("config not found")
	ErrStaleState      = errors.New("state is stale")
	ErrMissingSock     = errors.New("container_socket not set")
)
