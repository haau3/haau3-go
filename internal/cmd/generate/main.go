// Command generate writes the SDK's typed surface from the pinned spec, openapi/openapi.json, and
// HL7's FHIR R4 schema, hl7/fhir.schema.json.zip:
//
//   - fhir/r4_gen.go: every FHIR R4 type, from HL7's own schema;
//   - types_gen.go: the spec's other components (Scheduling's records and answers, Problem);
//   - operations_gen.go: one method per operation, named by its operationId in Go's style, its
//     params struct, and its entry for a batch;
//   - operations_gen_test.go: each operation called with the spec's own example;
//   - api.md: every method by service, each linking its page on docs.haau3.com.
//
// Run it from the repository's root:
//
//	go generate ./...                            write them
//	go run ./internal/cmd/generate -check        fail, listing them, when any differs from what is committed
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	check := flag.Bool("check", false, "fail when a generated file differs from what is written, instead of writing it")
	root := flag.String("root", ".", "the repository's root")
	flag.Parse()
	if err := run(*root, *check); err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}
}

func outputs(root string) (map[string][]byte, error) {
	specData, err := os.ReadFile(filepath.Join(root, "openapi", "openapi.json"))
	if err != nil {
		return nil, err
	}
	s, err := parseSpec(specData)
	if err != nil {
		return nil, err
	}
	zipped, err := os.ReadFile(filepath.Join(root, "hl7", "fhir.schema.json.zip"))
	if err != nil {
		return nil, err
	}
	hl7, err := readFHIRSchema(zipped)
	if err != nil {
		return nil, err
	}
	for _, t := range s.fhirTypes {
		if _, ok := hl7.defs[t]; !ok {
			return nil, fmt.Errorf("the spec names %s, which FHIR R4 does not define", t)
		}
	}
	fhirSource, err := emitFHIR(hl7)
	if err != nil {
		return nil, err
	}
	typesSource, err := emitTypes(s)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for name, source := range map[string]string{
		"fhir/r4_gen.go":         fhirSource,
		"types_gen.go":           typesSource,
		"operations_gen.go":      emitOperations(s),
		"operations_gen_test.go": emitExamplesTest(s),
	} {
		formatted, err := format.Source([]byte(source))
		if err != nil {
			return nil, fmt.Errorf("%s does not format: %w", name, err)
		}
		out[name] = formatted
	}
	out["api.md"] = []byte(apiMD(s))
	samples, err := emitSamples(s, hl7)
	if err != nil {
		return nil, err
	}
	out["openapi/code-samples.json"] = samples
	return out, nil
}

func run(root string, check bool) error {
	files, err := outputs(root)
	if err != nil {
		return err
	}
	var stale []string
	for name, want := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if check {
			have, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(have, want) {
				stale = append(stale, name)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, want, 0o644); err != nil {
			return err
		}
	}
	if len(stale) > 0 {
		return fmt.Errorf("these differ from what the spec generates; run go generate ./...: %s", strings.Join(stale, ", "))
	}
	return nil
}
