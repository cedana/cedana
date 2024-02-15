package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/utils"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/spf13/afero"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// wrapper over filesystem, useful for mocking in tests
var AppFs = afero.NewOsFs()

type Client struct {
	CRIU   *Criu
	logger *zerolog.Logger
	config *utils.Config

	// for dependency-injection of filesystems (useful for testing)
	fs *afero.Afero

	// external checkpoint remoteStore
	remoteStore utils.Store

	// db meta/state store
	db *DB

	// a separate client is created for each connection to the gRPC client,
	// and the jobID is set at instantiation time. We rely on whatever is callig
	// InstantiateClient() to set the jobID correctly.
	jobID string

	// used for perf, CEDANA_OTEL_ENABLED needs to be set
	tracer trace.Tracer
}

type ClientLogs struct {
	Timestamp string `json:"timestamp"`
	Source    string `json:"source"`
	Level     string `json:"level"`
	Msg       string `json:"msg"`
}

func InstantiateClient() (*Client, error) {
	// instantiate logger
	logger := utils.GetLogger()
	config, err := utils.InitConfig()
	if err != nil {
		logger.Fatal().Err(err).Msg("Could not read config")
		return nil, err
	}

	// set up filesystem wrapper
	fs := &afero.Afero{Fs: AppFs}

	db := &DB{}

	return &Client{
		CRIU:   new(Criu),
		logger: &logger,
		config: config,
		fs:     fs,
		db:     db,
		tracer: otel.Tracer("cedana-daemon"),
	}, nil
}

func (c *Client) cleanupClient() error {
	c.logger.Info().Msg("cleaning up client")
	return nil
}

// sets up subscribers for dump and restore commands
func (c *Client) timeTrack(start time.Time, name string) {
	elapsed := time.Since(start)
	c.logger.Debug().Msgf("%s took %s", name, elapsed)
}

// TODO NR - customizable errors
func (c *Client) generateState(pid int32) (*task.ProcessState, error) {
	if pid == 0 {
		return nil, nil
	}

	var state task.ProcessState

	p, err := process.NewProcess(pid)
	if err != nil {
		c.logger.Info().Msgf("Could not instantiate new gopsutil process for pid %d with error: %v", pid, err)
	}

	state.PID = pid

	var openFiles []*task.OpenFilesStat
	var openConnections []*task.ConnectionStat

	if p != nil {
		of, err := p.OpenFiles()
		for _, f := range of {
			var mode string
			var stream task.OpenFilesStat_StreamType
			file, err := os.Stat(f.Path)
			if err == nil {
				mode = file.Mode().Perm().String()
				switch f.Fd {
				case 0:
					stream = task.OpenFilesStat_STDIN
				case 1:
					stream = task.OpenFilesStat_STDOUT
				case 2:
					stream = task.OpenFilesStat_STDERR
				default:
					stream = task.OpenFilesStat_NONE
				}
			}

			openFiles = append(openFiles, &task.OpenFilesStat{
				Fd:     f.Fd,
				Path:   f.Path,
				Mode:   mode,
				Stream: stream,
			})
		}

		if err != nil {
			// don't want to error out and break
			return nil, nil
		}
		// used for network barriers (TODO: NR)
		conns, err := p.Connections()
		if err != nil {
			return nil, nil
		}
		for _, conn := range conns {
			Laddr := &task.Addr{
				IP:   conn.Laddr.IP,
				Port: conn.Laddr.Port,
			}
			Raddr := &task.Addr{
				IP:   conn.Raddr.IP,
				Port: conn.Raddr.Port,
			}
			openConnections = append(openConnections, &task.ConnectionStat{
				Fd:     conn.Fd,
				Family: conn.Family,
				Type:   conn.Type,
				Laddr:  Laddr,
				Raddr:  Raddr,
				Status: conn.Status,
				PID:    conn.Pid,
				Uids:   conn.Uids,
			})
		}
	}

	memoryUsed, _ := p.MemoryPercent()
	isRunning, _ := p.IsRunning()

	// if the process is actually running, we don't care that
	// we're potentially overriding a failed flag here.
	// In the case of a restored/resuscitated process this is a good thing

	// this is the status as returned by gopsutil.
	// ideally we want more than this, or some parsing to happen from this end
	status, _ := p.Status()

	// we need the cwd to ensure that it exists on the other side of the restore.
	// if it doesn't - we inheritFd it?
	cwd, err := p.Cwd()
	if err != nil {
		return nil, nil
	}

	// ignore sending network for now, little complicated
	state.ProcessInfo = &task.ProcessInfo{
		OpenFds:         openFiles,
		WorkingDir:      cwd,
		MemoryPercent:   memoryUsed,
		IsRunning:       isRunning,
		OpenConnections: openConnections,
		Status:          strings.Join(status, ""),
	}

	return &state, nil
}

// WriteOnlyFds takes a snapshot of files that are open (in writeonly) by process PID
// and outputs full paths. For concurrent processes (multithreaded) this can be dangerous and lead to
// weird race conditions (maybe).
// To avoid actually using ptrace (TODO NR) we loop through the openFds of the process and check the
// flags.
func (c *Client) WriteOnlyFds(openFds []*task.OpenFilesStat, pid int32) []string {
	var paths []string
	for _, fd := range openFds {
		info, err := c.fs.ReadFile(fmt.Sprintf("/proc/%s/fdinfo/%s", strconv.Itoa(int(pid)), strconv.FormatUint(fd.Fd, 10)))
		if err != nil {
			c.logger.Debug().Msgf("could not read fdinfo: %v", err)
			continue
		}

		lines := strings.Split(string(info), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "flags:") {
				// parsing out flags from the line and converting it out of octal.
				// so converting flags: 0100002 -> 32770
				flags, err := strconv.ParseInt(strings.TrimSpace(line[6:]), 8, 0)
				if err != nil {
					c.logger.Debug().Msgf("could not parse flags: %v", err)
					continue
				}

				// bitwise compare flags with os.O_RDWR
				if int(flags)&os.O_RDWR != 0 || int(flags)&os.O_WRONLY != 0 {
					// gopsutil appends a (deleted) flag to the path sometimes, which I'm not fully sure of why yet
					// TODO NR - figure this out
					path := strings.Replace(fd.Path, " (deleted)", "", -1)
					paths = append(paths, path)
				}
			}
		}
	}
	return paths
}

// Generates state using gops and updates pid state in db.
func (c *Client) getState(pid int32) (*task.ProcessState, error) {
	state, err := c.generateState(pid)
	if err != nil {
		return nil, err
	}

	err = c.db.UpdateProcessStateWithPID(pid, state)
	if err != nil {
		return nil, err
	}

	return state, err
}

func (c *Client) SerializeStateToDir(dir string, state *task.ProcessState) error {
	serialized, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "checkpoint_state.json")
	file, err := os.Create(path)
	if err != nil {
		return err
	}

	defer file.Close()
	_, err = file.Write(serialized)
	return err
}

func closeCommonFds(parentPID, childPID int32) error {
	parent, err := process.NewProcess(parentPID)
	if err != nil {
		return err
	}

	child, err := process.NewProcess(childPID)
	if err != nil {
		return err
	}

	parentFds, err := parent.OpenFiles()
	if err != nil {
		return err
	}

	childFds, err := child.OpenFiles()
	if err != nil {
		return err
	}

	for _, pfd := range parentFds {
		for _, cfd := range childFds {
			if pfd.Path == cfd.Path && strings.Contains(pfd.Path, ".pid") {
				// we have a match, close the FD
				err := syscall.Close(int(cfd.Fd))
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}
