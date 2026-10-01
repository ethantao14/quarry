//go:build !unix

package index

import "errors"

func mapFile(path string) ([]byte, error) {
	return nil, errors.New("memory-mapped indexes are not supported on this platform")
}

func unmapFile(data []byte) error {
	return nil
}
