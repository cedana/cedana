package csx

import (
	"context"
	"fmt"
	"io"
	"os"

	"buf.build/gen/go/cedana/cedana/grpc/go/plugins/csx/csxgrpc"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/csx"
)

type CSXWriter struct {
  ctx context.Context
  objectID string
  writeID string
  path string
  writer io.WriteCloser
  csxClient csxgrpc.CSXClient
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
    _, err = w.csxClient.CloseWrite(w.ctx, &csx.CloseReq{ActionID: w.writeID, ObjectID: w.objectID})
    if err != nil {
      w.csxClient = nil
      return err
    }
    w.csxClient = nil
 }
 return nil
}

func NewWriter(ctx context.Context, path, objectID, writeID string, csxClient csxgrpc.CSXClient) (*CSXWriter, error) {
  writer, err := os.Create(path)
  if err != nil {
    return nil, err
  }
  return &CSXWriter{
    ctx,
    objectID,
    writeID,
    path,
    writer,
    csxClient,
  }, nil
}

type CSXReader struct {
  ctx context.Context
  objectID string
  readID string
  path string
  reader io.ReadCloser
  csxClient csxgrpc.CSXClient
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
    _, err = r.csxClient.CloseRead(r.ctx, &csx.CloseReq{ActionID: r.readID, ObjectID: r.objectID})
    if err != nil {
      r.csxClient = nil
      return err
    }
    r.csxClient = nil
 }
 return nil
}

func NewReader(ctx context.Context, path, objectID, readID string, csxClient csxgrpc.CSXClient) (*CSXReader, error) {
  reader, err := os.Open(path)
  if err != nil {
    return nil, err
  }
  return &CSXReader{
    ctx,
    objectID,
    readID,
    path,
    reader,
    csxClient,
  }, nil
}
