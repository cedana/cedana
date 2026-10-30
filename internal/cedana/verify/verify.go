// Package verify compares the checksum of a checkpoint as read on restore with the
// checksum recorded at dump. The restore adapters compute the value as they read
// the checkpoint; this package decides what a difference means and what it does.
package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"syscall"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/criu"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Mode returns the configured verification mode, one of config.CHECKSUM_VERIFY_*.
// An unknown value is taken as warn.
func Mode() string {
	mode := config.ChecksumVerifyMode()
	if raw := strings.ToLower(strings.TrimSpace(config.Global.Checkpoint.ChecksumVerify)); raw != "" && raw != mode {
		log.Warn().Str("checksum_verify", raw).Msg("unknown checksum verification mode, using warn")
	}
	return mode
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
	// The mode goes with the result, so that the propagator acts on a stored mismatch
	// from strict mode only, whether or not the restore failed for it
	resp.ChecksumStrict = c.Strict
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

// Result is what the mem-verify CRIU plugin sends at the end of a restore, as the
// notification "mem-verify-result": the checksum of the manifest as it read it, which
// is the checkpoint's checksum, and what it found file by file
type Result struct {
	Manifest   string   `json:"manifest"`
	Mode       string   `json:"mode"`
	Files      int      `json:"files"`
	Verified   int      `json:"verified"`
	Mismatched int      `json:"mismatched"`
	Missing    int      `json:"missing"`
	Unverified int      `json:"unverified"`
	GPU        int      `json:"gpu"`
	Bytes      int64    `json:"bytes"`
	Failures   []string `json:"failures"`
}

// Callback returns the notify callback that takes the plugin's result and acts on it
// at pre-resume, before the restored tasks run: the manifest's checksum the plugin
// reports is compared with the expected value, and a per-file mismatch is a mismatch
// of the stored bytes. CRIU resumes the tasks whatever pre-resume returns, so in
// strict mode a mismatch kills them, as a failed GPU restore does. A restore the
// plugin did not report on, under a CRIU without it, is not verified: strict mode
// does not restore it.
func (c *Check) Callback(resp *daemon.RestoreResp) *criu.NotifyCallback {
	var result *Result
	var restored int32
	kill := func() {
		if restored > 0 {
			syscall.Kill(int(restored), syscall.SIGKILL)
		}
	}
	return &criu.NotifyCallback{
		Name: "mem-verify",
		PostRestoreFunc: func(ctx context.Context, pid int32) error {
			restored = pid
			return nil
		},
		MemVerifyResultFunc: func(ctx context.Context, payload []byte) error {
			r := &Result{}
			if err := json.Unmarshal(payload, r); err != nil {
				log.Warn().Err(err).Msg("unreadable result from the mem-verify plugin")
				return nil
			}
			result = r
			return nil
		},
		PreResumeFunc: func(ctx context.Context) error {
			if result == nil {
				resp.ChecksumResult = daemon.ChecksumResult_CHECKSUM_NOT_CHECKED
				resp.ChecksumStrict = c.Strict
				if c.Strict {
					kill()
					return status.Error(codes.FailedPrecondition, "checkpoint checksum not verified: no result from the mem-verify plugin, is it installed?")
				}
				log.Warn().Msg("no result from the mem-verify plugin, checkpoint not verified")
				return nil
			}
			log.Info().Str("manifest", result.Manifest).Int("files", result.Files).Int("verified", result.Verified).
				Int("mismatched", result.Mismatched).Int("missing", result.Missing).Int("unverified", result.Unverified).
				Int("gpu", result.GPU).Int64("bytes", result.Bytes).Msg("checkpoint files verified by the mem-verify plugin")
			o := c.Compare(result.Manifest, "", true)
			if o.Result == daemon.ChecksumResult_CHECKSUM_MATCH && result.Mismatched == 0 && result.Missing == 0 {
				return c.Record(resp, o)
			}
			o = Outcome{Actual: result.Manifest, Result: daemon.ChecksumResult_CHECKSUM_MISMATCH, Reason: daemon.ChecksumReason_REASON_STORED}
			first := fmt.Sprintf("manifest %s, recorded %s", result.Manifest, c.Expected)
			if len(result.Failures) > 0 {
				first = result.Failures[0]
			}
			if !c.Strict {
				log.Error().Str("first", first).Msg("checkpoint file mismatch, the restore goes on in warn mode")
				return c.Record(resp, o)
			}
			kill()
			c.Record(resp, o)
			return status.Errorf(codes.FailedPrecondition, "checkpoint file mismatch: %s", first)
		},
	}
}
