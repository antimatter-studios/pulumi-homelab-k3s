package homelabk3s

import "testing"

func TestNodeTokenPath(t *testing.T) {
	if got := nodeTokenPath(ptr("/mnt/storage/k3s")); got != "/mnt/storage/k3s/server/node-token" {
		t.Errorf("custom token path = %q", got)
	}
	if got := nodeTokenPath(nil); got != "/var/lib/rancher/k3s/server/node-token" {
		t.Errorf("default token path = %q", got)
	}
}
