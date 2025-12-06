package network

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	"github.com/containerd/containerd"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func DumpPodIP(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		container, ok := ctx.Value(containerd_keys.CONTAINER_CONTEXT_KEY).(containerd.Container)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get container from context")
		}

		task, err := container.Task(ctx, nil)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get container info: %v", err)
		}

		jobPid := task.Pid()

		if jobPid == 0 {
			return nil, status.Errorf(codes.Internal, "failed to get container pid")
		}

		podIP, err := getPodIPFromNamespace(jobPid)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get pod IP from container pid %d: %v", jobPid, err)
		}

		if opts.DumpFs != nil {
			log.Info().Msgf("Dumping pod IP %s to file", podIP)
			if err := writePodIPToFile(opts.DumpFs, podIP); err != nil {
				return nil, status.Errorf(codes.Internal, "failed to write pod IP to file: %v", err)
			}
		}

		return next(ctx, opts, resp, req)
	}
}

func getPodIPFromNamespace(nsPid uint32) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	nsPath := fmt.Sprintf("/proc/%d/ns/net", nsPid)
	nsFd, err := os.Open(nsPath)
	if err != nil {
		return "", err
	}
	defer nsFd.Close()

	if err := unix.Setns(int(nsFd.Fd()), syscall.CLONE_NEWNET); err != nil {
		return "", err
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				if ipnet.IP.To4() != nil {
					return ipnet.IP.String(), nil
				}
			}
		}
	}

	return "", fmt.Errorf("no IP found")
}

func writePodIPToFile(fs afero.Fs, podIP string) error {
	f, err := fs.Create(containerd_keys.DUMP_PODIP_KEY)
	if err != nil {
		return fmt.Errorf("failed to create pod-ip file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write([]byte(podIP)); err != nil {
		return fmt.Errorf("failed to write pod IP to file: %w", err)
	}

	return nil
}
