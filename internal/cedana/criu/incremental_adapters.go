package criu

// Incremental (memory-tracked) CPU dumps.
//
// With track_mem, CRIU clears the kernel's soft-dirty bits for every task in the tree at the
// end of a dump. Given a parent image directory, the next track_mem dump writes only the pages
// dirtied since then and records the rest as "in parent" pagemap entries; restore follows the
// images' `parent` symlink to read them. At dump time CRIU reads only the parent's pagemap
// images, but restore needs the pages of every level, side by side.
//
// Correctness hinges on the parent being the dump at which the bits were last cleared (or a
// later one): if they were cleared after the parent was taken, the writes before that clear
// are neither in the parent nor marked dirty, and the restored memory silently misses them.
// A dump without track_mem never clears the bits, so it is harmless; a later restore makes
// every restored page dirty again, which is also safe (just not small).
//
// So the parent is never "the job's latest checkpoint". It is the last *successful* track_mem
// dump of this exact process (PID + start time), recorded here once the dump has fully
// succeeded, and forgotten before every track_mem attempt: CRIU may clear the bits and then
// fail later (e.g. compressing in the post-dump hook), and that dump must not become a parent.
//
// Only a dump written as a plain local directory can be a parent: compressed, remote and
// streamed images are not linkable (CRIU ignores parents when streaming). Such dumps still
// use track_mem, which only makes the next dump a full one.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// CRIU's image that every dump directory contains; its presence marks a usable parent.
const criuInventoryImage = "inventory.img"

type chainKey struct {
	pid       uint32
	startTime uint64
}

// Last successful track_mem dump directory per process (chainKey -> absolute path).
var incrementalChains sync.Map

// memTrackSupported reports whether the kernel supports soft-dirty memory tracking.
// A variable so tests can stub out the CRIU call.
var memTrackSupported = func(ctx context.Context, c *criu.Criu) (bool, error) {
	features, err := c.FeatureCheck(ctx, &criu_proto.CriuFeatures{MemTrack: proto.Bool(true)})
	if err != nil {
		return false, err
	}
	return features.GetMemTrack(), nil
}

// IncrementalForDump sets up the parent image directory for track_mem dumps.
// Needs the process state and the images directory, so it must run after
// FillProcessStateForDump and the storage/filesystem adapters.
func IncrementalForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		criuOpts := req.GetCriu()
		if criuOpts == nil || !criuOpts.GetTrackMem() {
			return next(ctx, opts, resp, req)
		}

		state := resp.GetState()
		key := chainKey{pid: state.GetPID(), startTime: state.GetStartTime()}
		imagesDir := criuOpts.GetImagesDir()

		log := log.With().Uint32("PID", key.pid).Str("dir", imagesDir).Logger()

		// This dump clears the bits whether or not it succeeds, so the previous link is
		// no longer a valid parent for anything after it.
		prev, _ := incrementalChains.LoadAndDelete(key)

		supported, err := memTrackSupported(ctx, opts.CRIU)
		if err != nil || !supported {
			log.Warn().Err(err).Msg("kernel lacks soft-dirty memory tracking, doing a full dump")
			resp.Messages = append(resp.Messages,
				"Incremental dump unsupported on this kernel (needs CONFIG_MEM_SOFT_DIRTY), dumped in full")
			criuOpts.TrackMem = nil
			criuOpts.ParentImg = nil
			return next(ctx, opts, resp, req)
		}

		linkable := isLinkableDump(opts, req)

		explicit := criuOpts.GetParentImg() != ""
		parent := criuOpts.GetParentImg()
		if !explicit && prev != nil {
			parent = prev.(string)
		}

		var parentAbs string
		if parent != "" {
			parentAbs = parent
			if !filepath.IsAbs(parentAbs) {
				parentAbs = filepath.Join(imagesDir, parentAbs)
			}
			parentAbs = filepath.Clean(parentAbs)

			var reason string
			if !linkable {
				reason = "compressed, remote or streamed dumps cannot link to a parent"
			} else if _, statErr := os.Stat(filepath.Join(parentAbs, criuInventoryImage)); statErr != nil {
				reason = fmt.Sprintf("parent %s is not a CRIU image directory: %v", parentAbs, statErr)
			}

			if reason != "" {
				if explicit {
					return nil, status.Errorf(codes.InvalidArgument, "incremental dump: %s", reason)
				}
				log.Warn().Str("parent", parentAbs).Msgf("%s, doing a full dump", reason)
				parent, parentAbs = "", ""
			} else {
				// CRIU symlinks <images>/parent to this path and warns that absolute
				// targets may not resolve on restore, so keep it relative.
				rel, relErr := filepath.Rel(imagesDir, parentAbs)
				if relErr != nil {
					return nil, status.Errorf(codes.Internal, "incremental dump: %v", relErr)
				}
				parent = rel
			}
		}

		if parent == "" {
			criuOpts.ParentImg = nil
			log.Info().Msg("incremental dump: no parent, dumping in full and starting a chain")
		} else {
			criuOpts.ParentImg = proto.String(parent)
			log.Info().Str("parent", parentAbs).Msg("incremental dump: writing only pages dirtied since the parent")
		}

		code, err = next(ctx, opts, resp, req)
		if err != nil {
			return code, err
		}

		if linkable {
			incrementalChains.Store(key, filepath.Clean(imagesDir))
		}
		if parentAbs != "" {
			resp.Messages = append(resp.Messages, "Incremental dump, unchanged memory is in parent "+parentAbs)
		}

		return code, nil
	}
}

// Whether the dump is written as a plain local directory that later dumps can link to.
// Mirrors the storage/filesystem adapters' choice of where the images go.
func isLinkableDump(opts types.Opts, req *daemon.DumpReq) bool {
	if opts.Storage != nil && opts.Storage.IsRemote() {
		return false
	}

	compression := req.GetCompression()
	if compression == "" {
		compression = config.Global.Checkpoint.Compression
	}
	if compression != "" && compression != "none" {
		return false
	}

	streams := req.GetStreams()
	if streams == 0 {
		streams = config.Global.Checkpoint.Streams
	}
	return streams <= 1 && req.GetCriu().GetImagesDir() != ""
}
