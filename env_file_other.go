//go:build !unix

package haau3

import "os"

// openNonBlocking opens a file for reading. Outside Unix a folder holds no named pipe to wait on.
func openNonBlocking(path string) (*os.File, error) {
	return os.Open(path)
}
