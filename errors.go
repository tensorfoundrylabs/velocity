package velocity

import "errors"

var (
	ErrWriterClosed  = errors.New("writer is closed")
	ErrInvalidConfig = errors.New("invalid configuration")
	ErrEntryNotFound = errors.New("entry not found")
)
