package utils

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/cedana/cedana/pkg/api/daemon"
	"google.golang.org/grpc"
)

/////////////////////////////////////////////
//// Master/Slave I/O using gRPC Streams ////
/////////////////////////////////////////////

const (
	channelBufLen      = 32
	readFromBufLen     = 1024
	streamDoneExitCode = 254
	maxPendingMasters  = 0 // UNTESTED: DO NOT CHANGE
)

// Map of JID to Slave
var availableSlaves = sync.Map{}

type StreamIOMaster struct {
	slave grpc.BidiStreamingClient[daemon.AttachReq, daemon.AttachResp]

	in       <-chan []byte
	out      chan<- []byte
	err      chan<- []byte
	exitCode chan<- int
}

type StreamIOSlave struct {
	// Channel of masters waiting to attach (only one at a time allowed)
	master chan grpc.BidiStreamingServer[daemon.AttachReq, daemon.AttachResp]

	in       chan<- []byte
	out      <-chan []byte
	err      <-chan []byte
	exitCode <-chan int
}

type StreamIOReader struct {
	io.Reader
	io.WriterTo
	bytes <-chan []byte
}

type StreamIOWriter struct {
	io.Writer
	io.ReaderFrom
	bytes chan<- []byte
}

func NewStreamIOMaster(slave grpc.BidiStreamingClient[daemon.AttachReq, daemon.AttachResp]) (stdIn *StreamIOWriter, stdOut *StreamIOReader, stdErr *StreamIOReader, exitCode chan int) {
	in := make(chan []byte, channelBufLen)
	out := make(chan []byte, channelBufLen)
	err := make(chan []byte, channelBufLen)
	exitCode = make(chan int, 1)

	master := &StreamIOMaster{slave, in, out, err, exitCode}

	// Receive out/err from slave
	go func() {
	loop:
		for {
			resp, error := master.slave.Recv()
			if error != nil {
				exitCode <- streamDoneExitCode
				break
			}
			switch resp.Output.(type) {
			case *daemon.AttachResp_Stdout:
				out <- resp.GetStdout()
			case *daemon.AttachResp_Stderr:
				err <- resp.GetStderr()
			case *daemon.AttachResp_ExitCode:
				exitCode <- int(resp.GetExitCode())
				break loop
			}
		}
		close(out)
		close(err)
		close(exitCode)
	}()

	// Send in to slave
	go func() {
	loop:
		for {
			select {
			case <-master.slave.Context().Done():
				break loop
			case b, ok := <-in:
				if !ok {
					in = nil
					break loop
				}
				error := master.slave.Send(&daemon.AttachReq{Input: &daemon.AttachReq_Stdin{Stdin: b}})
				if error != nil {
					break loop
				}
			}
		}
		master.slave.CloseSend()
	}()

	stdIn = &StreamIOWriter{bytes: in}
	stdOut = &StreamIOReader{bytes: out}
	stdErr = &StreamIOReader{bytes: err}

	return
}

func NewStreamIOSlave(ctx context.Context, JID string, exitCode chan int) (stdIn *StreamIOReader, stdOut *StreamIOWriter, stdErr *StreamIOWriter) {
	in := make(chan []byte, channelBufLen)
	out := make(chan []byte, channelBufLen)
	err := make(chan []byte, channelBufLen)

	slave := &StreamIOSlave{
		make(chan grpc.BidiStreamingServer[daemon.AttachReq, daemon.AttachResp], maxPendingMasters),
		in,
		out,
		err,
		exitCode,
	}
	availableSlaves.Store(JID, slave)

	// Send out/err to master
	go func() {
		defer availableSlaves.Delete(JID)
		var master grpc.BidiStreamingServer[daemon.AttachReq, daemon.AttachResp]
		for {
			select {
			case <-ctx.Done():
				close(in)
				return
			case master = <-slave.master: // wait for a master to attach
				{
				stream:
					for {
						select {
						case <-master.Context().Done():
							break stream // and wait for new master
						case b, ok := <-out:
							if !ok {
								out = nil
								break
							}
							master.Send(&daemon.AttachResp{Output: &daemon.AttachResp_Stdout{Stdout: b}})
						case b, ok := <-err:
							if !ok {
								err = nil
								break
							}
							master.Send(&daemon.AttachResp{Output: &daemon.AttachResp_Stderr{Stderr: b}})
						}
						if out == nil && err == nil { // exit once we've sent all out/err
							close(in) // XXX: causes race with in's senders in attach
							master.Send(&daemon.AttachResp{Output: &daemon.AttachResp_ExitCode{ExitCode: int32(<-exitCode)}})
							return
						}
					}
				}
			}
		}
	}()

	stdIn = &StreamIOReader{bytes: in}
	stdOut = &StreamIOWriter{bytes: out}
	stdErr = &StreamIOWriter{bytes: err}

	return
}

// Attach attaches a master stream to the slave.
func (s *StreamIOSlave) Attach(ctx context.Context, master grpc.BidiStreamingServer[daemon.AttachReq, daemon.AttachResp]) error {
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context done")
		case s.master <- master:
			goto loop
		}
	}

	// Receive in from master
loop:
	for {
		fmt.Println("master attached to slave")
		req, error := master.Recv()
		if error != nil {
			break
		}
		select {
		case s.in <- req.GetStdin():
		case <-master.Context().Done():
			break loop
		}
	}

	return nil
}

func (s *StreamIOReader) Read(p []byte) (n int, err error) {
	for b := range s.bytes {
		nb := copy(p, b)
		return nb, nil
	}
	return 0, io.EOF
}

func (s *StreamIOReader) WriteTo(w io.Writer) (n int64, err error) {
	for b := range s.bytes {
		nb, err := w.Write(b)
		if err != nil {
			return n, err
		}
		n += int64(nb)
	}
	return
}

func (s *StreamIOWriter) Write(p []byte) (n int, err error) {
	s.bytes <- p
	return len(p), nil
}

func (s *StreamIOWriter) ReadFrom(r io.Reader) (n int64, err error) {
	defer close(s.bytes)
	buf := make([]byte, readFromBufLen)
	for {
		nr, err := r.Read(buf)
		if nr > 0 {
			s.bytes <- buf[:nr]
			n += int64(nr)
		}
		if err != nil {
			return n, err
		}
	}
}

////////////////////////////////
//// Other Helper Functions ////
////////////////////////////////

func GetIOSlave(JID string) *StreamIOSlave {
	slave, ok := availableSlaves.Load(JID)
	if !ok {
		return nil
	}
	return slave.(*StreamIOSlave)
}

// CopyNotify asynchronously does io.Copy, notifying when done.
func CopyNotify(dst io.Writer, src io.Reader) chan any {
	done := make(chan any)
	go func() {
		io.Copy(dst, src)
		close(done)
	}()
	return done
}
