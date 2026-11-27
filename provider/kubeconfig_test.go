package homelabk3s

import (
	"strings"
	"testing"
)

func TestRepoint(t *testing.T) {
	const already = "    server: https://192.168.0.47:6443\n"
	if got := repoint("    server: https://127.0.0.1:6443\n", "192.168.0.47"); got != already {
		t.Errorf("rewritten endpoint = %q", got)
	}
	const both = "a: https://127.0.0.1:6443\nb: https://127.0.0.1:6443\n"
	if got := repoint(both, "pi"); strings.Contains(got, "127.0.0.1") || got != "a: https://pi:6443\nb: https://pi:6443\n" {
		t.Errorf("rewritten endpoints = %q", got)
	}
	if got := repoint(already, "192.168.0.47"); got != already {
		t.Errorf("already correct endpoint = %q", got)
	}
	if got := repoint("x: https://127.0.0.1:8080\n", "pi"); !strings.Contains(got, "127.0.0.1:8080") {
		t.Errorf("different port = %q", got)
	}
}
