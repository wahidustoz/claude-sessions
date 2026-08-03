//go:build !windows

package main

import "os"

// openTTY opens the controlling terminal for reading and writing. The picker uses
// it instead of stdin/stdout so the resume commands it prints stay pipeable.
func openTTY() (in, out *os.File, closeTTY func(), err error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, func() {}, err
	}
	return f, f, func() { f.Close() }, nil
}
