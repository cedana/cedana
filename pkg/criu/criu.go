package criu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"google.golang.org/protobuf/proto"
)

// Criu struct
type Criu struct {
	swrkCmd  *exec.Cmd
	swrkSk   *net.UnixConn
	swrkPath string
}

// MakeCriu returns the Criu object required for most operations
func MakeCriu() *Criu {
	return &Criu{
		swrkPath: "criu",
	}
}

// SetCriuPath allows setting the path to the CRIU binary
// if it is in a non standard location
func (c *Criu) SetCriuPath(path string) {
	c.swrkPath = path
}

// Prepare sets up everything for the RPC communication to CRIU
func (c *Criu) Prepare(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, extraFiles ...*os.File) error {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return err
	}

	cln := os.NewFile(uintptr(fds[0]), "criu-xprt-cln")
	defer cln.Close()
	clnNet, err := net.FileConn(cln)
	if err != nil {
		return err
	}
	srv := os.NewFile(uintptr(fds[1]), "criu-xprt-srv")
	defer srv.Close()

	args := []string{"swrk", strconv.Itoa(3 + len(extraFiles))}
	cmd := exec.CommandContext(ctx, c.swrkPath, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.ExtraFiles = append(extraFiles, srv)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGKILL, // kill even if server dies suddenly
	}

	err = cmd.Start()
	if err != nil {
		clnNet.Close()
		return err
	}

	c.swrkCmd = cmd
	c.swrkSk = clnNet.(*net.UnixConn)

	return nil
}

// Cleanup cleans up
func (c *Criu) Cleanup() error {
	var errs []error
	if c.swrkCmd != nil {
		if err := c.swrkSk.Close(); err != nil {
			errs = append(errs, err)
		}
		c.swrkSk = nil
		// XXX: We don't use s.swrkCmd.Wait() because it can hang forever
		// since the stdin, stdout, and stderr copy might not be over.
		if _, err := c.swrkCmd.Process.Wait(); err != nil {
			errs = append(errs, fmt.Errorf("criu swrk failed: %w", err))
		}
		c.swrkCmd = nil
	}
	return errors.Join(errs...)
}

func (c *Criu) sendAndRecv(reqB []byte) (respB []byte, n int, oobB []byte, oobn int, err error) {
	cln := c.swrkSk

	// Try write a couple of times
	for range 5 {
		var wrote int
		wrote, _, err = cln.WriteMsgUnix(reqB, nil, nil)
		if err == nil && wrote == len(reqB) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		return nil, 0, nil, 0, err
	}

	respB = make([]byte, 2*4096)
	oobB = make([]byte, 4096)
	n, oobn, _, _, err = cln.ReadMsgUnix(respB, oobB)
	if err != nil {
		return nil, 0, nil, 0, err
	}

	return respB, n, oobB, oobn, err
}

func (c *Criu) doSwrk(
	ctx context.Context,
	reqType criu.CriuReqType,
	opts *criu.CriuOpts,
	nfy Notify,
	stdin io.Reader,
	stdout, stderr io.Writer,
	extraFiles ...*os.File,
) (*criu.CriuResp, error) {
	resp, err := c.doSwrkWithResp(ctx, reqType, opts, nfy, nil, stdin, stdout, stderr, extraFiles...)
	if err != nil {
		return nil, err
	}
	respType := resp.GetType()
	if respType != reqType {
		return nil, errors.New("unexpected CRIU RPC response")
	}

	return resp, nil
}

func (c *Criu) doSwrkWithResp(
	ctx context.Context,
	reqType criu.CriuReqType,
	opts *criu.CriuOpts,
	nfy Notify,
	features *criu.CriuFeatures,
	stdin io.Reader,
	stdout, stderr io.Writer,
	extraFiles ...*os.File,
) (resp *criu.CriuResp, retErr error) {
	req := criu.CriuReq{
		Type: &reqType,
		Opts: opts,
	}

	if nfy != nil {
		opts.NotifyScripts = proto.Bool(true)
	}

	if features != nil {
		req.Features = features
	}

	err := c.Prepare(ctx, stdin, stdout, stderr, extraFiles...)
	if err != nil {
		return nil, err
	}

	defer func() {
		// append any cleanup errors to the returned error
		err := c.Cleanup()
		if err != nil {
			retErr = errors.Join(retErr, err)
		}
	}()

	if nfy != nil {
		err := nfy.Initialize(ctx, int32(c.swrkCmd.Process.Pid))
		if err != nil {
			return nil, fmt.Errorf("initialize failed: %w", err)
		}
		switch reqType {
		case criu.CriuReqType_RESTORE:
			err := nfy.InitializeRestore(ctx, opts)
			if err != nil {
				return nil, fmt.Errorf("initialize-restore failed: %w", err)
			}
			defer func() {
				err := nfy.FinalizeRestore(ctx, opts)
				if err != nil {
					retErr = errors.Join(retErr, err)
				}
			}()
		case criu.CriuReqType_DUMP:
			err := nfy.InitializeDump(ctx, opts)
			if err != nil {
				return nil, fmt.Errorf("initialize-dump failed: %w", err)
			}
			defer func() {
				err := nfy.FinalizeDump(ctx, opts)
				if err != nil {
					retErr = errors.Join(retErr, err)
				}
			}()
		}
	}

	for {
		reqB, err := proto.Marshal(&req)
		if err != nil {
			return nil, err
		}

		respB, respS, oobB, oobn, err := c.sendAndRecv(reqB)
		if err != nil {
			return nil, err
		}

		resp = &criu.CriuResp{}
		err = proto.Unmarshal(respB[:respS], resp)
		if err != nil {
			return nil, err
		}

		if !resp.GetSuccess() {
			return resp, fmt.Errorf("operation failed (msg:%s err:%d)",
				resp.GetCrErrmsg(), resp.GetCrErrno())
		}

		respType := resp.GetType()
		if respType != criu.CriuReqType_NOTIFY {
			break
		}
		if nfy == nil {
			return resp, errors.New("unexpected notify")
		}

		notify := resp.GetNotify()
		switch notify.GetScript() {
		case "pre-dump":
			err = nfy.PreDump(ctx, opts)
		case "post-dump":
			err = nfy.PostDump(ctx, opts)
		case "pre-restore":
			err = nfy.PreRestore(ctx, opts)
		case "post-restore":
			err = nfy.PostRestore(ctx, notify.GetPid())
		case "network-lock":
			err = nfy.NetworkLock(ctx)
		case "network-unlock":
			err = nfy.NetworkUnlock(ctx)
		case "setup-namespaces":
			err = nfy.SetupNamespaces(ctx, notify.GetPid())
		case "post-setup-namespaces":
			err = nfy.PostSetupNamespaces(ctx)
		case "pre-resume":
			err = nfy.PreResume(ctx)
		case "post-resume":
			err = nfy.PostResume(ctx)
		case "orphan-pts-master":
			scm, err := syscall.ParseSocketControlMessage(oobB[:oobn])
			if err != nil {
				return nil, err
			}
			fds, err := syscall.ParseUnixRights(&scm[0])
			if err != nil {
				return nil, err
			}
			err = nfy.OrphanPtsMaster(ctx, int32(fds[0]))
		default:
			err = nil
		}

		if err != nil {
			return resp, err
		}

		req = criu.CriuReq{
			Type:          &respType,
			NotifySuccess: proto.Bool(true),
		}
	}

	return resp, nil
}

// Dump dumps a process
func (c *Criu) Dump(
	ctx context.Context,
	opts *criu.CriuOpts,
	nfy Notify,
) (*criu.CriuDumpResp, error) {
	resp, err := c.doSwrk(ctx, criu.CriuReqType_DUMP, opts, nfy, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	return resp.GetDump(), nil
}

// Restore restores a process
func (c *Criu) Restore(
	ctx context.Context,
	opts *criu.CriuOpts,
	nfy Notify,
	stdin io.Reader,
	stdout, stderr io.Writer,
	extraFiles ...*os.File,
) (*criu.CriuRestoreResp, error) {
	resp, err := c.doSwrk(ctx, criu.CriuReqType_RESTORE, opts, nfy, stdin, stdout, stderr, extraFiles...)
	if err != nil {
		return nil, err
	}

	return resp.GetRestore(), nil
}

// PreDump does a pre-dump
func (c *Criu) PreDump(ctx context.Context, opts *criu.CriuOpts, nfy Notify) error {
	_, err := c.doSwrk(ctx, criu.CriuReqType_PRE_DUMP, opts, nfy, nil, nil, nil)
	return err
}

// StartPageServer starts the page server
func (c *Criu) StartPageServer(ctx context.Context, opts *criu.CriuOpts) error {
	_, err := c.doSwrk(ctx, criu.CriuReqType_PAGE_SERVER, opts, nil, nil, nil, nil)
	return err
}

// StartPageServerChld starts the page server and returns PID and port
func (c *Criu) StartPageServerChld(ctx context.Context, opts *criu.CriuOpts) (int, int, error) {
	resp, err := c.doSwrkWithResp(ctx, criu.CriuReqType_PAGE_SERVER_CHLD, opts, nil, nil, nil, nil, nil)
	if err != nil {
		return 0, 0, err
	}

	return int(resp.GetPs().GetPid()), int(resp.GetPs().GetPort()), nil
}

// GetCriuVersion executes the VERSION RPC call and returns the version
// as an integer. Major * 10000 + Minor * 100 + SubLevel
func (c *Criu) GetCriuVersion(ctx context.Context) (int, error) {
	resp, err := c.doSwrkWithResp(ctx, criu.CriuReqType_VERSION, nil, nil, nil, nil, nil, nil)
	if err != nil {
		return 0, err
	}

	if resp.GetType() != criu.CriuReqType_VERSION {
		return 0, errors.New("unexpected CRIU RPC response")
	}

	version := resp.GetVersion().GetMajorNumber() * 10000
	version += resp.GetVersion().GetMinorNumber() * 100
	if resp.GetVersion().GetSublevel() != 0 {
		version += resp.GetVersion().GetSublevel()
	}

	if resp.GetVersion().GetGitid() != "" {
		// taken from runc: if it is a git release -> increase minor by 1
		version -= (version % 100)
		version += 100
	}

	return int(version), nil
}

// IsCriuAtLeast checks if the version is at least the same
// as the parameter version
func (c *Criu) IsCriuAtLeast(ctx context.Context, version int) (bool, error) {
	criuVersion, err := c.GetCriuVersion(ctx)
	if err != nil {
		return false, err
	}

	if criuVersion >= version {
		return true, nil
	}

	return false, nil
}

// Runs the criu check
func (c *Criu) Check(ctx context.Context, flags ...string) (string, error) {
	args := []string{"check"}
	args = append(args, flags...)

	cmd := exec.Command(c.swrkPath, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
