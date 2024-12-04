package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/specconv"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/selinux/go-selinux"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Defines run (runc) handlers that ship with this plugin

func Run() types.Start {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.StartResp, req *daemon.StartReq) (exited chan int, err error) {
		opts := req.GetDetails().GetRuncStart()
		if opts == nil {
			return nil, status.Error(codes.InvalidArgument, "missing process start options")
		}

		root := opts.GetRoot()
		bundle := opts.GetBundle()
		id := opts.GetID()
		noPivot := opts.GetNoPivot()
		noNewKeyring := opts.GetNoNewKeyring()

		spec, err := loadSpec(filepath.Join(bundle, "config.json"))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to load spec: %v", err)
		}

		config, err := specconv.CreateLibcontainerConfig(&specconv.CreateOpts{
			CgroupName:   id,
			NoPivotRoot:  noPivot,
			NoNewKeyring: noNewKeyring,
			Spec:         spec,
		})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create libcontainer config: %v", err)
		}

		container, err := libcontainer.Create(root, bundle, config)
    err := container.Start()

		return nil, nil
	}
}

//////////////////////
// Helper functions //
//////////////////////

// loadSpec loads the config JSON specification file at the given path and validates it.
func loadSpec(cPath string) (spec *specs.Spec, err error) {
	cf, err := os.Open(cPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("JSON specification file %s not found", cPath)
		}
		return nil, err
	}
	defer cf.Close()

	if err = json.NewDecoder(cf).Decode(&spec); err != nil {
		return nil, err
	}
	if spec == nil {
		return nil, errors.New("config cannot be null")
	}
	return spec, validateProcessSpec(spec.Process)
}

// Lifted from libcontainer
func validateProcessSpec(spec *specs.Process) error {
	if spec == nil {
		return errors.New("process property must not be empty")
	}
	if spec.Cwd == "" {
		return errors.New("Cwd property must not be empty")
	}
	if !filepath.IsAbs(spec.Cwd) {
		return errors.New("Cwd must be an absolute path")
	}
	if len(spec.Args) == 0 {
		return errors.New("args must not be empty")
	}
	if spec.SelinuxLabel != "" && !selinux.GetEnabled() {
		return errors.New("selinux label is specified in config, but selinux is disabled or not supported")
	}
	return nil
}
