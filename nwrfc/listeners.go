// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"sync"
	"time"
)

// ConnEvent is the kind of lifecycle transition a [Listener]
// is notified about.
type ConnEvent uint8

const (
	// EventOpened fires after a successful Open.
	EventOpened ConnEvent = iota + 1
	// EventClosed fires after a Close.
	EventClosed
	// EventBroken fires when the wrapper detects an
	// unrecoverable connection state (e.g. after a
	// Communication failure, before the next call retries).
	EventBroken
)

// String returns a stable lowercase identifier for the event.
func (e ConnEvent) String() string {
	switch e {
	case EventOpened:
		return "opened"
	case EventClosed:
		return "closed"
	case EventBroken:
		return "broken"
	}
	return "unknown"
}

// Listener is invoked for every Conn-lifecycle transition.
// Implementations MUST return quickly (the listener runs on the
// goroutine performing the transition). Long-running work
// should be handed to a separate goroutine inside the listener.
type Listener interface {
	OnConnEvent(e ConnEvent, dest string, at time.Time, err error)
}

// ListenerFunc adapts a function to the [Listener] interface.
type ListenerFunc func(e ConnEvent, dest string, at time.Time, err error)

// OnConnEvent implements [Listener].
func (f ListenerFunc) OnConnEvent(e ConnEvent, dest string, at time.Time, err error) {
	f(e, dest, at, err)
}

var (
	listenersMu sync.RWMutex
	listeners   []Listener
)

// AddListener registers l as a process-wide connection event
// observer. The same Listener may be registered multiple
// times (each registration is invoked separately).
func AddListener(l Listener) {
	if l == nil {
		return
	}
	listenersMu.Lock()
	defer listenersMu.Unlock()
	listeners = append(listeners, l)
}

// RemoveListener removes the first listener registration that
// equals l. No-op when l was not registered.
func RemoveListener(l Listener) {
	listenersMu.Lock()
	defer listenersMu.Unlock()
	for i, x := range listeners {
		if x == l {
			listeners = append(listeners[:i], listeners[i+1:]...)
			return
		}
	}
}

// fireEvent invokes every registered listener. Called by the
// Conn lifecycle methods (Open / Close, plus the cgo backend
// when it detects a broken state). Recovers from panics in
// listeners so a misbehaving observer does not break the SDK
// path.
func fireEvent(e ConnEvent, dest string, err error) {
	listenersMu.RLock()
	snapshot := make([]Listener, len(listeners))
	copy(snapshot, listeners)
	listenersMu.RUnlock()
	now := time.Now()
	for _, l := range snapshot {
		func() {
			defer func() { _ = recover() }()
			l.OnConnEvent(e, dest, now, err)
		}()
	}
}
