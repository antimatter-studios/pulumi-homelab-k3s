package homelabk3s

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/antimatter-studios/pulumi-homelab/provider/remote"
	"github.com/google/uuid"
)

type kindRunner string

func (r kindRunner) Run(ctx context.Context, command string, _ []byte) ([]byte, []byte, int, error) {
	cmd := exec.CommandContext(ctx, "docker", "exec", string(r), "sh", "-c", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && ctx.Err() == nil {
		return stdout.Bytes(), stderr.Bytes(), exit.ExitCode(), nil
	}
	return stdout.Bytes(), stderr.Bytes(), 0, err
}

func TestKindMaintenance(t *testing.T) {
	if os.Getenv("HOMELAB_K3S_TEST_KIND") != "1" {
		t.Skip("set HOMELAB_K3S_TEST_KIND=1 to create a disposable kind cluster in Docker")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	name := "homelab-k3s-test-" + uuid.NewString()[:8]
	kubeconfig := filepath.Join(t.TempDir(), "kind.yaml")
	kind := func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "kind", args...)
		cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfig, "KIND_EXPERIMENTAL_PROVIDER=docker")
		return cmd.CombinedOutput()
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if out, err := kind(cleanup, "delete", "cluster", "--name", name); err != nil {
			t.Errorf("delete kind cluster: %v\n%s", err, out)
		}
	})
	const kindVersion = "v1.33.1"
	if out, err := kind(ctx, "create", "cluster", "--name", name, "--kubeconfig", kubeconfig,
		"--image", "kindest/node:"+kindVersion, "--wait", "120s"); err != nil {
		t.Fatalf("create kind cluster: %v\n%s", err, out)
	}

	node := name + "-control-plane"
	target := &target{runner: kindRunner(node), cfg: remote.Config{Escalation: "none"}}
	// Adapt the disposable container's paths to the k3s layout without substituting the clients.
	_, err := target.must(ctx, true, `set -e
etcd_id=$(crictl ps --name "^etcd$" -q)
pid=$(crictl inspect -o go-template --template "{{.info.pid}}" "$etcd_id")
cp "/proc/$pid/root/usr/local/bin/etcdctl" /usr/local/bin/etcdctl
base=/usr/local/lib/k3s-maintenance-test/server/tls/etcd
mkdir -p "$base"
ln -s /etc/kubernetes/pki/etcd/ca.crt "$base/server-ca.crt"
ln -s /etc/kubernetes/pki/etcd/healthcheck-client.crt "$base/client.crt"
ln -s /etc/kubernetes/pki/etcd/healthcheck-client.key "$base/client.key"
cat >/usr/local/lib/k3s-maintenance-test/k3s <<'SCRIPT'
#!/bin/sh
shift
exec /usr/bin/kubectl "$@"
SCRIPT
chmod +x /usr/local/lib/k3s-maintenance-test/k3s`)
	if err != nil {
		t.Fatal(err)
	}

	// The command path is the same for one or many etcd voters. Unit tests cover the
	// resource minimum of three and member failures so this test needs only one kind node.
	args := K3sMaintenanceArgs{
		NodeName: node, Release: kindVersion, EtcdMembers: 1, TimeoutSeconds: ptr(30),
		KubeconfigPath: ptr("/etc/kubernetes/admin.conf"), EtcdDataDir: ptr("/usr/local/lib/k3s-maintenance-test"),
		K3sPath: ptr("/usr/local/lib/k3s-maintenance-test/k3s"),
	}
	kubectl := func(cmd ...string) string {
		t.Helper()
		out, err := target.must(ctx, true, args.kubectl(cmd...))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	if err := args.probeHealth(ctx, target, kindVersion); err != nil {
		t.Fatalf("initial kind health check: %v", err)
	}
	if got := kubectl("get", "node", node, "-o", "jsonpath={.spec.taints[?(@.key=='node-role.kubernetes.io/control-plane')].effect}"); got == "NoSchedule" {
		kubectl("taint", "node", node, "node-role.kubernetes.io/control-plane:NoSchedule-")
	}
	kubectl("create", "deployment", "workload", "--image=registry.k8s.io/pause:3.10")
	kubectl("rollout", "status", "deployment/workload", "--timeout=45s")
	oldUID := kubectl("get", "pod", "-l", "app=workload", "-o", "jsonpath={.items[0].metadata.uid}")
	kubectl("create", "poddisruptionbudget", "workload", "--min-available=1", "--selector=app=workload")
	kubectl("wait", "--for=jsonpath={.status.currentHealthy}=1", "pdb/workload", "--timeout=30s")

	blocked := args
	blocked.TimeoutSeconds = ptr(5)
	if err := blocked.drain(ctx, target); err == nil {
		t.Fatal("drain succeeded despite a PodDisruptionBudget that permits zero evictions")
	}
	if uid := kubectl("get", "pod", "-l", "app=workload", "-o", "jsonpath={.items[0].metadata.uid}"); uid == "" || uid != oldUID {
		t.Fatalf("the disruption budget did not preserve the original pod, uid = %q", uid)
	}
	if got := kubectl("get", "node", node, "-o", "jsonpath={.spec.unschedulable}"); got != "true" {
		t.Fatalf("the drain did not cordon the node, unschedulable = %q", got)
	}

	kubectl("delete", "pdb", "workload")
	if err := args.drain(ctx, target); err != nil {
		t.Fatal(err)
	}
	pods := kubectl("get", "pods", "--field-selector=spec.nodeName="+node, "-l", "app=workload", "-o", "name")
	if pods != "" {
		t.Fatalf("workload was not evicted: %q", pods)
	}
	gate := K3sHealthGateArgs{K3sMaintenanceArgs: args, Uncordon: ptr(true)}
	if err := gate.pass(ctx, target); err != nil {
		t.Fatal(err)
	}
	if got := kubectl("get", "node", node, "-o", "jsonpath={.spec.unschedulable}"); got == "true" {
		t.Fatal("the successful health gate did not uncordon the node")
	}
	kubectl("rollout", "status", "deployment/workload", "--timeout=45s")
}
