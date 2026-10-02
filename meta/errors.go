package meta

import "errors"

var (
	ErrUnmarshalConfig = errors.New("failed to unmarshal config")
	ErrConfigNotFound  = errors.New("config not found")
	ErrStaleSettings   = errors.New("settings have not been initialized")
	ErrMissingSock     = errors.New("container_socket not set")
)
