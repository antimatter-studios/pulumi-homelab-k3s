package homelabk3s

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

func TestLocalFileDelete(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("file", []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	r := LocalFileDelete{}
	req := infer.CreateRequest[LocalFileArgs]{Inputs: LocalFileArgs{Path: "file"}}
	for _, mode := range []string{"present", "absent"} {
		result, err := r.Create(t.Context(), req)
		path := filepath.Join(dir, "file")
		if err != nil || result.Output.Path != path || result.Output.DeletedAt == nil {
			t.Fatalf("delete %s file: %+v, %v", mode, result, err)
		}
		if decoded, err := vpsTimedIDValue("del", result.ID); err != nil || decoded != path {
			t.Fatalf("delete ID: %q, %v", result.ID, err)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("file was not deleted: %v", err)
		}
	}
	if err := os.Mkdir("directory", 0700); err != nil {
		t.Fatal(err)
	}
	req.Inputs.Path = "directory"
	if _, err := r.Create(t.Context(), req); err == nil {
		t.Fatal("delete accepted a directory")
	}
}

func TestLocalFileRotate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kube-config")
	for name, contents := range map[string]string{path: "fresh", path + ".1.old": "previous"} {
		if err := os.WriteFile(name, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	r := LocalFileRotate{}
	req := infer.CreateRequest[LocalFileArgs]{Inputs: LocalFileArgs{Path: path}}
	rotated, err := r.Create(t.Context(), req)
	preserved := path + ".2.old"
	if err != nil || rotated.Output.Path != path || valueOr(rotated.Output.RotatedTo, "") != preserved || rotated.Output.RotatedAt == nil {
		t.Fatalf("rotate: %+v, %v", rotated, err)
	}
	missing, err := r.Create(t.Context(), req)
	if err != nil || missing.Output.RotatedTo != nil || missing.Output.RotatedAt != nil {
		t.Fatalf("rotate absent file: %+v, %v", missing, err)
	}
	for name, want := range map[string]string{preserved: "fresh", path + ".1.old": "previous"} {
		if contents, err := os.ReadFile(name); err != nil || string(contents) != want {
			t.Errorf("read %s = %q, %v", name, contents, err)
		}
	}
}

func TestLocalFilePreviewPreservesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	server := vpsServer(t, nil)
	for typ, output := range map[string]string{"LocalFileDelete": "deletedAt", "LocalFileRotate": "rotatedTo"} {
		result, err := server.Create(p.CreateRequest{Urn: resourceURN(typ), DryRun: true,
			Properties: property.NewMap(map[string]property.Value{"path": property.New(path)})})
		if err != nil || !result.Properties.Get(output).IsComputed() {
			t.Fatalf("%s preview: %+v, %v", typ, result, err)
		}
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "keep" {
		t.Fatalf("preview changed the file: %q, %v", data, err)
	}
}
