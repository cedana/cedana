package criu

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	rpc "github.com/cedana/cedana/pkg/api/criu"
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
func (c *Criu) Prepare(extraFiles ...*os.File) error {
	fds, err := syscall.Socketpair(syscall.AF_LOCAL, syscall.SOCK_SEQPACKET, 0)
	if err != nil {
		return err
	}

	cln := os.NewFile(uintptr(fds[0]), "criu-xprt-cln")
	clnNet, err := net.FileConn(cln)
	cln.Close()
	if err != nil {
		return err
	}
	srv := os.NewFile(uintptr(fds[1]), "criu-xprt-srv")
	defer srv.Close()

	args := []string{"swrk", strconv.Itoa(fds[1])}
	// #nosec G204
	cmd := exec.Command(c.swrkPath, args...)
	cmd.ExtraFiles = append(cmd.ExtraFiles, extraFiles...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
		Credential: &syscall.Credential{
			Uid: uint32(0),
			Gid: uint32(0),
		},
	}

	err = cmd.Start()
	if err != nil {
		cln.Close()
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
		if err := c.swrkCmd.Wait(); err != nil {
			errs = append(errs, fmt.Errorf("criu swrk failed: %w", err))
		}
		c.swrkCmd = nil
	}
	return errors.Join(errs...)
}

func (c *Criu) sendAndRecv(reqB []byte) (respB []byte, n int, oobB []byte, oobn int, err error) {
	cln := c.swrkSk
	_, err = cln.Write(reqB)
	if err != nil {
		return nil, 0, nil, 0, err
	}

	respB = make([]byte, 2*4096)
	oobB = make([]byte, 4096)
	n, oobn, _, _, err = cln.ReadMsgUnix(respB, oobB)
	if err != nil {
		return nil, 0, nil, 0, err
	}

	return
}

func (c *Criu) doSwrk(reqType rpc.CriuReqType, opts *rpc.CriuOpts, nfy Notify, extraFiles ...*os.File) (*rpc.CriuResp, error) {
	resp, err := c.doSwrkWithResp(reqType, opts, nfy, nil, extraFiles...)
	if err != nil {
		return nil, err
	}
	respType := resp.GetType()
	if respType != reqType {
		return nil, errors.New("unexpected CRIU RPC response")
	}

	return resp, nil
}

func (c *Criu) doSwrkWithResp(reqType rpc.CriuReqType, opts *rpc.CriuOpts, nfy Notify, features *rpc.CriuFeatures, extraFiles ...*os.File) (resp *rpc.CriuResp, retErr error) {
	req := rpc.CriuReq{
		Type: &reqType,
		Opts: opts,
	}

	if nfy != nil {
		opts.NotifyScripts = proto.Bool(true)
	}

	if features != nil {
		req.Features = features
	}

	if c.swrkCmd == nil {
		err := c.Prepare(extraFiles...)
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

		resp = &rpc.CriuResp{}
		err = proto.Unmarshal(respB[:respS], resp)
		if err != nil {
			return nil, err
		}

		if !resp.GetSuccess() {
			return resp, fmt.Errorf("operation failed (msg:%s err:%d)",
				resp.GetCrErrmsg(), resp.GetCrErrno())
		}

		respType := resp.GetType()
		if respType != rpc.CriuReqType_NOTIFY {
			break
		}
		if nfy == nil {
			return resp, errors.New("unexpected notify")
		}

		notify := resp.GetNotify()
		switch notify.GetScript() {
		case "pre-dump":
			err = nfy.PreDump()
		case "post-dump":
			err = nfy.PostDump()
		case "pre-restore":
			err = nfy.PreRestore()
		case "post-restore":
			err = nfy.PostRestore(notify.GetPid())
		case "network-lock":
			err = nfy.NetworkLock()
		case "network-unlock":
			err = nfy.NetworkUnlock()
		case "setup-namespaces":
			err = nfy.SetupNamespaces(notify.GetPid())
		case "post-setup-namespaces":
			err = nfy.PostSetupNamespaces(notify.GetPid())
		case "pre-resume":
			err = nfy.PreResume(notify.GetPid())
		case "post-resume":
			err = nfy.PostResume(notify.GetPid())
		case "orphan-pts-master":
			fmt.Println("ORPHAN PTS MASTER")
			scm, err := syscall.ParseSocketControlMessage(oobB[:oobn])
			if err != nil {
				return nil, err
			}
			fds, err := syscall.ParseUnixRights(&scm[0])
			if err != nil {
				return nil, err
			}
			err = nfy.OrphanPtsMaster(int32(fds[0]))
		default:
			err = nil
		}

		if err != nil {
			return resp, err
		}

		req = rpc.CriuReq{
			Type:          &respType,
			NotifySuccess: proto.Bool(true),
		}
	}

	return resp, nil
}

// Dump dumps a process
func (c *Criu) Dump(opts *rpc.CriuOpts, nfy Notify) (*rpc.CriuDumpResp, error) {
	resp, err := c.doSwrk(rpc.CriuReqType_DUMP, opts, nfy)
	if err != nil {
		return nil, err
	}

	return resp.GetDump(), nil
}

// Restore restores a process
func (c *Criu) Restore(opts *rpc.CriuOpts, nfy Notify, extraFiles ...*os.File) (*rpc.CriuRestoreResp, error) {
	resp, err := c.doSwrk(rpc.CriuReqType_RESTORE, opts, nfy, extraFiles...)
	if err != nil {
		return nil, err
	}

	return resp.GetRestore(), nil
}

// PreDump does a pre-dump
func (c *Criu) PreDump(opts *rpc.CriuOpts, nfy Notify) error {
	_, err := c.doSwrk(rpc.CriuReqType_PRE_DUMP, opts, nfy)
	return err
}

// StartPageServer starts the page server
func (c *Criu) StartPageServer(opts *rpc.CriuOpts) error {
	_, err := c.doSwrk(rpc.CriuReqType_PAGE_SERVER, opts, nil)
	return err
}

// StartPageServerChld starts the page server and returns PID and port
func (c *Criu) StartPageServerChld(opts *rpc.CriuOpts) (int, int, error) {
	resp, err := c.doSwrkWithResp(rpc.CriuReqType_PAGE_SERVER_CHLD, opts, nil, nil)
	if err != nil {
		return 0, 0, err
	}

	return int(resp.GetPs().GetPid()), int(resp.GetPs().GetPort()), nil
}

// GetCriuVersion executes the VERSION RPC call and returns the version
// as an integer. Major * 10000 + Minor * 100 + SubLevel
func (c *Criu) GetCriuVersion() (int, error) {
	resp, err := c.doSwrkWithResp(rpc.CriuReqType_VERSION, nil, nil, nil)
	if err != nil {
		return 0, err
	}

	if resp.GetType() != rpc.CriuReqType_VERSION {
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
func (c *Criu) IsCriuAtLeast(version int) (bool, error) {
	criuVersion, err := c.GetCriuVersion()
	if err != nil {
		return false, err
	}

	if criuVersion >= version {
		return true, nil
	}

	return false, nil
}
