// checksums prints a portable SHA256SUMS manifest for explicitly named files.
package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fail(fmt.Errorf("usage: go run ./scripts/checksums FILE..."))
	}
	seen := map[string]bool{}
	for _, path := range os.Args[1:] {
		name := filepath.Base(path)
		if seen[name] || strings.ContainsAny(name, "\\\r\n") {
			fail(fmt.Errorf("duplicate or unsupported filename: %q", name))
		}
		seen[name] = true
		f, err := os.Open(path)
		if err != nil {
			fail(err)
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		closeErr := f.Close()
		if err != nil {
			fail(err)
		}
		if closeErr != nil {
			fail(closeErr)
		}
		fmt.Printf("%x  %s\n", h.Sum(nil), name)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
