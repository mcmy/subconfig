package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func inputPath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "examples", "clash.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestCLIStdoutAndMetadata(t *testing.T) {
	var out, diag bytes.Buffer
	meta := filepath.Join(t.TempDir(), "meta.json")
	err := run([]string{"-c", inputPath(t), "-t", "sing-box", "--used", "1GiB", "--total", "10GiB", "--userinfo-output", meta}, strings.NewReader(""), &out, &diag)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out.Bytes()) {
		t.Fatalf("stdout contains diagnostics: %s", out.Bytes())
	}
	if !strings.Contains(diag.String(), "converted singbox") {
		t.Fatal("missing stderr diagnostic")
	}
	b, err := os.ReadFile(meta)
	if err != nil || !bytes.Contains(b, []byte("1073741824")) {
		t.Fatal(string(b), err)
	}
}
func TestCLIAtomicFileAndFailure(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(p, []byte("original"), 0600)
	var out, diag bytes.Buffer
	err := run([]string{"-c", inputPath(t), "-t", "clash", "-o", p, "--include", "DOES-NOT-MATCH"}, strings.NewReader(""), &out, &diag)
	if err == nil {
		t.Fatal("expected empty conversion to fail")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "original" {
		t.Fatal("failed conversion overwrote output")
	}
	err = run([]string{"-c", inputPath(t), "-t", "clash", "-o", p, "--quiet"}, strings.NewReader(""), &out, &diag)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	if out.Len() != 0 {
		t.Fatal("file output should not print to stdout")
	}
}
func TestCLIHelpVersionAndMissingSecret(t *testing.T) {
	t.Setenv("SUBCONFIG_SECRET", "")
	for _, args := range [][]string{{"--help"}, {"version"}, {"--version"}} {
		var out, diag bytes.Buffer
		if err := run(args, strings.NewReader(""), &out, &diag); err != nil {
			t.Fatal(args, err)
		}
		if out.Len()+diag.Len() == 0 {
			t.Fatal("missing output")
		}
	}
	var out, diag bytes.Buffer
	if err := run([]string{"-p", "8080"}, strings.NewReader(""), &out, &diag); err == nil || !strings.Contains(err.Error(), "requires") {
		t.Fatal(err)
	}
}
func TestCLIStdinAndSidecarPathConflict(t *testing.T) {
	input, _ := os.ReadFile(inputPath(t))
	var out, diag bytes.Buffer
	if err := run([]string{"-c", "-", "-t", "clash", "--quiet"}, bytes.NewReader(input), &out, &diag); err != nil || out.Len() == 0 || diag.Len() != 0 {
		t.Fatal(err, out.Len(), diag.String())
	}
	p := filepath.Join(t.TempDir(), "output.yaml")
	if err := run([]string{"-c", inputPath(t), "-o", p, "--userinfo-output", filepath.Join(filepath.Dir(p), ".", "output.yaml")}, strings.NewReader(""), &out, &diag); err == nil {
		t.Fatal("metadata may not overwrite config")
	}
}
