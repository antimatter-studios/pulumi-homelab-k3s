package homelabk3s

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

func fakeKubectl(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KUBECTL_LOG", log)
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$KUBECTL_LOG"
if [ "$3" = "$KUBECTL_FAIL" ]; then
	exit 1
fi
`
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return log
}

func TestCordonAndDrainBarrier(t *testing.T) {
	for _, failed := range []string{"", "cordon", "drain"} {
		t.Run("fail-"+failed, func(t *testing.T) {
			log := fakeKubectl(t)
			t.Setenv("KUBECTL_FAIL", failed)
			r := CordonAndDrainBarrier{}
			args := CordonAndDrainBarrierArgs{Kubeconfig: ptr("cluster-config"), NodeName: "worker-east", ReinstallToken: ptr("old")}
			req := infer.CreateRequest[CordonAndDrainBarrierArgs]{Inputs: args}
			result, err := r.Create(t.Context(), req)
			if failed == "" {
				if err != nil || result.Output.CompletedAt == nil || !strings.HasPrefix(result.ID, "drain-worker-east-") {
					t.Fatalf("create = %+v, %v", result, err)
				}
				diff, err := r.Diff(t.Context(), infer.DiffRequest[CordonAndDrainBarrierArgs, CordonAndDrainBarrierState]{State: result.Output, Inputs: args})
				if err != nil || diff.HasChanges {
					t.Fatalf("matching diff = %+v, %v", diff, err)
				}
				args.ReinstallToken = ptr("new")
				diff, err = r.Diff(t.Context(), infer.DiffRequest[CordonAndDrainBarrierArgs, CordonAndDrainBarrierState]{State: result.Output, Inputs: args})
				if err != nil || !diff.HasChanges || diff.DeleteBeforeReplace || diff.DetailedDiff["reinstallToken"].Kind != p.UpdateReplace {
					t.Fatalf("token diff = %+v, %v", diff, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "Failed to "+failed+" node worker-east") {
				t.Fatalf("create failure = %v", err)
			}
			want := "--kubeconfig cluster-config cordon worker-east\n"
			if failed != "cordon" {
				want += "--kubeconfig cluster-config drain worker-east --ignore-daemonsets --delete-emptydir-data --force --disable-eviction --timeout=300s\n"
			}
			if data, err := os.ReadFile(log); err != nil || string(data) != want {
				t.Fatalf("kubectl calls = %q, %v; want %q", data, err, want)
			}
		})
	}
}
