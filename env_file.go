package haau3

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// Which env file in the current folder sets HAAU3_API_KEY when the program's environment has no
// value for it: the case of a key written to .env (by hand, or by `npx haau3 keys create`) and a
// program that reads no env file, since Go loads none by itself. Asked only when a client is refused
// for having no key from HAAU3_API_KEY, so the refusal can name the file and how to load it.
//
// It never keeps or shows the value. Anything unexpected (a file that is not a regular file, a read
// that fails, a folder that has gone) is taken as "no such file", so the refusal is then the one it
// was before.

// envFiles are the files a key is written to, in the order Next.js, Vite and the haau3 command line
// read them.
var envFiles = []string{".env.local", ".env"}

// envFileLimit is the most of a file read: a key is one short line, and a larger file is not an env
// file to read.
const envFileLimit = 1 << 20

// assignsTheKey is a line that assigns HAAU3_API_KEY: the name at the start of the line (after
// spaces and an optional `export`), then `=`.
var assignsTheKey = regexp.MustCompile(`^[ \t]*(?:export[ \t]+)?HAAU3_API_KEY[ \t]*=`)

// setsTheKey is such a line that sets it to something: a value that is not empty, a comment, quotes
// holding nothing but spaces, or a reference to another variable.
var setsTheKey = regexp.MustCompile(`^[ \t]*(?:export[ \t]+)?HAAU3_API_KEY[ \t]*=[ \t]*(?:"[^"\s]|'[^'\s]|[^\s#"'$])`)

// lastAssignmentSetsTheKey says whether the file's last line assigning HAAU3_API_KEY sets it, since
// a loader keeps the last. A byte-order mark before the first line is not skipped, as a shell does
// not skip it either.
func lastAssignmentSetsTheKey(data []byte) bool {
	set := false
	for _, line := range bytes.Split(data, []byte("\n")) {
		if assignsTheKey.Match(line) {
			set = setsTheKey.Match(line)
		}
	}
	return set
}

// fileSetsTheKey says whether a file sets HAAU3_API_KEY, reading at most envFileLimit bytes. It asks
// what the path is before opening it, so a named pipe is never opened: password managers can serve .env
// as one, and an open would count as a read (or ask the person for approval). The open never
// blocks, and the open file is asked again, so a pipe swapped in between is not read either.
func fileSetsTheKey(path string) bool {
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return false
	}
	f, err := openNonBlocking(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return false
	}
	data, err := io.ReadAll(io.LimitReader(f, envFileLimit))
	if err != nil {
		return false
	}
	return lastAssignmentSetsTheKey(data)
}

// lookUpEnvFile is how the client asks, so a test can see when it asks, and what a failure gives.
var lookUpEnvFile = unloadedEnvFile

// unloadedEnvFile is the first of envFiles in folder that sets HAAU3_API_KEY, or "" when none does or
// anything went wrong. folder is the current folder when empty.
func unloadedEnvFile(folder string) string {
	if folder == "" {
		wd, err := os.Getwd()
		if err != nil {
			return ""
		}
		folder = wd
	}
	for _, name := range envFiles {
		if fileSetsTheKey(filepath.Join(folder, name)) {
			return name
		}
	}
	return ""
}
