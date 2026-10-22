package consumer_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// A separate module is essential: an in-module test can accidentally import
// internal implementation packages and therefore cannot verify consumer reuse.
func TestExternalApplicationCompilesAndRoutes(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	module := "module example.com/gostack-consumer\n\ngo 1.27.3\n\nrequire github.com/tjahjono/gostack v0.0.0\nreplace github.com/tjahjono/gostack => " + strconv.Quote(filepath.ToSlash(root)) + "\n"
	for name, body := range map[string][]byte{"go.mod": []byte(module)} {
		if err = os.WriteFile(filepath.Join(directory, name), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for source, destination := range map[string]string{"testdata/consumer_test.go.txt": "consumer_test.go", filepath.Join(root, "go.sum"): "go.sum"} {
		body, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(directory, destination), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	name := "go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	command := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", name), "test", "-mod=mod", "./...")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("external consumer failed: %v\n%s", err, output)
	}
}
