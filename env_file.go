package haau3

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// Which env file in the current folder sets HAAU3_API_KEY when the program never loaded it: the case
// of a key written to .env (by hand, or by `npx haau3 keys create`) and a program that reads no env
// file, since Go loads none by itself. Asked only when a client is refused for having no key, so the
// refusal can name the file and how to load it.
//
// It looks for the variable's name and nothing else. The value is never parsed out, compared, kept
// or shown. Anything unexpected (a file that is not a regular file, a read that fails, a folder that
// has gone) is taken as "no such file", so the refusal is then the one it was before.

// envFiles are the files a key is written to, in the order Next.js, Vite and the haau3 command line
// read them.
var envFiles = []string{".env.local", ".env"}

// envFileLimit is the most of a file read: a key is one short line, and a larger file is not an env
// file to read.
const envFileLimit = 1 << 20

// setsTheKey is a line that sets HAAU3_API_KEY to something: the name at the start of a line (after
// spaces and an optional `export`), `=`, then a value that is not empty, a comment or an empty pair
// of quotes.
var setsTheKey = regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?HAAU3_API_KEY[ \t]*=[ \t]*(?:"[^"\r\n]|'[^'\r\n]|[^\s#"'])`)

var byteOrderMark = []byte{0xEF, 0xBB, 0xBF}

// fileSetsTheKey says whether a file sets HAAU3_API_KEY, reading at most envFileLimit bytes. Only a
// regular file is opened, so a named pipe called .env cannot hold the program.
func fileSetsTheKey(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, envFileLimit))
	if err != nil {
		return false
	}
	if len(data) >= len(byteOrderMark) && string(data[:len(byteOrderMark)]) == string(byteOrderMark) {
		data = data[len(byteOrderMark):]
	}
	return setsTheKey.Match(data)
}

// lookUpEnvFile is how the client asks, so a test can see that it asks only when refusing.
var lookUpEnvFile = unloadedEnvFile

// unloadedEnvFile is the first of envFiles in folder that sets HAAU3_API_KEY, or "" when none does or
// anything went wrong. folder is the current folder when empty.
func unloadedEnvFile(folder string) (found string) {
	defer func() {
		// Nothing here may change why the client was refused: any surprise is no file.
		if recover() != nil {
			found = ""
		}
	}()
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
