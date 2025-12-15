package io

/////////////////////////////////////////////
//// Master/Slave I/O using gRPC Streams ////
/////////////////////////////////////////////

import (
	"context"
	"fmt"
	"io"
	"sync"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	channelBufLen      = 32 // pending byte arrays in channel
	readFromBufLen     = 512
	streamDoneExitCode = 254
	maxPendingMasters  = 0 // UNTESTED: DO NOT CHANGE
)

// Map of PID to Slave
var availableSlaves = sync.Map{}

type StreamIOMaster struct {
	slave grpc.BidiStreamingClient[daemon.AttachReq, daemon.AttachResp]

	in       <-chan []byte
	out      chan<- []byte
	err      chan<- []byte
	exitCode chan<- int
}

type StreamIOSlave struct {
	PID uint32

	// Channel of masters waiting to attach
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

	buffer []byte
}

type StreamIOWriter struct {
	io.WriteCloser
	io.ReaderFrom
	bytes chan<- []byte
}

func NewStreamIOMaster(
	slave grpc.BidiStreamingClient[daemon.AttachReq, daemon.AttachResp],
) (stdIn *StreamIOWriter, stdOut *StreamIOReader, stdErr *StreamIOReader, exitCode chan int, errors chan error) {
	in := make(chan []byte, channelBufLen)
	out := make(chan []byte, channelBufLen)
	err := make(chan []byte, channelBufLen)
	exitCode = make(chan int, 1)
	errors = make(chan error, 1)

	master := &StreamIOMaster{slave, in, out, err, exitCode}

	// Receive out/err from slave
	go func() {
	loop:
		for {
			resp, error := master.slave.Recv()
			if error != nil {
				if st, _ := status.FromError(error); st.Code() == codes.Canceled {
					error = fmt.Errorf("Detached")
				}
				errors <- error
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
		close(errors)
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
	}()

	stdIn = &StreamIOWriter{bytes: in}
	stdOut = &StreamIOReader{bytes: out}
	stdErr = &StreamIOReader{bytes: err}

	return stdIn, stdOut, stdErr, exitCode, errors
}

func NewStreamIOSlave(
	ctx context.Context,
	wg *sync.WaitGroup,
	pid uint32,
) (stdIn *StreamIOReader, stdOut *StreamIOWriter, stdErr *StreamIOWriter) {
	in := make(chan []byte, channelBufLen)
	out := make(chan []byte, channelBufLen)
	err := make(chan []byte, channelBufLen)

	slave := &StreamIOSlave{
		pid,
		make(chan grpc.BidiStreamingServer[daemon.AttachReq, daemon.AttachResp], maxPendingMasters),
		in,
		out,
		err,
		make(chan int, 1),
	}

	SetIOSlave(pid, slave)

	// Send out/err to master
	wg.Go(func() {
		defer DeleteIOSlave(&slave.PID)

		masters := map[grpc.BidiStreamingServer[daemon.AttachReq, daemon.AttachResp]]any{}
		// Wait for first master before doing anything, so that no out/err is lost
	wait_first_master:
		for {
			select {
			case <-ctx.Done():
				close(in)
				return
			case master := <-slave.master:
				masters[master] = nil
				break wait_first_master
			}
		}
	exit:
		for {
			select {
			case <-ctx.Done():
				break exit
			case master := <-slave.master: // wait for a new master to attach
				masters[master] = nil
			case b, ok := <-out:
				if !ok {
					out = nil
					break
				}
				for master := range masters {
					err := master.Send(&daemon.AttachResp{Output: &daemon.AttachResp_Stdout{Stdout: b}})
					if err != nil {
						delete(masters, master)
					}
				}
			case b, ok := <-err:
				if !ok {
					err = nil
					break
				}
				for master := range masters {
					err := master.Send(&daemon.AttachResp{Output: &daemon.AttachResp_Stderr{Stderr: b}})
					if err != nil {
						delete(masters, master)
					}
				}
			}
			if out == nil && err == nil { // exit once we've sent all out/err
				break exit
			}
		}

		close(in)
		code := <-slave.exitCode
		for master := range masters {
			master.Send(
				&daemon.AttachResp{Output: &daemon.AttachResp_ExitCode{ExitCode: int32(code)}},
			)
		}
	})

	stdIn = &StreamIOReader{bytes: in}
	stdOut = &StreamIOWriter{bytes: out}
	stdErr = &StreamIOWriter{bytes: err}

	return stdIn, stdOut, stdErr
}

// Attach attaches a master stream to the slave.
func (s *StreamIOSlave) Attach(
	ctx context.Context,
	master grpc.BidiStreamingServer[daemon.AttachReq, daemon.AttachResp],
) error {
wait:
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-master.Context().Done():
			return master.Context().Err()
		case s.master <- master:
			break wait
		}
	}

	// Receive in from master
loop:
	for {
		req, error := master.Recv()
		if error != nil {
			break
		}
		select {
		case <-master.Context().Done():
			break loop
		case s.in <- req.GetStdin():
		}
	}

	return nil
}

func (s *StreamIOReader) Read(p []byte) (n int, err error) {
	var b []byte
	ok := true

	if len(s.buffer) > 0 { // if bytes in buffer, use it first
		b = s.buffer
	} else {
		b, ok = <-s.bytes
	}
	nb := copy(p, b)

	if nb < len(b) {
		s.buffer = b[nb:] // if bytes left, store in buffer
	} else {
		s.buffer = nil
	}

	if !ok {
		return nb, io.EOF
	}

	return nb, nil
}

func (s *StreamIOReader) WriteTo(w io.Writer) (n int64, err error) {
	for b := range s.bytes {
		nb, err := w.Write(b)
		n += int64(nb)
		if err != nil {
			return n, err
		}
	}
	return n, err
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
			// copy the buffer to the channel, as the buffer will be overwritten
			chunk := make([]byte, nr)
			copy(chunk, buf[:nr])
			s.bytes <- chunk
			n += int64(nr)
		}
		if err != nil {
			if err == io.EOF {
				return n, nil
			}
			return n, err
		}
	}
}

func (s *StreamIOWriter) Close() error {
	close(s.bytes)
	return nil
}

//////////////////////////
//// Helper Functions ////
//////////////////////////

// NOTE: Pointers are used in some functions below, to allow
// for updating the most current value at the time, expecially when
// using defer.

// SetIOSlave sets the slave associated with a PID.
func SetIOSlave(pid uint32, slave *StreamIOSlave) {
	slave.PID = pid
	availableSlaves.Store(pid, slave)
}

// DeleteIOSlave deletes the slave associated with a PID.
// Uses the PID value of pointer at the time of the call.
func DeleteIOSlave(pid *uint32) {
	availableSlaves.Delete(*pid)
}

// GetIOSlave returns the slave associated with a PID.
func GetIOSlave(pid uint32) *StreamIOSlave {
	slave, ok := availableSlaves.Load(pid)
	if !ok {
		return nil
	}
	return slave.(*StreamIOSlave)
}

// SetIOSlavePID updates the PID of an existing slave.
// Uses the PID value of pointer at the time of the call.
func SetIOSlavePID(oldId uint32, pid uint32) {
	slave := GetIOSlave(oldId)
	if slave == nil {
		return
	}
	DeleteIOSlave(&oldId)
	SetIOSlave(pid, slave)
}

func SetIOSlaveExitCode(oldId uint32, code <-chan int) {
	slave := GetIOSlave(oldId)
	if slave == nil {
		return
	}
	slave.exitCode = code
}
