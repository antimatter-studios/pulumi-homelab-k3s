package homelabk3s

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

type contaboResponse struct {
	method, url, body, requestBody string
	status                         int
}

type httpRoundTripFunc func(*http.Request) (*http.Response, error)

func (f httpRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func contaboResponses(t *testing.T, responses ...contaboResponse) *http.Client {
	t.Helper()
	next := 0
	t.Cleanup(func() {
		if next != len(responses) {
			t.Errorf("received %d Contabo requests, expected %d", next, len(responses))
		}
	})
	return &http.Client{Transport: httpRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if next >= len(responses) {
			t.Errorf("unexpected request: %s %s", req.Method, req.URL)
			return nil, fmt.Errorf("unexpected Contabo request")
		}
		expected := responses[next]
		next++
		if req.Method != expected.method || req.URL.String() != expected.url {
			t.Errorf("request = %s %s, want %s %s", req.Method, req.URL, expected.method, expected.url)
		}

		contentType := "application/json"
		if expected.url == contaboAuthURL {
			contentType = "application/x-www-form-urlencoded"
		} else {
			for _, header := range []string{"X-Request-ID", "X-Trace-ID"} {
				if _, err := uuid.Parse(req.Header.Get(header)); err != nil {
					t.Errorf("invalid %s: %v", header, err)
				}
			}
			if req.Header.Get("Authorization") != "Bearer token" {
				t.Error("request missing the bearer token")
			}
		}
		if req.Header.Get("Content-Type") != contentType {
			t.Errorf("unexpected Content-Type: %s", req.Header.Get("Content-Type"))
		}
		if expected.requestBody != "" {
			data, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			if contentType == "application/json" {
				var got, want any
				if json.Unmarshal(data, &got) != nil || json.Unmarshal([]byte(expected.requestBody), &want) != nil || !reflect.DeepEqual(got, want) {
					t.Errorf("sent %s, want %s", data, expected.requestBody)
				}
			} else if string(data) != expected.requestBody {
				t.Errorf("sent %q, want %q", data, expected.requestBody)
			}
		}
		status := expected.status
		if status == 0 {
			status = 200
		}
		return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)),
			Body: io.NopCloser(strings.NewReader(expected.body))}, nil
	})}
}

var testContaboCredentials = ContaboCredentialsArgs{ClientID: ptr("client"), ClientSecret: ptr("secret"), User: ptr("operator"), Pass: ptr("password")}

var testContaboAuth = contaboResponse{
	method: "POST", url: contaboAuthURL, body: `{"access_token":"token"}`,
	requestBody: "client_id=client&client_secret=secret&grant_type=password&password=password&username=operator",
}

func TestContaboValidation(t *testing.T) {
	image := contaboResponse{
		method: "GET", url: contaboAPIURL + "/compute/images?size=100&page=1",
		body: `{"data":[{"name":" Ubuntu 24 ","imageId":"image","extra":true}]}`,
	}
	instance := contaboResponse{
		method: "GET", url: contaboInstanceURL("42"),
		body: `{"data":[{"instanceId":42,"ipConfig":{"v4":{"ip":"primary"},"v6":{"ip":"v6"}},"ipAddress":"secondary",
		"dataCenter":"primary-dc","datacenter":"secondary-dc","diskMb":"12","ramMb":"2048","sshKeys":[1,"two"],"extra":true}]}`,
	}
	readInstance := instance
	readInstance.body = `{"data":{"instanceId":"42","ipv4":"fallback","datacenter":"fallback-dc"}}`
	api := contaboAPI{client: contaboResponses(t, testContaboAuth, instance, image, testContaboAuth, readInstance, image,
		testContaboAuth, readInstance)}
	r := ContaboInstanceValidation{api: api}
	args := ContaboInstanceValidationArgs{ContaboCredentialsArgs: testContaboCredentials, InstanceID: "42", OSImage: ptr(" UBUNTU 24 ")}
	created, err := r.Create(t.Context(), infer.CreateRequest[ContaboInstanceValidationArgs]{Inputs: args})
	if err != nil || created.ID != "42" {
		t.Fatalf("create validation: %+v, %v", created, err)
	}
	fp := created.Output.Fingerprint
	for key, want := range map[string]any{"instanceIdNumeric": float64(42), "instanceId": "42", "ipv4": "primary",
		"ipv6": "v6", "datacenter": "primary-dc", "diskMb": nil, "ramMb": "2048", "sshKeys": []any{float64(1), "two"}} {
		if !reflect.DeepEqual(fp[key], want) {
			t.Errorf("fingerprint[%s] = %#v, want %#v", key, fp[key], want)
		}
	}
	if jsonField(created.Output.Raw, "extra") != true || jsonField(created.Output.ResolvedImage, "extra") != true {
		t.Fatal("API responses lost extra fields")
	}
	diff := func(state ContaboInstanceValidationState, inputs ContaboInstanceValidationArgs) bool {
		t.Helper()
		resp, err := r.Diff(t.Context(), infer.DiffRequest[ContaboInstanceValidationArgs, ContaboInstanceValidationState]{State: state, Inputs: inputs})
		if err != nil {
			t.Fatal(err)
		}
		return resp.HasChanges
	}
	if diff(created.Output, args) {
		t.Fatal("matching validation with a resolved image should have a clean diff")
	}
	missingImage := created.Output
	missingImage.ResolvedImage = nil
	if !diff(missingImage, args) {
		t.Fatal("a declared image without a recorded resolution must update the resource")
	}
	read, err := r.Read(t.Context(), infer.ReadRequest[ContaboInstanceValidationArgs, ContaboInstanceValidationState]{
		ID: "42", Inputs: args, State: created.Output,
	})
	if err != nil || read.State.Fingerprint["ipv4"] != "fallback" || read.State.Fingerprint["instanceIdNumeric"] != nil {
		t.Fatalf("read validation: %+v, %v", read, err)
	}
	args.OSImage = nil
	if !diff(read.State, args) {
		t.Fatal("removing the image must update the resource")
	}
	updated, err := r.Update(t.Context(), infer.UpdateRequest[ContaboInstanceValidationArgs, ContaboInstanceValidationState]{
		ID: "42", Inputs: args, State: created.Output,
	})
	if err != nil || updated.Output.OSImage != nil || updated.Output.ResolvedImage != nil {
		t.Fatalf("update validation: %+v, %v", updated, err)
	}
	if diff(updated.Output, args) {
		t.Fatal("matching validation without an image should have a clean diff")
	}
}

func TestContaboImageRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, query, body, want string
	}{
		{"empty", " \t ", "", "findImageByName: query must be non-empty"},
		{"missing", "Other", `{"data":[{"name":" Ubuntu 24 "},{"name":"Debian"}]}`,
			"No Contabo image matched name 'Other'. First 2 image names: Ubuntu 24 | Debian"},
		{"ambiguous", "Ubuntu", `{"data":[{"name":" ubuntu ","id":"first"},{"name":"Ubuntu","imageId":"second"}]}`,
			"Ambiguous Contabo image name 'Ubuntu' (matched 2 images). Matching imageIds: first, second"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var responses []contaboResponse
			if tc.body != "" {
				responses = append(responses, contaboResponse{method: "GET", url: contaboAPIURL + "/compute/images?size=100&page=1", body: tc.body})
			}
			api := contaboAPI{client: contaboResponses(t, responses...)}
			if _, err := api.image(t.Context(), "token", tc.query); err == nil || err.Error() != tc.want {
				t.Fatalf("image lookup error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestContaboReinstall(t *testing.T) {
	for _, tc := range []struct {
		name, failKubectl, listed, secretMethod, secretPath, secretRequest, secretResponse, putRequest string
		wantSecretID                                                                                   float64
		defaultUser                                                                                    *string
		secretStatus                                                                                   int
	}{
		{"new-secret", "cordon", `{"data":[{"secretId":88,"name":"other"}]}`, "POST", "/secrets",
			`{"name":"pulumi-node-42","type":"ssh","value":"ssh-ed25519 test"}`, `{"data":[{"secretId":7}]}`,
			`{"imageId":"image","sshKeys":[7],"defaultUser":"admin","userData":"#cloud-config"}`, 7, nil, 0},
		{"existing-secret", "get", `{"data":[{"secretId":8,"name":"pulumi-node-42"}]}`, "PATCH", "/secrets/8",
			`{"name":"pulumi-node-42","value":"ssh-ed25519 test"}`, `{"secretId":99}`,
			`{"imageId":"image","sshKeys":[8],"defaultUser":"root","userData":"#cloud-config"}`, 8, ptr("root"), 0},
		{"empty-secret-patch", "get", `{"data":[{"secretId":8,"name":"pulumi-node-42"}]}`, "PATCH", "/secrets/8",
			`{"name":"pulumi-node-42","value":"ssh-ed25519 test"}`, "",
			`{"imageId":"image","sshKeys":[8],"defaultUser":"admin","userData":"#cloud-config"}`, 8, nil, http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := fakeKubectl(t)
			t.Setenv("KUBECTL_FAIL", tc.failKubectl)
			kubeconfig := filepath.Join(t.TempDir(), "cluster-config")
			if err := os.WriteFile(kubeconfig, []byte("config"), 0600); err != nil {
				t.Fatal(err)
			}
			api := contaboAPI{client: contaboResponses(t, testContaboAuth,
				contaboResponse{method: "GET", url: contaboAPIURL + "/compute/images?size=100&page=1", body: `{"data":[{"name":"Ubuntu","imageId":"image"}]}`},
				contaboResponse{method: "GET", url: contaboAPIURL + "/secrets?name=pulumi-node-42&type=ssh", body: tc.listed},
				contaboResponse{method: tc.secretMethod, url: contaboAPIURL + tc.secretPath, body: tc.secretResponse, requestBody: tc.secretRequest, status: tc.secretStatus},
				contaboResponse{method: "PUT", url: contaboInstanceURL("42"), body: `{"data":[]}`, requestBody: tc.putRequest})}

			r := ContaboInstanceReinstall{api: api}
			args := ContaboInstanceReinstallArgs{
				ContaboCredentialsArgs: testContaboCredentials, InstanceID: "42", OSImage: ptr("Ubuntu"),
				SSHPublicKey: ptr("ssh-ed25519 test"), ReinstallToken: "cycle-1",
				ContaboReinstallOptions: ContaboReinstallOptions{DefaultUser: tc.defaultUser, UserData: ptr("#cloud-config"),
					DrainBeforeReinstall: ptr(true), Kubeconfig: &kubeconfig, NodeName: ptr("worker"), DrainTimeoutSeconds: ptr(20.0)},
			}
			result, err := r.Create(t.Context(), infer.CreateRequest[ContaboInstanceReinstallArgs]{Inputs: args})
			if err != nil || result.ID != "42-cycle-1" || result.Output.ReinstalledAt == nil ||
				valueOr(result.Output.SSHKeySecretID, 0) != tc.wantSecretID || valueOr(result.Output.ImageID, "") != "image" {
				t.Fatalf("reinstall = %+v, %v", result, err)
			}

			want := "--kubeconfig " + kubeconfig + " get node worker --request-timeout=5s\n"
			if tc.failKubectl != "get" {
				want += "--kubeconfig " + kubeconfig + " cordon worker\n"
				want += "--kubeconfig " + kubeconfig + " drain worker --ignore-daemonsets --delete-emptydir-data --force --disable-eviction --timeout=20s\n"
			}
			if data, err := os.ReadFile(log); err != nil || string(data) != want {
				t.Fatalf("kubectl calls = %q, %v; want %q", data, err, want)
			}

			args.OSImage = ptr("Debian")
			diff, err := r.Diff(t.Context(), infer.DiffRequest[ContaboInstanceReinstallArgs, ContaboInstanceReinstallState]{State: result.Output, Inputs: args})
			if err != nil || diff.HasChanges {
				t.Fatalf("image-only diff = %+v, %v", diff, err)
			}
			args.ReinstallToken = "cycle-2"
			diff, err = r.Diff(t.Context(), infer.DiffRequest[ContaboInstanceReinstallArgs, ContaboInstanceReinstallState]{State: result.Output, Inputs: args})
			if err != nil || !diff.HasChanges || diff.DetailedDiff["reinstallToken"].Kind != p.UpdateReplace {
				t.Fatalf("token diff = %+v, %v", diff, err)
			}
		})
	}
}

func TestContaboValidationRPCProperties(t *testing.T) {
	server := vpsServer(t, contaboResponses(t, testContaboAuth,
		contaboResponse{method: "GET", url: contaboInstanceURL("42"), body: `{"instanceId":42,"name":"server","ipConfig":{"v4":{"ip":"example"}}}`}), property.Map{})
	urn := resourceURN("ContaboInstanceValidation")
	inputs := property.NewMap(map[string]property.Value{
		"instanceId": property.New("42"), "clientId": property.New("client").WithSecret(true),
		"clientSecret": property.New("secret").WithSecret(true), "user": property.New("operator").WithSecret(true),
		"pass": property.New("password").WithSecret(true),
	})
	for _, dryRun := range []bool{true, false} {
		result, err := server.Create(p.CreateRequest{Urn: urn, DryRun: dryRun, Properties: inputs})
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"clientId", "clientSecret", "user", "pass"} {
			if !result.Properties.Get(key).IsSecret() {
				t.Errorf("%s was not preserved as a secret", key)
			}
		}
		fingerprint := result.Properties.Get("fingerprint")
		if dryRun {
			if !fingerprint.IsComputed() {
				t.Errorf("preview fingerprint is not computed: %v", fingerprint)
			}
		} else if !fingerprint.IsMap() || !fingerprint.AsMap().Get("ipv4").Equals(property.New("example")) {
			t.Errorf("unexpected fingerprint %v", fingerprint)
		}
	}
}

// The images list is paginated: an image past the first page is still found.
func TestContaboImageOnALaterPage(t *testing.T) {
	var first []string
	for i := 0; i < 10; i++ {
		first = append(first, fmt.Sprintf(`{"name":"other-%d","imageId":"o%d"}`, i, i))
	}
	api := contaboAPI{client: contaboResponses(t,
		contaboResponse{method: "GET", url: contaboAPIURL + "/compute/images?size=100&page=1",
			body: `{"data":[` + strings.Join(first, ",") + `],"_pagination":{"page":1,"size":10,"totalPages":2}}`},
		contaboResponse{method: "GET", url: contaboAPIURL + "/compute/images?size=100&page=2",
			body: `{"data":[{"name":"debian-13","imageId":"debian"}],"_pagination":{"page":2,"size":10,"totalPages":2}}`},
	)}
	image, err := api.image(t.Context(), "token", "Debian-13")
	if err != nil || jsonField(image, "imageId") != "debian" {
		t.Fatalf("image = %v, %v", image, err)
	}
}
