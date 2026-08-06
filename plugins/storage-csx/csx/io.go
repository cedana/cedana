package csx

import (
	"context"
	"fmt"
	"io"
	"os"

	"buf.build/gen/go/cedana/cedana/grpc/go/plugins/csx/csxgrpc"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/csx"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
)

type CSXWriter struct {
	ctx       context.Context
	writeID   string
	objectID  string
	path      string
	writer    io.WriteCloser
	csxClient csxgrpc.CSXClient
	*grpc.ClientConn
}

func (w *CSXWriter) Write(p []byte) (n int, err error) {
	if w.writer == nil {
		return 0, fmt.Errorf("underlying writer has been closed")
	}
	return w.writer.Write(p)
}

func (w *CSXWriter) Close() error {
	if w.writer != nil && w.csxClient != nil {
		err := w.writer.Close()
		if err != nil {
			return err
		}
		w.writer = nil
		_, err = w.csxClient.ClosePath(w.ctx, &csx.ClosePathReq{ActionID: w.writeID, ID: w.objectID})
		if err != nil {
			w.ClientConn.Close()
			w.csxClient = nil
			return err
		}
		w.ClientConn.Close()
		w.csxClient = nil
	}
	return nil
}

func NewWriter(ctx context.Context, path, objectID, writeID string, csxClient csxgrpc.CSXClient, conn *grpc.ClientConn) (*CSXWriter, error) {
	writer, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &CSXWriter{
		ctx,
		writeID,
		objectID,
		path,
		writer,
		csxClient,
		conn,
	}, nil
}

type CSXReader struct {
	ctx       context.Context
	readID    string
	objectID  string
	path      string
	reader    io.ReadCloser
	csxClient csxgrpc.CSXClient
	*grpc.ClientConn
}

func (r *CSXReader) Read(p []byte) (n int, err error) {
	if r.reader == nil {
		return 0, fmt.Errorf("underlying reader has been closed")
	}
	return r.reader.Read(p)
}

func (r *CSXReader) Close() error {
	if r.reader != nil && r.csxClient != nil {
		err := r.reader.Close()
		if err != nil {
			return err
		}
		r.reader = nil
		_, err = r.csxClient.ClosePath(r.ctx, &csx.ClosePathReq{ActionID: r.readID, ID: r.objectID})
		if err != nil {
			r.ClientConn.Close()
			r.csxClient = nil
			return err
		}
		r.ClientConn.Close()
		r.csxClient = nil
	}
	return nil
}

func NewReader(ctx context.Context, path, objectID, readID string, csxClient csxgrpc.CSXClient, conn *grpc.ClientConn) (*CSXReader, error) {
	reader, err := os.Open(path)
	if err != nil {
		log.Debug().Err(err).Str("path", path).Msg("failed to open file with os.Open()")
		return nil, err
	}
	log.Debug().Msg("returning CSX reader")
	return &CSXReader{
		ctx,
		readID,
		objectID,
		path,
		reader,
		csxClient,
		conn,
	}, nil
}
