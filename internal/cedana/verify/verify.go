// Package verify compares the checksum of a checkpoint as read on restore with the
// checksum recorded at dump. The restore adapters compute the value as they read
// the checkpoint; this package decides what a difference means and what it does.
package verify

import (
	"context"
	"fmt"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Mode returns the configured verification mode, one of config.CHECKSUM_VERIFY_*.
// An unknown value is taken as warn.
func Mode() string {
	switch mode := strings.ToLower(strings.TrimSpace(config.Global.Checkpoint.ChecksumVerify)); mode {
	case config.CHECKSUM_VERIFY_OFF, config.CHECKSUM_VERIFY_WARN, config.CHECKSUM_VERIFY_STRICT:
		return mode
	case "":
		return config.DEFAULT_CHECKPOINT_CHECKSUM_VERIFY
	default:
		log.Warn().Str("checksum_verify", mode).Msg("unknown checksum verification mode, using warn")
		return config.CHECKSUM_VERIFY_WARN
	}
}

// Check is the verification a restore asks for
type Check struct {
	Expected string // the checksum recorded at dump
	Strict   bool   // a mismatch fails the restore
}

// For returns the check a restore request asks for, or nil when there is nothing
// to verify: verification is off, the request has no expected checksum, or the
// expected checksum is of an algorithm this daemon does not compute.
func For(req *daemon.RestoreReq) *Check {
	expected := strings.TrimSpace(req.GetChecksum())
	mode := Mode()
	if expected == "" || mode == config.CHECKSUM_VERIFY_OFF {
		return nil
	}
	if !strings.HasPrefix(expected, cedana_io.CHECKSUM_ALGORITHM+":") {
		log.Warn().Str("expected", expected).Msg("checksum of another algorithm, not verified")
		return nil
	}
	return &Check{Expected: expected, Strict: mode == config.CHECKSUM_VERIFY_STRICT}
}

// Outcome is the result of one comparison
type Outcome struct {
	Actual string // the checksum of the bytes read, or the store's value when nothing was read
	Result daemon.ChecksumResult
	Reason daemon.ChecksumReason
}

// StoreValue returns the store's own whole-object checksum of a path on a remote
// storage, or "" when the storage is local, holds no value, or a composite one.
func StoreValue(ctx context.Context, storage cedana_io.Storage, path string) string {
	if storage == nil || !storage.IsRemote() {
		return ""
	}
	pc, ok := storage.(cedana_io.PathChecksummer)
	if !ok {
		return ""
	}
	value, err := pc.ChecksumPath(ctx, path)
	if err != nil {
		log.Debug().Err(err).Str("path", path).Msg("the store gave no checksum of the checkpoint")
		return ""
	}
	return value
}

// StoreManifest returns the manifest of the store's own values of a streamed
// checkpoint's shards, named as they are stored, or "" if any shard has none.
func StoreManifest(ctx context.Context, storage cedana_io.Storage, names []string, paths []string) string {
	if len(paths) == 0 || len(names) != len(paths) {
		return ""
	}
	entries := make([]cedana_io.ManifestEntry, 0, len(paths))
	for i, path := range paths {
		value := StoreValue(ctx, storage, path)
		if value == "" {
			return ""
		}
		entries = append(entries, cedana_io.ManifestEntry{Name: names[i], Checksum: value})
	}
	return cedana_io.ManifestChecksum(entries)
}

// StoreDiffers reports whether the store's own value is known and differs from the
// expected one: the stored bytes are not the bytes recorded, before any are read.
func (c *Check) StoreDiffers(store string) bool {
	return store != "" && store != c.Expected
}

// Compare compares the checksum of the bytes read with the expected one. On a
// mismatch it says where the difference comes from: the stored bytes, when the
// store's own value differs too or the checkpoint is local (a local file is its own
// store); the transfer, when the store's value matches; unknown, when the store has
// no value to compare.
func (c *Check) Compare(actual, store string, local bool) Outcome {
	if actual == c.Expected {
		return Outcome{Actual: actual, Result: daemon.ChecksumResult_CHECKSUM_MATCH, Reason: daemon.ChecksumReason_REASON_NONE}
	}
	reason := daemon.ChecksumReason_REASON_UNKNOWN
	switch {
	case local || c.StoreDiffers(store):
		reason = daemon.ChecksumReason_REASON_STORED
	case store == c.Expected:
		reason = daemon.ChecksumReason_REASON_TRANSFER
	}
	return Outcome{Actual: actual, Result: daemon.ChecksumResult_CHECKSUM_MISMATCH, Reason: reason}
}

// Stored is the outcome of a store value that differs before anything is read
func (c *Check) Stored(store string) Outcome {
	return Outcome{Actual: store, Result: daemon.ChecksumResult_CHECKSUM_MISMATCH, Reason: daemon.ChecksumReason_REASON_STORED}
}

// Retry reports whether a mismatch is worth one more read before the restore
// fails: in strict mode, when the stored bytes may be right.
func (c *Check) Retry(o Outcome) bool {
	return c.Strict && o.Result == daemon.ChecksumResult_CHECKSUM_MISMATCH &&
		o.Reason != daemon.ChecksumReason_REASON_STORED
}

// Record writes the outcome into the response and returns the error that fails
// the restore, in strict mode on a mismatch, or nil.
func (c *Check) Record(resp *daemon.RestoreResp, o Outcome) error {
	resp.Checksum = o.Actual
	resp.ChecksumResult = o.Result
	resp.ChecksumReason = o.Reason
	if o.Result != daemon.ChecksumResult_CHECKSUM_MISMATCH {
		log.Info().Str("checksum", o.Actual).Msg("checkpoint checksum verified")
		resp.Messages = append(resp.Messages, fmt.Sprintf("Checkpoint checksum verified: %s", o.Actual))
		return nil
	}
	message := fmt.Sprintf("checkpoint checksum mismatch (%s): recorded %s, read %s", ReasonName(o.Reason), c.Expected, o.Actual)
	log.Error().Str("expected", c.Expected).Str("actual", o.Actual).Str("reason", ReasonName(o.Reason)).Bool("strict", c.Strict).
		Msg("checkpoint checksum mismatch")
	resp.Messages = append(resp.Messages, message)
	if c.Strict {
		return status.Error(codes.FailedPrecondition, message)
	}
	return nil
}

// ReasonName is the reason as the propagator and the UI name it
func ReasonName(reason daemon.ChecksumReason) string {
	switch reason {
	case daemon.ChecksumReason_REASON_STORED:
		return "stored"
	case daemon.ChecksumReason_REASON_TRANSFER:
		return "transfer"
	case daemon.ChecksumReason_REASON_UNKNOWN:
		return "unknown"
	default:
		return ""
	}
}
