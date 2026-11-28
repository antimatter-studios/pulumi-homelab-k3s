package homelabk3s

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/antimatter-studios/pulumi-homelab/provider/remote"
	"github.com/antimatter-studios/pulumi-homelab/provider/remote/remotetest"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const (
	testRelease = "v1.36.4+k3s1"
	testMembers = `{"members":[
		{"name":"s2","clientURLs":["https://s2:2379"]},
		{"name":"s3","clientURLs":["https://s3:2379"]},
		{"name":"s4","clientURLs":["https://s4:2379"]},
		{"name":"s5","clientURLs":["https://s5:2379"]}
	]}`
	testEndpoints = "https://s2:2379,https://s3:2379,https://s4:2379,https://s5:2379"
	testHealth    = `[
		{"endpoint":"https://s5:2379","health":true},
		{"endpoint":"https://s3:2379","health":true},
		{"endpoint":"https://s4:2379","health":true},
		{"endpoint":"https://s2:2379","health":true}
	]`
)

func readyNode(name, version string) string {
	return fmt.Sprintf(`{"metadata":{"name":%q},"status":{"conditions":[{"type":"Ready","status":"True"}],"nodeInfo":{"kubeletVersion":%q}}}`, name, version)
}

func clusterReply(nodeJSON, members, health string) *remotetest.Runner {
	a := K3sMaintenanceArgs{}
	return remotetest.New().
		On(a.kubectl("--request-timeout=5s", "get", "nodes", "-o", "json"), remotetest.Reply{Stdout: `{"items":[` + nodeJSON + `]}`}).
		On(a.etcdctl("https://127.0.0.1:2379", "member", "list"), remotetest.Reply{Stdout: members}).
		On(a.etcdctl(testEndpoints, "endpoint", "health"), remotetest.Reply{Stdout: health})
}

func maintenanceInputs() property.Map {
	return property.NewMap(map[string]property.Value{
		"nodeName": property.New("s2"), "release": property.New(testRelease), "etcdMembers": property.New(4.0),
		"timeoutSeconds": property.New(1.0),
	})
}

func TestMaintenanceGateBlocksMutation(t *testing.T) {
	targetNode := readyNode("s2", testRelease)
	for _, tc := range []struct{ name, resource, nodes, members, health, failure string }{
		{"missing", "K3sDrain", readyNode("other", testRelease), testMembers, testHealth, "s2 is missing"},
		{"not ready", "K3sDrain", targetNode + "," + strings.Replace(readyNode("s3", testRelease), "True", "Unknown", 1), testMembers, testHealth, "s3 is not Ready"},
		{"old release", "K3sHealthGate", readyNode("s2", "v1.36.3+k3s1"), testMembers, testHealth, "waiting for " + testRelease},
		{"missing member", "K3sDrain", targetNode, `{"members":[]}`, testHealth, "waiting for 4"},
		{"learner", "K3sDrain", targetNode, strings.Replace(testMembers, `"name":"s3"`, `"name":"s3","isLearner":true`, 1), testHealth, "not a started voter"},
		{"unhealthy endpoint", "K3sDrain", targetNode, testMembers, strings.Replace(testHealth, "true", "false", 1), "is unhealthy"},
		{"missing endpoint response", "K3sHealthGate", targetNode, testMembers, `[]`, "did not report health"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := clusterReply(tc.nodes, tc.members, tc.health)
			server := testServer(t, fake)
			inputs := maintenanceInputs()
			if tc.resource == "K3sHealthGate" {
				inputs = inputs.Set("uncordon", property.New(true))
			}
			_, err := server.Create(p.CreateRequest{Urn: resourceURN(tc.resource), Properties: inputs})
			if err == nil || !strings.Contains(err.Error(), tc.failure) {
				t.Fatalf("create error = %v, want %q", err, tc.failure)
			}
			if fake.Ran("drain") || fake.Ran("uncordon") {
				t.Fatalf("unhealthy cluster ran a mutation: %v", fake.Commands())
			}
		})
	}
}

func TestDrainCheckpointByRelease(t *testing.T) {
	a := K3sMaintenanceArgs{NodeName: "s2"}
	drain := a.kubectl("drain", "s2", "--ignore-daemonsets", "--timeout=1s")
	fake := clusterReply(readyNode("s2", testRelease), testMembers, testHealth).On(drain, remotetest.Reply{})
	server := testServer(t, fake)
	urn, inputs := resourceURN("K3sDrain"), maintenanceInputs()
	preview, err := server.Create(p.CreateRequest{Urn: urn, Properties: inputs, DryRun: true})
	if err != nil || !preview.Properties.Get("approvedRelease").HasComputed() || len(fake.Commands()) > 0 {
		t.Fatalf("preview %v, error %v, commands %v", preview, err, fake.Commands())
	}
	created, err := server.Create(p.CreateRequest{Urn: urn, Properties: inputs})
	if err != nil || !created.Properties.Get("approvedRelease").Equals(property.New(testRelease)) {
		t.Fatalf("create = %v, %v", created, err)
	}
	read, err := server.Read(p.ReadRequest{Urn: urn, ID: created.ID, Inputs: inputs, Properties: created.Properties})
	if err != nil || read.ID != created.ID || !read.Properties.Get("approvedRelease").Equals(property.New(testRelease)) {
		t.Fatalf("read = %v, %v", read, err)
	}
	if _, err := server.Read(p.ReadRequest{Urn: urn, ID: "s2"}); err == nil || !strings.Contains(err.Error(), "cannot be imported") {
		t.Fatalf("ID-only read error = %v", err)
	}

	changed := inputs.Set("timeoutSeconds", property.New(2.0))
	updated, err := server.Update(p.UpdateRequest{Urn: urn, ID: created.ID, State: created.Properties, Inputs: changed})
	if err != nil || !updated.Properties.Get("completedAt").Equals(created.Properties.Get("completedAt")) {
		t.Fatalf("settings update = %v, %v", updated, err)
	}
	if got := fake.Commands(); len(got) != 4 || got[3] != drain {
		t.Fatalf("commands = %v", got)
	}

	next := property.New("v1.36.5+k3s1")
	fake.On(a.kubectl("drain", "s2", "--ignore-daemonsets", "--timeout=2s"), remotetest.Reply{})
	updated, err = server.Update(p.UpdateRequest{Urn: urn, ID: created.ID, State: updated.Properties, Inputs: changed.Set("release", next)})
	if err != nil || !updated.Properties.Get("approvedRelease").Equals(next) || len(fake.Commands()) != 8 {
		t.Fatalf("release update = %v, %v, commands = %v", updated, err, fake.Commands())
	}
}

type rollingMock struct {
	register func(pulumi.MockResourceArgs) (string, resource.PropertyMap, error)
}

func (m rollingMock) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	return m.register(args)
}

func (rollingMock) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return args.Args, nil
}

// Verify the ordering and property dependency emitted by the documented declarations with the SDK.
func TestRollingMaintenanceChain(t *testing.T) {
	type drainState struct {
		pulumi.CustomResourceState
		ApprovedRelease pulumi.StringOutput `pulumi:"approvedRelease"`
	}
	nodes := []string{"s2", "s3", "s4", "s5"}
	mocks := rollingMock{register: func(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
		if !args.Custom {
			return args.Name, args.Inputs, nil
		}
		phase, node, _ := strings.Cut(args.Name, "-")
		deps := []string{}
		for _, urn := range args.RegisterRPC.GetDependencies() {
			deps = append(deps, string(resource.URN(urn).Name()))
		}
		switch phase {
		case "drain":
			if i := slices.Index(nodes, node); i > 0 && !slices.Contains(deps, "gate-"+nodes[i-1]) {
				return "", nil, fmt.Errorf("%s must depend on completion of %s, got %v", args.Name, nodes[i-1], deps)
			}
			args.Inputs["approvedRelease"] = resource.NewStringProperty(testRelease)
		case "binary":
			if want := "drain-" + node; !slices.Contains(deps, want) ||
				args.Inputs["version"] != resource.NewStringProperty(testRelease) {
				return "", nil, fmt.Errorf("%s must take its release from %s", args.Name, want)
			}
		case "server":
			if !slices.Contains(deps, "binary-"+node) {
				return "", nil, fmt.Errorf("%s must run after its binary", args.Name)
			}
		case "gate":
			if !slices.Contains(deps, "server-"+node) || !slices.Contains(deps, "binary-"+node) {
				return "", nil, fmt.Errorf("%s must run after both machine resources", args.Name)
			}
		}
		return args.Name, args.Inputs, nil
	}}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		var previous pulumi.Resource
		for _, n := range nodes {
			args := pulumi.Map{"nodeName": pulumi.String(n), "release": pulumi.String(testRelease), "etcdMembers": pulumi.Int(4)}
			var opts []pulumi.ResourceOption
			if previous != nil {
				opts = append(opts, pulumi.DependsOn([]pulumi.Resource{previous}))
			}
			drain := &drainState{}
			if err := ctx.RegisterResource(Name+":index:K3sDrain", "drain-"+n, args, drain, opts...); err != nil {
				return err
			}
			bin, server, gate := &pulumi.CustomResourceState{}, &pulumi.CustomResourceState{}, &pulumi.CustomResourceState{}
			if err := ctx.RegisterResource(Name+":index:K3sBinary", "binary-"+n, pulumi.Map{"version": drain.ApprovedRelease}, bin); err != nil {
				return err
			}
			if err := ctx.RegisterResource(Name+":index:K3sServer", "server-"+n, nil, server, pulumi.DependsOn([]pulumi.Resource{bin})); err != nil {
				return err
			}
			if err := ctx.RegisterResource(Name+":index:K3sHealthGate", "gate-"+n, args, gate, pulumi.DependsOn([]pulumi.Resource{bin, server})); err != nil {
				return err
			}
			previous = gate
		}
		return nil
	}, pulumi.WithMocks("rolling", "test", mocks))
	if err != nil {
		t.Fatal(err)
	}
}

func TestHealthWaitRetriesTransientFailure(t *testing.T) {
	a := K3sMaintenanceArgs{NodeName: "s2", EtcdMembers: 4, TimeoutSeconds: ptr(1)}
	command := a.kubectl("--request-timeout=5s", "get", "nodes", "-o", "json")
	r := remotetest.New().Once(command, remotetest.Reply{Code: 1, Stderr: "API unavailable"}).
		On(command, remotetest.Reply{Stdout: `{"items":[` + readyNode("s2", testRelease) + `]}`}).
		On(a.etcdctl("https://127.0.0.1:2379", "member", "list"), remotetest.Reply{Stdout: testMembers}).
		On(a.etcdctl(testEndpoints, "endpoint", "health"), remotetest.Reply{Stdout: testHealth})
	if err := a.waitHealthy(t.Context(), &target{runner: r, cfg: remote.Config{Escalation: "none"}}, testRelease, 1); err != nil {
		t.Fatal(err)
	}
	if got := r.Commands(); len(got) != 4 || got[0] != got[1] {
		t.Fatalf("retry commands = %v", got)
	}
}

func TestMaintenanceValidatesBeforeConnecting(t *testing.T) {
	for _, tc := range []struct {
		key, message string
		value        property.Value
	}{
		{"nodeName", "invalid Kubernetes node name", property.New("--all")},
		{"release", "must be a pinned version", property.New("stable")},
		{"etcdMembers", "at least three voting etcd members", property.New(2.0)},
		{"timeoutSeconds", "invalid timeoutSeconds", property.New(-1.0)},
		{"kubeconfigPath", "paths must be absolute", property.New("relative.yaml")},
	} {
		t.Run(tc.key, func(t *testing.T) {
			fake := remotetest.New()
			server := testServer(t, fake)
			_, err := server.Create(p.CreateRequest{Urn: resourceURN("K3sDrain"), Properties: maintenanceInputs().Set(tc.key, tc.value)})
			if err == nil || !strings.Contains(err.Error(), tc.message) || len(fake.Commands()) != 0 {
				t.Fatalf("invalid %s reported %v after commands %v", tc.key, err, fake.Commands())
			}
		})
	}
}
