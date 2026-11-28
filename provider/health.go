package homelabk3s

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/antimatter-studios/pulumi-homelab/provider/shell"
	"github.com/blang/semver"
	"github.com/pulumi/pulumi-go-provider/infer"
)

type K3sMaintenanceArgs struct {
	NodeName       string  `pulumi:"nodeName" provider:"replaceOnChanges"`
	Release        string  `pulumi:"release"`
	EtcdMembers    int     `pulumi:"etcdMembers"`
	KubeconfigPath *string `pulumi:"kubeconfigPath,optional"`
	EtcdDataDir    *string `pulumi:"etcdDataDir,optional"`
	K3sPath        *string `pulumi:"k3sPath,optional"`
	TimeoutSeconds *int    `pulumi:"timeoutSeconds,optional"`
}

func (a *K3sMaintenanceArgs) Annotate(annotator infer.Annotator) {
	annotator.Describe(&a.NodeName, "Name of the Kubernetes node to maintain.")
	annotator.Describe(&a.Release, "Pinned release that triggers maintenance. The post-upgrade gate waits for this exact kubelet version.")
	annotator.Describe(&a.EtcdMembers, "Expected number of embedded etcd voters. At least three are required.")
	annotator.Describe(&a.KubeconfigPath, "Kubeconfig on the provider host. Defaults to /etc/rancher/k3s/k3s.yaml.")
	annotator.Describe(&a.EtcdDataDir, "k3s data directory on the provider host. Defaults to /var/lib/rancher/k3s.")
	annotator.Describe(&a.K3sPath, "k3s executable on the provider host. Defaults to /usr/local/bin/k3s.")
	annotator.Describe(&a.TimeoutSeconds, "Deadline in seconds for each health wait and maintenance command. Defaults to 300.")
}

var nodeNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

func (a K3sMaintenanceArgs) validate() error {
	if len(a.NodeName) > 253 || !nodeNamePattern.MatchString(a.NodeName) {
		return fmt.Errorf("invalid Kubernetes node name %q", a.NodeName)
	}
	version, hasPrefix := strings.CutPrefix(a.Release, "v")
	if _, err := semver.Parse(version); !hasPrefix || err != nil {
		return fmt.Errorf("release must be a pinned version such as v1.36.4+k3s1, got %q", a.Release)
	}
	if a.EtcdMembers < 3 {
		return fmt.Errorf("expected at least three voting etcd members, got %d", a.EtcdMembers)
	}
	seconds := int64(valueOr(a.TimeoutSeconds, 300))
	if seconds < 1 || seconds > math.MaxInt64/int64(time.Second) {
		return fmt.Errorf("invalid timeoutSeconds %d", seconds)
	}
	for _, p := range []*string{a.KubeconfigPath, a.EtcdDataDir, a.K3sPath} {
		if p != nil && !path.IsAbs(*p) {
			return fmt.Errorf("maintenance paths must be absolute on the provider host, got %q", *p)
		}
	}
	return nil
}

func (a K3sMaintenanceArgs) timeout() time.Duration {
	return time.Duration(valueOr(a.TimeoutSeconds, 300)) * time.Second
}

func (a K3sMaintenanceArgs) kubectl(args ...string) string {
	args = append([]string{valueOr(a.K3sPath, defaultBinary), "kubectl",
		"--kubeconfig", valueOr(a.KubeconfigPath, kubeconfigPath)}, args...)
	return shellCommand(args)
}

func (a K3sMaintenanceArgs) etcdctl(endpoints string, args ...string) string {
	dir := path.Join(valueOr(a.EtcdDataDir, defaultDataDir), "server/tls/etcd")
	args = append([]string{"etcdctl", "--endpoints=" + endpoints, "--cacert=" + path.Join(dir, "server-ca.crt"),
		"--cert=" + path.Join(dir, "client.crt"), "--key=" + path.Join(dir, "client.key"),
		"--dial-timeout=5s", "--command-timeout=5s", "--write-out=json"}, args...)
	return shellCommand(args)
}

func shellCommand(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shell.Quote(arg)
	}
	return strings.Join(quoted, " ")
}

func (a K3sMaintenanceArgs) probeHealth(ctx context.Context, t *target, expectedVersion string) error {
	out, err := t.must(ctx, true, a.kubectl("--request-timeout=5s", "get", "nodes", "-o", "json"))
	if err != nil {
		return err
	}
	if err := checkNodes(out, a.NodeName, expectedVersion); err != nil {
		return err
	}
	out, err = t.must(ctx, true, a.etcdctl("https://127.0.0.1:2379", "member", "list"))
	if err != nil {
		return err
	}
	endpoints, err := memberEndpoints(out, a.EtcdMembers)
	if err != nil {
		return err
	}
	out, err = t.must(ctx, true, a.etcdctl(strings.Join(endpoints, ","), "endpoint", "health"))
	if err != nil {
		return err
	}
	return checkEndpoints(out, endpoints)
}

func checkNodes(out, targetName, expectedVersion string) error {
	var nodes struct {
		Items []struct {
			Metadata struct {
				Name string
			}
			Status struct {
				Conditions []struct {
					Type, Status string
				}
				NodeInfo struct {
					KubeletVersion string
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &nodes); err != nil {
		return fmt.Errorf("decode Kubernetes nodes: %w", err)
	}
	found := false
	for _, node := range nodes.Items {
		name := node.Metadata.Name
		if !slices.ContainsFunc(node.Status.Conditions, func(condition struct{ Type, Status string }) bool {
			return condition.Type == "Ready" && condition.Status == "True"
		}) {
			return fmt.Errorf("node %s is not Ready", name)
		}
		if name == targetName {
			found = true
			if got := node.Status.NodeInfo.KubeletVersion; expectedVersion != "" && got != expectedVersion {
				return fmt.Errorf("node %s reports kubelet version %q, waiting for %s", name, got, expectedVersion)
			}
		}
	}
	if !found {
		return fmt.Errorf("node %s is missing from the cluster", targetName)
	}
	return nil
}

func memberEndpoints(out string, expected int) ([]string, error) {
	var list struct {
		Members []struct {
			Name       string
			ClientURLs []string
			IsLearner  bool
		}
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, fmt.Errorf("decode etcd member list: %w", err)
	}
	if len(list.Members) != expected {
		return nil, fmt.Errorf("etcd reports %d members, waiting for %d", len(list.Members), expected)
	}
	endpoints := make(map[string]bool, expected)
	for _, member := range list.Members {
		if member.Name == "" || member.IsLearner || len(member.ClientURLs) == 0 {
			return nil, fmt.Errorf("etcd member %q is not a started voter with a client endpoint", member.Name)
		}
		for _, endpoint := range member.ClientURLs {
			u, err := url.Parse(endpoint)
			if err != nil || u.Scheme != "https" || u.Host == "" || strings.Contains(endpoint, ",") {
				return nil, fmt.Errorf("etcd member %q advertises invalid TLS endpoint %q", member.Name, endpoint)
			}
			if endpoints[endpoint] {
				return nil, fmt.Errorf("etcd advertises duplicate endpoint %q", endpoint)
			}
			endpoints[endpoint] = true
		}
	}
	return slices.Sorted(maps.Keys(endpoints)), nil
}

func checkEndpoints(out string, expected []string) error {
	var checks []struct {
		Endpoint string
		Health   bool
	}
	if err := json.Unmarshal([]byte(out), &checks); err != nil {
		return fmt.Errorf("decode etcd endpoint health: %w", err)
	}
	healthy := make(map[string]bool, len(checks))
	for _, check := range checks {
		if !check.Health {
			return fmt.Errorf("etcd endpoint %s is unhealthy", check.Endpoint)
		}
		healthy[check.Endpoint] = true
	}
	for _, endpoint := range expected {
		if !healthy[endpoint] {
			return fmt.Errorf("etcd endpoint %s did not report health", endpoint)
		}
	}
	return nil
}

func (a K3sMaintenanceArgs) waitHealthy(ctx context.Context, t *target, version string, interval time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var lastCheck error
	for ctx.Err() == nil {
		err := a.probeHealth(ctx, t, version)
		if err == nil {
			return nil
		}
		if lastCheck == nil || ctx.Err() == nil {
			lastCheck = err
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	return fmt.Errorf("waiting for cluster health for %s: %w (last check: %v)", a.NodeName, ctx.Err(), lastCheck)
}
