//go:build unix

package haau3

import (
	"os"
	"syscall"
)

// openNonBlocking opens a file for reading without waiting, so a named pipe that took a regular
// file's place since it was asked about cannot hold the program.
func openNonBlocking(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
