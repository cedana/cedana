package api

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/utils"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/spf13/afero"
)

// wrapper over filesystem, useful for mocking in tests
var AppFs = afero.NewOsFs()

type Client struct {
	CRIU *utils.Criu

	Logger *zerolog.Logger
	config *utils.Config

	context context.Context
	Process *task.ProcessInfo

	// a single big state glob
	state *task.ClientStateStreamingArgs

	// for dependency-injection of filesystems (useful for testing)
	fs *afero.Afero

	// checkpoint store
	store utils.Store

	CheckpointDir string
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

	criu := utils.MakeCriu()
	_, err := criu.GetCriuVersion()
	// TODO BS may err out if criu binaries aren't installed
	if err != nil {
		logger.Fatal().Err(err).Msg("Error checking CRIU version")
		return nil, err
	}
	// prepare client
	err = criu.Prepare()
	if err != nil {
		logger.Fatal().Err(err).Msg("Error preparing CRIU client")
		return nil, err
	}

	config, err := utils.InitConfig()
	if err != nil {
		logger.Fatal().Err(err).Msg("Could not read config")
		return nil, err
	}

	// set up filesystem wrapper
	fs := &afero.Afero{Fs: AppFs}

	return &Client{
		CRIU:    criu,
		Logger:  &logger,
		config:  config,
		context: context.Background(),
		fs:      fs,
	}, nil
}

func (c *Client) cleanupClient() error {
	c.CRIU.Cleanup()
	c.Logger.Info().Msg("cleaning up client")
	return nil
}

// sets up subscribers for dump and restore commands
func (c *Client) timeTrack(start time.Time, name string) {
	elapsed := time.Since(start)
	c.Logger.Debug().Msgf("%s took %s", name, elapsed)
}

func (c *Client) getState(pid int32) *task.ClientStateStreamingArgs {

	if pid == 0 {
		return nil
	}

	p, err := process.NewProcess(pid)
	if err != nil {
		c.Logger.Info().Msgf("Could not instantiate new gopsutil process with error %v", err)
	}

	var openFiles []*task.OpenFilesStat
	var writeOnlyFiles []string
	var openConnections []*task.ConnectionStat
	var flag task.FlagEnum

	if p != nil {
		openFilesOrig, err := p.OpenFiles()
		for _, f := range openFilesOrig {
			openFiles = append(openFiles, &task.OpenFilesStat{
				Fd:   f.Fd,
				Path: f.Path,
			})
		}

		if err != nil {
			// don't want to error out and break
			return nil
		}
		// used for network barriers (TODO: NR)
		openConnectionsOrig, err := p.Connections()
		if err != nil {
			return nil
		}
		for _, c := range openConnectionsOrig {
			Laddr := &task.Addr{
				IP:   c.Laddr.IP,
				Port: c.Laddr.Port,
			}
			Raddr := &task.Addr{
				IP:   c.Raddr.IP,
				Port: c.Raddr.Port,
			}
			openConnections = append(openConnections, &task.ConnectionStat{
				Fd:     c.Fd,
				Family: c.Family,
				Type:   c.Type,
				Laddr:  Laddr,
				Raddr:  Raddr,
				Status: c.Status,
				Pid:    c.Pid,
				Uids:   c.Uids,
			})
		}

		writeOnlyFiles = c.WriteOnlyFds(openFiles, pid)
	}

	memoryUsed, _ := p.MemoryPercent()
	isRunning, _ := p.IsRunning()

	// if the process is actually running, we don't care that
	// we're potentially overriding a failed flag here.
	// In the case of a restored/resuscitated process this is a good thing
	if isRunning {
		flag = task.FlagEnum_JOB_RUNNING
	}

	// this is the status as returned by gopsutil.
	// ideally we want more than this, or some parsing to happen from this end
	status, _ := p.Status()

	m, _ := mem.VirtualMemory()
	h, _ := host.Info()

	// ignore sending network for now, little complicated
	return &task.ClientStateStreamingArgs{
		ProcessInfo: &task.ProcessInfo{
			PID:                    pid,
			OpenFds:                openFiles,
			OpenWriteOnlyFilePaths: writeOnlyFiles,
			MemoryPercent:          memoryUsed,
			IsRunning:              isRunning,
			OpenConnections:        openConnections,
			Status:                 strings.Join(status, ""),
		},
		ClientInfo: &task.ClientInfo{
			Id:              "NOT IMPLEMENTED",
			Hostname:        h.Hostname,
			Platform:        h.Platform,
			OS:              h.OS,
			Uptime:          h.Uptime,
			RemainingMemory: m.Available,
		},
		Flag:            flag,
		CheckpointState: c.state.CheckpointState,
	}
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
			// c.Logger.Debug().Msgf("could not read fdinfo: %v", err)
			continue
		}

		lines := strings.Split(string(info), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "flags:") {
				// parsing out flags from the line and converting it out of octal.
				// so converting flags: 0100002 -> 32770
				flags, err := strconv.ParseInt(strings.TrimSpace(line[6:]), 8, 0)
				if err != nil {
					// c.Logger.Debug().Msgf("could not parse flags: %v", err)
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

// func (c *Client) startNATSService() {
// 	// create a subscription to NATS commands from the orchestrator first
// 	go c.subscribeToCommands(300)

// 	go c.publishStateContinuous(30)

// 	// listen for broadcast commands
// 	// subscribe to our broadcasters
// 	dumpCmdChn := c.channels.dumpCmdBroadcaster.Subscribe()
// 	restoreCmdChn := c.channels.restoreCmdBroadcaster.Subscribe()

// 	dir := c.config.SharedStorage.DumpStorageDir

// 	for {
// 		select {
// 		case <-dumpCmdChn:
// 			c.Logger.Info().Msg("received checkpoint command from NATS server")
// 			err := c.Dump(dir)
// 			if err != nil {
// 				c.Logger.Warn().Msgf("could not checkpoint process: %v", err)
// 				c.state.CheckpointState = cedana.CheckpointFailed
// 				c.publishStateOnce(c.getState(c.Process.PID))
// 			}
// 			c.state.CheckpointState = cedana.CheckpointSuccess
// 			c.publishStateOnce(c.getState(c.Process.PID))

// 		case cmd := <-restoreCmdChn:
// 			c.Logger.Info().Msg("received restore command from NATS server")
// 			pid, err := c.NatsRestore(&cmd, nil)
// 			if err != nil {
// 				c.Logger.Warn().Msgf("could not restore process: %v", err)
// 				c.state.CheckpointState = cedana.RestoreFailed
// 				c.publishStateOnce(c.getState(c.Process.PID))
// 			}
// 			c.state.CheckpointState = cedana.RestoreSuccess
// 			c.Process.PID = *pid
// 			c.publishStateOnce(c.getState(c.Process.PID))

// 		default:
// 			time.Sleep(1 * time.Second)
// 		}
// 	}
// }
