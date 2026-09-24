package cedana

import (
	"context"
	"sync"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/cedana/gpu"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const DEFERRED_FLUSH_ORPHAN_TIMEOUT = gpu.FLUSH_TIMEOUT + time.Minute

// Dump paths whose GPU image is still being written
var deferredFlushes sync.Map

func (s *Server) deferFlush(paths []string, d *gpu.DeferredFlushes) {
	for _, path := range paths {
		deferredFlushes.Store(path, d)
	}
	go func() {
		select {
		case <-time.After(DEFERRED_FLUSH_ORPHAN_TIMEOUT):
		case <-s.lifetime.Done():
		}
		if !forgetFlush(d) {
			return // FinishDump took it
		}
		log.Warn().Strs("paths", paths).Msg("FinishDump never called for a deferred GPU flush; finishing it")
		if err := d.Wait(context.WithoutCancel(s.lifetime)); err != nil {
			log.Error().Err(err).Strs("paths", paths).Msg("deferred GPU flush failed")
		}
	}()
}

func forgetFlush(d *gpu.DeferredFlushes) bool {
	found := false
	deferredFlushes.Range(func(key, value any) bool {
		if value == d {
			deferredFlushes.Delete(key)
			found = true
		}
		return true
	})
	return found
}

func (s *Server) FinishDump(ctx context.Context, req *daemon.FinishDumpReq) (*daemon.FinishDumpResp, error) {
	v, ok := deferredFlushes.Load(req.GetPath())
	if !ok {
		return &daemon.FinishDumpResp{Messages: []string{"Nothing pending for " + req.GetPath()}}, nil
	}
	d := v.(*gpu.DeferredFlushes)
	forgetFlush(d)

	start := time.Now()
	if err := d.Wait(ctx); err != nil {
		log.Error().Err(err).Str("path", req.GetPath()).Msg("failed to finish dump")
		return nil, status.Errorf(codes.Internal, "failed to finish writing the GPU checkpoint: %v", err)
	}
	log.Info().Str("path", req.GetPath()).Dur("waited", time.Since(start)).Msg("dump finished")
	return &daemon.FinishDumpResp{Messages: []string{"Finished " + req.GetPath()}}, nil
}
