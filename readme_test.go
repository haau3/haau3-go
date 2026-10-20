package haau3

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var goBlock = regexp.MustCompile("(?s)```go\n(.*?)```")

// readmeBlocks is the README's Go code, block by block.
func readmeBlocks(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	var blocks []string
	for _, m := range goBlock.FindAllStringSubmatch(string(data), -1) {
		blocks = append(blocks, m[1])
	}
	return blocks
}

// declared is the names a block declares at its top level, so the test can use each one: a README
// shows what a call gives, and leaves its use to the reader.
func declared(t *testing.T, body string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "block.go", "package p\nfunc f() {\n"+body+"\n}\n", 0)
	if err != nil {
		t.Fatalf("a README block does not parse: %v\n%s", err, body)
	}
	var names []string
	for _, stmt := range f.Decls[0].(*ast.FuncDecl).Body.List {
		switch s := stmt.(type) {
		case *ast.AssignStmt:
			if s.Tok == token.DEFINE {
				for _, lhs := range s.Lhs {
					if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" && !slices.Contains(names, id.Name) {
						names = append(names, id.Name)
					}
				}
			}
		case *ast.DeclStmt:
			for _, spec := range s.Decl.(*ast.GenDecl).Specs {
				if v, ok := spec.(*ast.ValueSpec); ok {
					for _, id := range v.Names {
						names = append(names, id.Name)
					}
				}
			}
		}
	}
	return names
}

// Every Go block in the README compiles against this module, as a customer would paste it.
func TestTheREADMEsGoCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a module")
	}
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module readme\n\ngo 1.26.0\n\nrequire github.com/haau3/haau3-go v0.0.0\n\nreplace github.com/haau3/haau3-go => "+root+"\n")
	var body bytes.Buffer
	body.WriteString(`package readme

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/haau3/haau3-go"
	"github.com/haau3/haau3-go/fhir"
)

var (
	_ = errors.As
	_ = fmt.Sprint
	_ = io.EOF
	_ = slog.Info
	_ = http.MethodGet
	_ = strings.Contains
	_ = time.Second
	_ = testing.Short
	_ = fhir.DecodeResource
)
`)
	n := 0
	var tests bytes.Buffer
	tests.WriteString("package readme\n\nimport (\n\t\"io\"\n\t\"net/http\"\n\t\"strings\"\n\t\"testing\"\n\n\t\"github.com/haau3/haau3-go\"\n)\n")
	for _, block := range readmeBlocks(t) {
		if strings.HasPrefix(block, "type ") || strings.HasPrefix(block, "func ") {
			tests.WriteString("\n" + block) // a test of a customer's own, which runs too
			continue
		}
		n++
		fmt.Fprintf(&body, "\nfunc block%d(ctx context.Context, client *haau3.Client, planID, jobID string, records, first, message *fhir.Bundle) error {\n%s", n, block)
		for _, name := range declared(t, block) {
			fmt.Fprintf(&body, "\t_ = %s\n", name)
		}
		body.WriteString("\treturn nil\n}\n")
	}
	if n < 10 {
		t.Fatalf("only %d blocks found", n)
	}
	write("readme.go", body.String())
	write("readme_test.go", tests.String())
	for _, args := range [][]string{{"vet", "./..."}, {"test", "-count=1", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s on the README's Go:\n%s\n%s\n%s", args[0], out, body.String(), tests.String())
		}
	}
}
