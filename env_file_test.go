package haau3

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A client refused for having no key from HAAU3_API_KEY names an env file in the current folder that
// sets it, and says how to load it: when the environment has no such variable, and when it has one set
// to nothing, which a loader would keep. The value is never in the message or anywhere on the error.
// Anything unexpected leaves the refusal as it was, nothing but a regular file is opened, and nothing
// is read unless the client is refused for having no key from the variable.

// plantedKey is a made-up key planted in the files, which must never be repeated.
const (
	plantedKey   = "haau3_sk_live_PLANTEDmarkerNEVERshown000000000000000000"
	plantedMark  = "PLANTEDmarker"
	plantedLine  = "HAAU3_API_KEY=" + plantedKey + "\n"
	plantedOther = "OTHER=1\n"
)

// folderWith is a new folder holding these files.
func folderWith(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// withoutTheVariable leaves HAAU3_API_KEY out of the environment for this test, not merely empty.
func withoutTheVariable(t *testing.T) {
	t.Helper()
	clearEnvironment(t)
	if err := os.Unsetenv("HAAU3_API_KEY"); err != nil {
		t.Fatal(err)
	}
}

// shownAll is every way an error can be shown, as one text.
func shownAll(err error) string {
	data, _ := json.Marshal(err)
	return strings.Join([]string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err), string(data)}, "\n")
}

func TestALineThatSetsTheKeyIsFound(t *testing.T) {
	for name, text := range map[string]string{
		"plain":                   plantedLine,
		"after export":            "export " + plantedLine,
		"spaces and quotes":       `  HAAU3_API_KEY = "` + plantedKey + "\"\n",
		"single quotes":           "HAAU3_API_KEY='" + plantedKey + "'\n",
		"CRLF after other lines":  "# keys\r\nOTHER=1\r\nHAAU3_API_KEY=" + plantedKey + "\r\n",
		"with no line ending":     strings.TrimSuffix(plantedLine, "\n"),
		"set again after empty":   "HAAU3_API_KEY=\n" + plantedLine,
		"after a byte-order mark": "\xEF\xBB\xBFOTHER=1\n" + plantedLine,
	} {
		if got := unloadedEnvFile(folderWith(t, map[string]string{".env": text})); got != ".env" {
			t.Errorf("%s: found %q, want .env", name, got)
		}
	}
}

func TestALineThatDoesNotSetTheKeyIsNotCounted(t *testing.T) {
	for name, text := range map[string]string{
		"an empty value":          "HAAU3_API_KEY=\n",
		"empty double quotes":     "HAAU3_API_KEY=\"\"\n",
		"empty single quotes":     "HAAU3_API_KEY=''\n",
		"quotes holding spaces":   "HAAU3_API_KEY=\"   \"\n",
		"a comment for a value":   "HAAU3_API_KEY= # later\n",
		"another variable":        "HAAU3_API_KEY=$OTHER\n",
		"another variable braced": "HAAU3_API_KEY=${OTHER}\n",
		"a commented-out line":    "# " + plantedLine,
		"a longer name":           "HAAU3_API_KEY_OLD=" + plantedKey + "\n",
		"a name that ends in it":  "MY_" + plantedLine,
		"only other variables":    "HAAU3_ENVIRONMENT=sandbox\n",
		// A loader keeps the last assignment.
		"emptied after it is set": plantedLine + "HAAU3_API_KEY=\n",
		// A shell does not skip a byte-order mark, so a first line behind one sets nothing.
		"behind a byte-order mark": "\xEF\xBB\xBF" + plantedLine,
	} {
		if got := unloadedEnvFile(folderWith(t, map[string]string{".env": text})); got != "" {
			t.Errorf("%s: found %q, want none", name, got)
		}
	}
}

func TestEnvLocalIsNamedBeforeEnv(t *testing.T) {
	if got := unloadedEnvFile(folderWith(t, map[string]string{".env.local": plantedLine, ".env": plantedLine})); got != ".env.local" {
		t.Errorf("found %q, want .env.local", got)
	}
	if got := unloadedEnvFile(folderWith(t, map[string]string{".env.local": plantedOther, ".env": plantedLine})); got != ".env" {
		t.Errorf("found %q, want .env", got)
	}
	if got := unloadedEnvFile(folderWith(t, map[string]string{"other.env": plantedLine})); got != "" {
		t.Errorf("found %q in a folder with neither file", got)
	}
}

func TestAtMostOneMiBOfAFileIsRead(t *testing.T) {
	padding := strings.Repeat("# "+strings.Repeat("x", 1022)+"\n", envFileLimit/1024)
	if got := unloadedEnvFile(folderWith(t, map[string]string{".env": padding + plantedLine})); got != "" {
		t.Errorf("found %q past the limit", got)
	}
	if got := unloadedEnvFile(folderWith(t, map[string]string{".env": plantedLine + padding})); got != ".env" {
		t.Errorf("found %q within the limit, want .env", got)
	}
}

func TestOnlyARegularFileIsAsked(t *testing.T) {
	dir := folderWith(t, map[string]string{".env": plantedLine})
	if err := os.Mkdir(filepath.Join(dir, ".env.local"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := unloadedEnvFile(dir); got != ".env" {
		t.Errorf("found %q, want .env after a folder called .env.local", got)
	}
}

func TestASymlinkToARegularFileIsFollowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a symlink needs a privilege on Windows")
	}
	shared := folderWith(t, map[string]string{"shared.env": plantedLine})
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(shared, "shared.env"), filepath.Join(dir, ".env")); err != nil {
		t.Fatal(err)
	}
	if got := unloadedEnvFile(dir); got != ".env" {
		t.Errorf("found %q through a symlink, want .env", got)
	}
}

func TestAFileThatCannotBeReadIsNoFile(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a file's mode does not stop root, or Windows")
	}
	dir := folderWith(t, map[string]string{".env": plantedLine})
	if err := os.Chmod(filepath.Join(dir, ".env"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, ".env"), 0o600) })
	if got := unloadedEnvFile(dir); got != "" {
		t.Errorf("found %q in a file that cannot be read", got)
	}
}

func TestTheRefusalSaysTheEnvironmentHasNoValueAndHowToLoadTheFile(t *testing.T) {
	withoutTheVariable(t)
	t.Chdir(folderWith(t, map[string]string{".env": plantedOther + plantedLine}))
	e := refusedBy(t)
	if e.Code != CodeNoAPIKey {
		t.Errorf("code %q, want %q", e.Code, CodeNoAPIKey)
	}
	want := "No API key. .env in this folder sets HAAU3_API_KEY, but this program's environment has no value for it. " +
		"Read the file into the environment before NewClient, with a dotenv package, or, for a file of plain " +
		"NAME=value lines, run set -a; . ./.env; set +a in a POSIX shell first."
	if !strings.Contains(e.Error(), want) {
		t.Errorf("%q does not say %q", e.Error(), want)
	}
	if strings.Contains(shownAll(e), plantedMark) {
		t.Error("the refusal shows the planted key")
	}
}

func TestTheRefusalNamesEnvLocalWhenThatIsTheFile(t *testing.T) {
	withoutTheVariable(t)
	t.Chdir(folderWith(t, map[string]string{".env.local": plantedLine}))
	e := refusedBy(t)
	for _, want := range []string{".env.local in this folder sets HAAU3_API_KEY", "set -a; . ./.env.local; set +a"} {
		if !strings.Contains(e.Error(), want) {
			t.Errorf("%q does not say %q", e.Error(), want)
		}
	}
	if strings.Contains(shownAll(e), plantedMark) {
		t.Error("the refusal shows the planted key")
	}
}

// A variable set to nothing, as a compose file passing an unset variable through leaves it, is kept
// by a loader, so the refusal says to unset it rather than only to load the file.
func TestTheRefusalSaysAnEmptyVariableHidesTheFile(t *testing.T) {
	clearEnvironment(t)
	t.Chdir(folderWith(t, map[string]string{".env": plantedLine}))
	e := refusedBy(t)
	want := "No API key. HAAU3_API_KEY is set but empty in this program's environment, and .env in this folder sets it. " +
		"A loader keeps a variable the environment already has, so unset HAAU3_API_KEY where it is set to nothing, " +
		"then load the file."
	if !strings.Contains(e.Error(), want) {
		t.Errorf("%q does not say %q", e.Error(), want)
	}
	if strings.Contains(shownAll(e), plantedMark) {
		t.Error("the refusal shows the planted key")
	}
}

func TestWithNoSuchFileTheRefusalNamesTheCommandLine(t *testing.T) {
	withoutTheVariable(t)
	t.Chdir(folderWith(t, map[string]string{".env": "HAAU3_ENVIRONMENT=\n"}))
	e := refusedBy(t)
	want := "No API key. Set HAAU3_API_KEY, or pass it as haau3.WithAPIKey. Create a free key at " +
		"https://platform.haau3.com, or run npx haau3 login, then npx haau3 keys create, which writes one to " +
		"this project's env file (see https://docs.haau3.com/authentication). To call the Sandbox with no " +
		"key, set HAAU3_ENVIRONMENT=sandbox, or pass haau3.WithEnvironmentSandbox()."
	if !strings.Contains(e.Error(), want) {
		t.Errorf("%q does not say %q", e.Error(), want)
	}
}

// The control: the file is asked only for a refusal with no key from HAAU3_API_KEY; never with a key,
// the Sandbox named, or an empty key given with WithAPIKey, which is the caller's own choice.
func TestTheFileIsAskedOnlyWhenTheVariableGaveNoKey(t *testing.T) {
	withoutTheVariable(t)
	t.Chdir(folderWith(t, map[string]string{".env": plantedLine}))
	asked := 0
	original := lookUpEnvFile
	lookUpEnvFile = func(folder string) string {
		asked++
		return original(folder)
	}
	t.Cleanup(func() { lookUpEnvFile = original })
	if _, err := NewClient(WithAPIKey(markerKey)); err != nil {
		t.Errorf("a key: %v", err)
	}
	if _, err := NewClient(WithEnvironmentSandbox()); err != nil {
		t.Errorf("the Sandbox named: %v", err)
	}
	e := refusedBy(t, WithAPIKey(""))
	if e.Code != CodeNoAPIKey || strings.Contains(e.Error(), ".env") {
		t.Errorf("an empty WithAPIKey: %q", e.Error())
	}
	t.Setenv("HAAU3_ENVIRONMENT", "sandbox")
	if _, err := NewClient(); err != nil {
		t.Errorf("HAAU3_ENVIRONMENT=sandbox: %v", err)
	}
	if asked != 0 {
		t.Errorf("an env file was asked %d times with a key, the Sandbox or WithAPIKey", asked)
	}
	t.Setenv("HAAU3_ENVIRONMENT", "")
	var refusal *Error
	if _, err := NewClient(); err == nil || !errors.As(err, &refusal) || refusal.Code != CodeNoAPIKey || asked != 1 {
		t.Errorf("the refusal for no key asked %d times: %v", asked, err)
	}
}
