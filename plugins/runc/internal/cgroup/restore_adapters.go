package cgroup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/opencontainers/cgroups"
	"github.com/opencontainers/runc/libcontainer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	specs "github.com/opencontainers/runtime-spec/specs-go"

	// WARN: DO NOT REMOVE THIS IMPORT. Has side effects.
	// See -> 'github.com/opencontainers/runc/libcontainer/cgroups/cgroups.go'
	_ "github.com/opencontainers/cgroups/devices"
)

// Sets the ManageCgroups field in the criu options to true.
func ManageCgroupsForRestore(mode criu_proto.CriuCgMode) types.Adapter[types.Restore] {
	return func(next types.Restore) types.Restore {
		return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
			if req.GetCriu() == nil {
				req.Criu = &criu_proto.CriuOpts{}
			}

			req.Criu.ManageCgroups = proto.Bool(true)
			req.Criu.ManageCgroupsMode = &mode

			return next(ctx, opts, resp, req)
		}
	}
}

func GetNetworkPid(bundlePath string) (int, error) {
	var spec specs.Spec
	var pid int = 0
	configPath := filepath.Join(bundlePath, "config.json")
	if _, err := os.Stat(configPath); err == nil {
		configFile, err := os.ReadFile(configPath)
		if err != nil {
			return 0, err
		}
		if err := json.Unmarshal(configFile, &spec); err != nil {
			return 0, err
		}
		for _, ns := range spec.Linux.Namespaces {
			if ns.Type == "network" && strings.Count(ns.Path, "/") > 1 {
				path := ns.Path
				splitPath := strings.Split(path, "/")
				pid, err = strconv.Atoi(splitPath[2])
				if err != nil {
					return 0, err
				}
				break
			}
		}
	}
	return pid, nil
}

// Adds a initialize hook that applies cgroups to the CRIU process as soon as it is started.
func ApplyCgroupsOnRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get container from context")
		}
		manager, ok := ctx.Value(runc_keys.CONTAINER_CGROUP_MANAGER_CONTEXT_KEY).(cgroups.Manager)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get cgroup manager from context")
		}

		if req.Criu == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		config := container.Config()
		var netpid int = 0
		if req.Details.Runc != nil {
			netpid, err = GetNetworkPid(req.Details.Runc.Bundle)
			if err != nil {
				return nil, err
			}
		}

		callback := &criu.NotifyCallback{
			InitializeFunc: func(ctx context.Context, criuPid int32) error {
				if netpid != 0 {
					targetCgroups, err := cgroups.ParseCgroupFile(fmt.Sprintf("/proc/%d/cgroup", netpid))
					if err != nil {
						return err
					}

					for controller, path := range targetCgroups {
						cgroupPath := filepath.Join("/sys/fs/cgroup", controller, path, "cgroup.procs")
						err := os.WriteFile(cgroupPath, []byte(strconv.Itoa(int(criuPid))), 0644)
						if err != nil {
							return err
						}
					}
					return nil
				}
				err := manager.Apply(int(criuPid))
				if err != nil {
					return fmt.Errorf("failed to apply cgroups: %v", err)
				}
				err = manager.Set(config.Cgroups.Resources)
				if err != nil {
					return fmt.Errorf("failed to set cgroup resources: %v", err)
				}

				// TODO Should we use c.cgroupManager.GetPaths()
				// instead of reading /proc/pid/cgroup?
				path := fmt.Sprintf("/proc/%d/cgroup", criuPid)
				cgroupsPaths, err := cgroups.ParseCgroupFile(path)
				if err != nil {
					return err
				}
				for c, p := range cgroupsPaths {
					cgroupRoot := &criu_proto.CgroupRoot{
						Ctrl: proto.String(c),
						Path: proto.String(p),
					}
					req.Criu.CgRoot = append(req.Criu.CgRoot, cgroupRoot)
				}

				return nil
			},
		}
		opts.CRIUCallback.Include(callback)

		return next(ctx, opts, resp, req)
	}
}
