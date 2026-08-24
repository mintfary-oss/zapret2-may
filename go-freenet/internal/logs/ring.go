// Package logs provides a thread-safe ring buffer that captures log lines
// and broadcasts them to WebSocket subscribers.
package logs

import (
	"io"
	"sync"
	"time"
)

// Entry is a single log record.
type Entry struct {
	Time    time.Time `json:"time"`
	Message string    `json:"msg"`
}

// Ring is a fixed-capacity ring buffer of log entries. It also implements
// io.Writer so it can be used as a log.SetOutput target.
type Ring struct {
	mu    sync.RWMutex
	buf   []Entry
	cap   int
	head  int // next write position
	count int
	subs  []chan Entry
	subMu sync.Mutex
}

// NewRing creates a Ring with the given capacity.
func NewRing(capacity int) *Ring {
	return &Ring{
		buf: make([]Entry, capacity),
		cap: capacity,
	}
}

// Write implements io.Writer, splitting p on newlines and storing each
// non-empty line as a separate Entry.
func (r *Ring) Write(p []byte) (int, error) {
	line := string(p)
	// Strip trailing newline added by log package.
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if line == "" {
		return len(p), nil
	}
	e := Entry{Time: time.Now(), Message: line}
	r.push(e)
	return len(p), nil
}

// Ensure Ring satisfies io.Writer.
var _ io.Writer = (*Ring)(nil)

func (r *Ring) push(e Entry) {
	r.mu.Lock()
	r.buf[r.head] = e
	r.head = (r.head + 1) % r.cap
	if r.count < r.cap {
		r.count++
	}
	r.mu.Unlock()

	// Notify subscribers (non-blocking).
	r.subMu.Lock()
	for _, ch := range r.subs {
		select {
		case ch <- e:
		default:
		}
	}
	r.subMu.Unlock()
}

// Recent returns the last n entries in chronological order.
func (r *Ring) Recent(n int) []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if n > r.count {
		n = r.count
	}
	out := make([]Entry, n)
	start := (r.head - n + r.cap) % r.cap
	for i := 0; i < n; i++ {
		out[i] = r.buf[(start+i)%r.cap]
	}
	return out
}

// Subscribe returns a channel that receives new log entries in real time.
// The caller must call Unsubscribe when done to avoid a goroutine leak.
func (r *Ring) Subscribe() chan Entry {
	ch := make(chan Entry, 64)
	r.subMu.Lock()
	r.subs = append(r.subs, ch)
	r.subMu.Unlock()
	return ch
}

// Unsubscribe removes a previously registered channel.
func (r *Ring) Unsubscribe(ch chan Entry) {
	r.subMu.Lock()
	defer r.subMu.Unlock()
	for i, s := range r.subs {
		if s == ch {
			r.subs = append(r.subs[:i], r.subs[i+1:]...)
			close(ch)
			return
		}
	}
}
