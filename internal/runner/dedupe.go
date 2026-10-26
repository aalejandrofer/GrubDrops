package runner

import "sync"

// onceLog reports whether a line with the given key differs from the last one
// seen, so a loop that repeats the same line logs it once per change.
type onceLog struct {
	mu   sync.Mutex
	last string
	set  bool
	n    int // repeats suppressed since the last logged line
}

// Changed reports whether key is new. Repeats return false and are counted.
func (o *onceLog) Changed(key string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.set && o.last == key {
		o.n++
		return false
	}
	o.last, o.set, o.n = key, true, 0
	return true
}

// Reset forgets the last key and returns how many repeats were suppressed.
func (o *onceLog) Reset() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := o.n
	o.last, o.set, o.n = "", false, 0
	return n
}
