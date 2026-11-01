package contract_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

var (
	apiRow     = regexp.MustCompile("(?m)^\\| `(\\w+)`(?: \\*terms\\*)? \\| ([^|]+) \\|")
	cliRow     = regexp.MustCompile("(?m)^\\| `sock [^`]*` \\| ([^|]+) \\|")
	cliExclude = regexp.MustCompile("(?m)^- `(\\w+)`:")
	backticked = regexp.MustCompile("`(\\w+)`")
)

// TestCLICoversHumanRPCs keeps docs/specs/cli.md in step with api.md: every
// RPC a human or an anonymous caller reaches has a sock command or a stated
// exclusion, and cli.md names no RPC api.md does not define.
func TestCLICoversHumanRPCs(t *testing.T) {
	api := read(t, "docs/specs/api.md")
	cli := read(t, "docs/specs/cli.md")

	all := map[string]bool{}
	var human []string
	for _, m := range apiRow.FindAllStringSubmatch(api, -1) {
		all[m[1]] = true
		caller := strings.ToLower(m[2])
		if strings.Contains(caller, "human") || strings.Contains(caller, "anonymous") {
			human = append(human, m[1])
		}
	}
	if len(human) == 0 {
		t.Fatal("no human RPCs parsed from api.md; did its table format change?")
	}

	covered := map[string]bool{}
	for _, m := range cliRow.FindAllStringSubmatch(cli, -1) {
		for _, b := range backticked.FindAllStringSubmatch(m[1], -1) {
			if !all[b[1]] {
				t.Errorf("cli.md maps to %s, which api.md does not define", b[1])
			}
			covered[b[1]] = true
		}
	}
	for _, m := range cliExclude.FindAllStringSubmatch(cli, -1) {
		covered[m[1]] = true
	}
	for _, rpc := range human {
		if !covered[rpc] {
			t.Errorf("human RPC %s has no sock command or exclusion in cli.md", rpc)
		}
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
