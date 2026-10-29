// notices gathers license texts for the modules used by the binary and Go itself.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func main() {
	out, err := exec.Command("go", "list", "-buildvcs=false", "-deps", "-f", "{{with .Module}}{{.Path}}\t{{.Version}}\t{{.Dir}}{{end}}", "./cmd/outline-mcp").CombinedOutput()
	if err != nil {
		must(fmt.Errorf("list dependency licenses: %w: %s", err, out))
	}
	modules := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 || fields[0] == "outline-mcp" {
			continue
		}
		modules[fields[0]+" "+fields[1]] = fields[2]
	}
	var result bytes.Buffer
	fmt.Fprintln(&result, "Outline MCP third-party notices\n\nProject code: MIT; see LICENSE. The following components retain their own licenses.")
	appendLicense(&result, "Outline OpenAPI e3d5671461fc8c292286b432af0ce1f87dba5345", "api/LICENSE")
	goLicense := filepath.Join(runtime.GOROOT(), "LICENSE")
	if _, err := os.Stat(goLicense); os.IsNotExist(err) {
		// Arch packages the Go license separately from GOROOT.
		goLicense = "/usr/share/licenses/go/LICENSE"
	}
	appendLicense(&result, "Go runtime "+runtime.Version(), goLicense)
	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		found := false
		for _, file := range []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "COPYING", "NOTICE"} {
			path := filepath.Join(modules[name], file)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				continue
			} else {
				must(err)
			}
			appendLicense(&result, name, path)
			found = true
		}
		if !found {
			must(fmt.Errorf("no license found for %s; review before release", name))
		}
	}
	_, err = os.Stdout.Write(result.Bytes())
	must(err)
}
func appendLicense(b *bytes.Buffer, name, path string) {
	data, err := os.ReadFile(path)
	must(err)
	fmt.Fprintf(b, "\n--- %s ---\n\n%s\n", name, data)
}
func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
