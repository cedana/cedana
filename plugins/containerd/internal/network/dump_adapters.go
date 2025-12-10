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

func DumpNetnsEth0IPv4Addr(next types.Dump) types.Dump {
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

		netnsEth0IPv4Addr, err := getNetnsEth0IPv4Addr(jobPid)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get netns eth0 IPv4 address from container pid %d: %v", jobPid, err)
		}

		if opts.DumpFs != nil {
			log.Info().Msgf("Dumping netns eth0 IPv4 address %s to file", netnsEth0IPv4Addr)
			if err := writeNetnsEth0IPv4AddrFile(opts.DumpFs, netnsEth0IPv4Addr); err != nil {
				return nil, status.Errorf(codes.Internal, "failed to write netns eth0 IPv4 address to file: %v", err)
			}
		}

		return next(ctx, opts, resp, req)
	}
}

func getNetnsEth0IPv4Addr(nsPid uint32) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origNsFd, err := os.Open("/proc/self/ns/net")
	if err != nil {
		return "", err
	}
	defer origNsFd.Close()

	nsPath := fmt.Sprintf("/proc/%d/ns/net", nsPid)
	nsFd, err := os.Open(nsPath)
	if err != nil {
		return "", err
	}
	defer nsFd.Close()

	if err := unix.Setns(int(nsFd.Fd()), syscall.CLONE_NEWNET); err != nil {
		return "", err
	}

	defer unix.Setns(int(origNsFd.Fd()), syscall.CLONE_NEWNET)

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

func writeNetnsEth0IPv4AddrFile(fs afero.Fs, netnsEth0IPv4Addr string) error {
	f, err := fs.Create(containerd_keys.DUMP_NETNS_ETH0_IPV4ADDR_KEY)
	if err != nil {
		return fmt.Errorf("failed to create netns eth0 IPv4 address file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write([]byte(netnsEth0IPv4Addr)); err != nil {
		return fmt.Errorf("failed to write netns eth0 IPv4 address to file: %w", err)
	}

	return nil
}
