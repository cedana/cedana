package cmd

import (
	"fmt"
	"io"
	"net"
	"os"
	"sync"

	"github.com/containerd/console"
	"github.com/opencontainers/runc/libcontainer/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

// Yoinked from https://github.com/opencontainers/runc/blob/v1.1.15/contrib/cmd/recvtty/recvtty.go, because
// installing recvtty in regression tests is a complete nightmare.

func handleSingle(path string, noStdin bool) error {
	// Open a socket.
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer ln.Close()

	// We only accept a single connection, since we can only really have
	// one reader for os.Stdin. Plus this is all a PoC.
	conn, err := ln.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()

	// Close ln, to allow for other instances to take over.
	ln.Close()

	// Get the fd of the connection.
	unixconn, ok := conn.(*net.UnixConn)
	if !ok {
		log.Error().Msg("failed to cast to unixconn")
		return nil
	}

	socket, err := unixconn.File()
	if err != nil {
		return err
	}
	defer socket.Close()

	// Get the master file descriptor from runC.
	master, err := utils.RecvFd(socket)
	if err != nil {
		return err
	}
	c, err := console.ConsoleFromFile(master)
	if err != nil {
		return err
	}
	if err := console.ClearONLCR(c.Fd()); err != nil {
		return err
	}

	// Copy from our stdio to the master fd.
	var (
		wg            sync.WaitGroup
		inErr, outErr error
	)
	wg.Add(1)
	go func() {
		_, outErr = io.Copy(os.Stdout, c)
		wg.Done()
	}()
	if !noStdin {
		wg.Add(1)
		go func() {
			_, inErr = io.Copy(c, os.Stdin)
			wg.Done()
		}()
	}

	// Only close the master fd once we've stopped copying.
	wg.Wait()
	c.Close()

	if outErr != nil {
		return outErr
	}

	return inErr
}

func handleNull(path string) error {
	// Open a socket.
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer ln.Close()

	// As opposed to handleSingle we accept as many connections as we get, but
	// we don't interact with Stdin at all (and we copy stdout to /dev/null).
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go func(conn net.Conn) {
			// Don't leave references lying around.
			defer conn.Close()

			// Get the fd of the connection.
			unixconn, ok := conn.(*net.UnixConn)
			if !ok {
				return
			}

			socket, err := unixconn.File()
			if err != nil {
				return
			}
			defer socket.Close()

			// Get the master file descriptor from runC.
			master, err := utils.RecvFd(socket)
			if err != nil {
				return
			}

			_, _ = io.Copy(io.Discard, master)
		}(conn)
	}
}

var recvttyCmd = &cobra.Command{
	Use: "recvtty",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("need to specify a single socket path")
		}
		path := args[0]

		pidPath, _ := cmd.Flags().GetString(pidFileFlag)
		if pidPath != "" {
			pid := fmt.Sprintf("%d\n", os.Getpid())
			if err := os.WriteFile(pidPath, []byte(pid), 0o644); err != nil {
				return err
			}
		}

		noStdin, _ := cmd.Flags().GetBool(noStdinFlag)
		mode, _ := cmd.Flags().GetString(modeFlag)
		switch mode {
		case "single":
			if err := handleSingle(path, noStdin); err != nil {
				return err
			}
		case "null":
			if err := handleNull(path); err != nil {
				return err
			}
		default:
			return fmt.Errorf("need to select a valid mode: %s", mode)
		}
		return nil
	},
}

func init() {
	debugCmd.AddCommand(recvttyCmd)
	recvttyCmd.Flags().Bool(noStdinFlag, false, "Disable stdin handling (no-op for null mode)")
	recvttyCmd.Flags().String(pidFileFlag, "", "Path to write daemon process ID to")
	recvttyCmd.Flags().String(modeFlag, "single", "Mode of operation (single or null)")
}
