package cli

import "sync"

const maxProcessOutput = 64 * 1024

// boundedOutput retains the end of an error log without keeping an entire
// transfer or long-running command's output in memory.
type boundedOutput struct {
	mu   sync.Mutex
	data []byte
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if n >= maxProcessOutput {
		b.data = append(b.data[:0], p[n-maxProcessOutput:]...)
	} else {
		if excess := len(b.data) + n - maxProcessOutput; excess > 0 {
			copy(b.data, b.data[excess:])
			b.data = b.data[:len(b.data)-excess]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}

func (b *boundedOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}
