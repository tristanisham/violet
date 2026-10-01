package meta

import "errors"

var (
	ErrUnmarshalConfig = errors.New("failed to unmarshal config")
	ErrConfigNotFound = errors.New("config not found")
)