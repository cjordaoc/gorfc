// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package backend

import "context"

// TableStream is a lazy RFC table reader owned by a caller until
// Close is called. Implementations keep any underlying SDK
// resources alive while the stream is open.
//
// Next returns io.EOF after the final row. Any other error means
// the stream is no longer usable and callers should still call
// Close; Close is idempotent.
type TableStream interface {
	Next(ctx context.Context) (map[string]any, error)
	Close() error
}

// StreamingBackend is an optional backend extension for lazy
// table responses. Unlike Backend.Invoke, the implementation
// does not destroy the RFC_FUNCTION_HANDLE before returning;
// ownership transfers to the returned TableStream until Close.
type StreamingBackend interface {
	InvokeTableStream(ctx context.Context, h ConnHandle, fn string, table string, in CallParams, opts InvokeOptions) (TableStream, error)
}
