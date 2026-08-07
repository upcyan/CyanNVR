package snapshot

import (
	"os"
	"sync"
	"time"
)

func nowMs() int64 { return time.Now().UnixMilli() }

// Ring keeps the last N raw JPEG frames with capture times (in-memory per device).
type Ring struct {
	mu     sync.Mutex
	cap    int
	Frames []Frame
}

type Frame struct {
	Data []byte
	Time int64 // unix millis
}

func NewRing(cap int) *Ring {
	return &Ring{cap: cap}
}

func (r *Ring) Push(data []byte, t int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Frames = append(r.Frames, Frame{Data: data, Time: t})
	if len(r.Frames) > r.cap {
		r.Frames = r.Frames[len(r.Frames)-r.cap:]
	}
}

func (r *Ring) Snapshot(withinMs int64, max int) [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := nowMs()
	var out [][]byte
	for i := len(r.Frames) - 1; i >= 0 && len(out) < max; i-- {
		f := r.Frames[i]
		if now-f.Time <= withinMs {
			out = append(out, f.Data)
		}
	}
	// reverse to chronological
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
