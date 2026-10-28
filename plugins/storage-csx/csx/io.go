package csx

import (
	"context"
	"errors"
	"io"
	"os"

	"buf.build/gen/go/cedana/cedana/grpc/go/plugins/csx/csxgrpc"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/csx"
	"google.golang.org/grpc"
)

type File struct {
	ctx       context.Context
	conn      *grpc.ClientConn
	csxClient csxgrpc.CSXClient
	objectID  string
	actionID  string
	path      string
	writer    io.WriteCloser
	reader    io.ReadCloser
}

func (f *File) Write(p []byte) (int, error) {
	if f.writer == nil {
		var err error
		f.writer, err = os.Create(f.path)
		if err != nil {
			return 0, err
		}
	}
	return f.writer.Write(p)
}

func (f *File) Read(p []byte) (int, error) {
	if f.reader == nil {
		var err error
		f.reader, err = os.Open(f.path)
		if err != nil {
			return 0, err
		}
	}
	return f.reader.Read(p)
}

func (f *File) Close() error {
	var err error
	if f.writer != nil {
		err = errors.Join(err, f.writer.Close())
	}
	if f.reader != nil {
		err = errors.Join(err, f.reader.Close())
	}
	closeReq := &csx.ClosePathReq{
		ID:       f.objectID,
		ActionID: f.actionID,
	}
	_, closeErr := f.csxClient.ClosePath(f.ctx, closeReq)
	err = errors.Join(err, closeErr)
	err = errors.Join(err, f.conn.Close())
	return err
}

func NewFile(ctx context.Context, objectID, actionID, path string, conn *grpc.ClientConn, csxClient csxgrpc.CSXClient) *File {
	return &File{
		ctx,
		conn,
		csxClient,
		objectID,
		actionID,
		path,
		nil,
		nil,
	}
}
