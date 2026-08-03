//go:build windows

package main

import "os"

// openTTY opens the Windows console. Unlike Unix there is no single read-write
// terminal device, so input and output are separate handles.
func openTTY() (in, out *os.File, closeTTY func(), err error) {
	cin, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, func() {}, err
	}
	cout, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		cin.Close()
		return nil, nil, func() {}, err
	}
	return cin, cout, func() { cin.Close(); cout.Close() }, nil
}
