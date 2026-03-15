package logs

import (
	"context"
	"sync"
)

const DefaultCapacity = 1 << 20 // 1 MB

type RingBuffer struct {
	mu       sync.Mutex
	data     []byte
	capacity int
	writePos int
	total    int64
	wrapped  bool

	subsMu sync.Mutex
	subs   map[int64]chan []byte
	nextID int64
}

func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &RingBuffer{
		data:     make([]byte, capacity),
		capacity: capacity,
		subs:     make(map[int64]chan []byte),
	}
}

func (rb *RingBuffer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	rb.mu.Lock()

	n := len(p)

	if n >= rb.capacity {
		copy(rb.data, p[n-rb.capacity:])
		rb.writePos = 0
		rb.wrapped = true
	} else {
		remaining := rb.capacity - rb.writePos
		if n <= remaining {
			copy(rb.data[rb.writePos:], p)
			rb.writePos += n
			if rb.writePos == rb.capacity {
				rb.writePos = 0
				rb.wrapped = true
			}
		} else {
			copy(rb.data[rb.writePos:], p[:remaining])
			copy(rb.data, p[remaining:])
			rb.writePos = n - remaining
			rb.wrapped = true
		}
	}

	rb.total += int64(n)
	rb.mu.Unlock()

	chunk := make([]byte, len(p))
	copy(chunk, p)

	rb.subsMu.Lock()
	for _, ch := range rb.subs {
		select {
		case ch <- chunk:
		default:
		}
	}
	rb.subsMu.Unlock()

	return n, nil
}

func (rb *RingBuffer) ReadAll() []byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.total == 0 {
		return nil
	}

	if !rb.wrapped {
		out := make([]byte, rb.writePos)
		copy(out, rb.data[:rb.writePos])
		return out
	}

	out := make([]byte, rb.capacity)
	n := copy(out, rb.data[rb.writePos:])
	copy(out[n:], rb.data[:rb.writePos])
	return out
}

func (rb *RingBuffer) TailLines(count int) []byte {
	all := rb.ReadAll()
	if count <= 0 || len(all) == 0 {
		return all
	}

	found := 0
	i := len(all) - 1

	if all[i] == '\n' {
		i--
	}

	for ; i >= 0; i-- {
		if all[i] == '\n' {
			found++
			if found >= count {
				return all[i+1:]
			}
		}
	}

	return all
}

func (rb *RingBuffer) Follow(ctx context.Context) <-chan []byte {
	ch := make(chan []byte, 64)

	rb.subsMu.Lock()
	id := rb.nextID
	rb.nextID++
	rb.subs[id] = ch
	rb.subsMu.Unlock()

	go func() {
		<-ctx.Done()
		rb.subsMu.Lock()
		delete(rb.subs, id)
		rb.subsMu.Unlock()
		close(ch)
	}()

	return ch
}

func (rb *RingBuffer) Len() int64 {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.total
}
