package vaultproc

// lockedBytes holds bytes outside the Go heap where the platform allows it, in memory that is kept out of
// swap and core dumps as far as the system lets this process. free overwrites them.
type lockedBytes struct {
	mem    []byte // the whole allocation
	n      int    // the bytes in use
	locked bool   // whether the system keeps mem out of swap
	free   func()
}

// newLocked copies data into a fresh allocation. It falls back to the heap where the platform has no
// allocation of its own or refuses it; the copy is still overwritten when it is freed.
func newLocked(data []byte) *lockedBytes {
	l := allocLocked(len(data))
	if l == nil {
		mem := make([]byte, len(data))
		l = &lockedBytes{mem: mem, free: func() { clear(mem) }}
	}
	l.n = copy(l.mem, data)
	return l
}

func (l *lockedBytes) bytes() []byte {
	if l == nil {
		return nil
	}
	return l.mem[:l.n:l.n]
}

func (l *lockedBytes) release() {
	if l != nil && l.free != nil {
		l.free()
		l.free, l.mem, l.n = nil, nil, 0
	}
}

func pageSize(n, page int) int {
	if n <= 0 {
		return page
	}
	return (n + page - 1) / page * page
}
