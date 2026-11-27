package homelabk3s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

type LocalFileArgs struct {
	Path string `pulumi:"path"`
}

type LocalFileDelete struct {
	oneShot[LocalFileArgs, LocalFileDeleteState]
}

type LocalFileDeleteState struct {
	LocalFileArgs
	DeletedAt *string `pulumi:"deletedAt,optional"`
}

func (r *LocalFileDelete) Annotate(a infer.Annotator) {
	a.SetToken("index", "LocalFileDelete")
	a.Describe(r, "Delete a local file when the resource is created.")
}

func (LocalFileDelete) Create(_ context.Context, req infer.CreateRequest[LocalFileArgs]) (infer.CreateResponse[LocalFileDeleteState], error) {
	state := LocalFileDeleteState{LocalFileArgs: req.Inputs}
	if req.DryRun {
		state.DeletedAt = ptr("")
		return infer.CreateResponse[LocalFileDeleteState]{Output: state}, nil
	}

	path, err := filepath.Abs(req.Inputs.Path)
	if err != nil {
		return infer.CreateResponse[LocalFileDeleteState]{}, err
	}
	if localPathExists(path) {
		// unlink(2) refuses directories, like Node's unlinkSync.
		if err := syscall.Unlink(path); err != nil {
			return infer.CreateResponse[LocalFileDeleteState]{}, fmt.Errorf("Failed to delete file '%s': %w", path, err)
		}
	}

	state.Path = path
	state.DeletedAt = ptr(vpsTimestamp())
	return infer.CreateResponse[LocalFileDeleteState]{ID: vpsTimedID("del", path), Output: state}, nil
}

func (LocalFileDelete) Read(_ context.Context, req infer.ReadRequest[LocalFileArgs, LocalFileDeleteState]) (infer.ReadResponse[LocalFileArgs, LocalFileDeleteState], error) {
	if req.State.Path == "" {
		path, err := vpsTimedIDValue("del", req.ID)
		if err != nil {
			return infer.ReadResponse[LocalFileArgs, LocalFileDeleteState]{}, err
		}
		req.State.Path, req.Inputs.Path = path, path
	}
	return infer.ReadResponse[LocalFileArgs, LocalFileDeleteState]{ID: req.ID, Inputs: req.Inputs, State: req.State}, nil
}

func (LocalFileDelete) Diff(context.Context, infer.DiffRequest[LocalFileArgs, LocalFileDeleteState]) (p.DiffResponse, error) {
	return p.DiffResponse{}, nil
}

type LocalFileRotate struct {
	oneShot[LocalFileArgs, LocalFileRotateState]
}

type LocalFileRotateState struct {
	LocalFileArgs
	RotatedTo *string `pulumi:"rotatedTo,optional"`
	RotatedAt *string `pulumi:"rotatedAt,optional"`
}

func (r *LocalFileRotate) Annotate(a infer.Annotator) {
	a.SetToken("index", "LocalFileRotate")
	a.Describe(r, "Rename a local file to the next available .N.old path when the resource is created.")
}

func (LocalFileRotate) Create(_ context.Context, req infer.CreateRequest[LocalFileArgs]) (infer.CreateResponse[LocalFileRotateState], error) {
	state := LocalFileRotateState{LocalFileArgs: req.Inputs}
	if req.DryRun {
		state.RotatedTo, state.RotatedAt = ptr(""), ptr("")
		return infer.CreateResponse[LocalFileRotateState]{Output: state}, nil
	}

	path, err := filepath.Abs(req.Inputs.Path)
	if err != nil {
		return infer.CreateResponse[LocalFileRotateState]{}, err
	}
	if localPathExists(path) {
		for n := 1; ; n++ {
			candidate := fmt.Sprintf("%s.%d.old", path, n)
			if localPathExists(candidate) {
				continue
			}
			if err := os.Rename(path, candidate); err != nil {
				return infer.CreateResponse[LocalFileRotateState]{}, fmt.Errorf("Failed to rotate file '%s': %w", path, err)
			}
			state.RotatedTo, state.RotatedAt = &candidate, ptr(vpsTimestamp())
			break
		}
	}

	state.Path = path
	return infer.CreateResponse[LocalFileRotateState]{ID: vpsTimedID("rot", path), Output: state}, nil
}

func (LocalFileRotate) Read(_ context.Context, req infer.ReadRequest[LocalFileArgs, LocalFileRotateState]) (infer.ReadResponse[LocalFileArgs, LocalFileRotateState], error) {
	if req.State.Path == "" {
		path, err := vpsTimedIDValue("rot", req.ID)
		if err != nil {
			return infer.ReadResponse[LocalFileArgs, LocalFileRotateState]{}, err
		}
		req.State.Path, req.Inputs.Path = path, path
	}
	return infer.ReadResponse[LocalFileArgs, LocalFileRotateState]{ID: req.ID, Inputs: req.Inputs, State: req.State}, nil
}

func (LocalFileRotate) Diff(context.Context, infer.DiffRequest[LocalFileArgs, LocalFileRotateState]) (p.DiffResponse, error) {
	return p.DiffResponse{}, nil
}

type oneShot[I, S any] struct{}

func (oneShot[I, S]) Update(_ context.Context, req infer.UpdateRequest[I, S]) (infer.UpdateResponse[S], error) {
	return infer.UpdateResponse[S]{Output: req.State}, nil
}

func (oneShot[I, S]) Delete(context.Context, infer.DeleteRequest[S]) (infer.DeleteResponse, error) {
	return infer.DeleteResponse{}, nil
}

func localPathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func vpsTimestamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func vpsTimedID(prefix, value string) string {
	return fmt.Sprintf("%s-%s-%d", prefix, value, time.Now().UnixMilli())
}

func vpsTimedIDValue(prefix, id string) (string, error) {
	rest, ok := strings.CutPrefix(id, prefix+"-")
	if end := strings.LastIndexByte(rest, '-'); ok && end >= 0 {
		if _, err := strconv.ParseUint(rest[end+1:], 10, 64); err == nil {
			return rest[:end], nil
		}
	}
	return "", fmt.Errorf("invalid ID %q, expected %s-<value>-<unix milliseconds>", id, prefix)
}
