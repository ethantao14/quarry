//go:build unix

package index

import (
	"fmt"
	"math"
	"os"
	"sync/atomic"
	"syscall"
)

// liveMappings counts mapped files that are not yet unmapped, so tests can detect leaks.
var liveMappings atomic.Int64

func mapFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("map %s: %w", path, err)
	}
	// Closing a read-only file cannot lose data; its mapping remains valid.
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("map %s: %w", path, err)
	}
	if info.Size() == 0 {
		return []byte{}, nil
	}
	if info.Size() > math.MaxInt {
		return nil, fmt.Errorf("map %s: file size exceeds maximum int", path)
	}
	data, err := syscall.Mmap(int(file.Fd()), 0, int(info.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("map %s: %w", path, err)
	}
	liveMappings.Add(1)
	return data, nil
}

func unmapFile(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if err := syscall.Munmap(data); err != nil {
		return err
	}
	liveMappings.Add(-1)
	return nil
}
