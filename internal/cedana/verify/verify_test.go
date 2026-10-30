package verify

import (
	"context"
	"strings"
	"testing"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCompareClassifiesAMismatch(t *testing.T) {
	c := &Check{Expected: "crc32c:aaaaaaaa", Strict: true}
	for _, tc := range []struct {
		name   string
		actual string
		store  string
		local  bool
		result daemon.ChecksumResult
		reason daemon.ChecksumReason
	}{
		{"match", "crc32c:aaaaaaaa", "", false, daemon.ChecksumResult_CHECKSUM_MATCH, daemon.ChecksumReason_REASON_NONE},
		{"local file", "crc32c:bbbbbbbb", "", true, daemon.ChecksumResult_CHECKSUM_MISMATCH, daemon.ChecksumReason_REASON_STORED},
		{"store differs", "crc32c:bbbbbbbb", "crc32c:cccccccc", false, daemon.ChecksumResult_CHECKSUM_MISMATCH, daemon.ChecksumReason_REASON_STORED},
		{"store matches", "crc32c:bbbbbbbb", "crc32c:aaaaaaaa", false, daemon.ChecksumResult_CHECKSUM_MISMATCH, daemon.ChecksumReason_REASON_TRANSFER},
		{"store has none", "crc32c:bbbbbbbb", "", false, daemon.ChecksumResult_CHECKSUM_MISMATCH, daemon.ChecksumReason_REASON_UNKNOWN},
	} {
		o := c.Compare(tc.actual, tc.store, tc.local)
		if o.Result != tc.result || o.Reason != tc.reason {
			t.Errorf("%s: got %v %v, want %v %v", tc.name, o.Result, o.Reason, tc.result, tc.reason)
		}
		retry := tc.reason == daemon.ChecksumReason_REASON_TRANSFER || tc.reason == daemon.ChecksumReason_REASON_UNKNOWN
		if c.Retry(o) != retry {
			t.Errorf("%s: Retry = %t, want %t", tc.name, c.Retry(o), retry)
		}
	}
	if (&Check{Expected: c.Expected}).Retry(c.Compare("crc32c:bbbbbbbb", "", false)) {
		t.Error("warn mode must not read again")
	}
}

func TestRecordFailsOnlyAStrictMismatch(t *testing.T) {
	mismatch := Outcome{Actual: "crc32c:bbbbbbbb", Result: daemon.ChecksumResult_CHECKSUM_MISMATCH, Reason: daemon.ChecksumReason_REASON_TRANSFER}
	resp := &daemon.RestoreResp{}
	err := (&Check{Expected: "crc32c:aaaaaaaa", Strict: true}).Record(resp, mismatch)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("strict mismatch: err = %v", err)
	}
	if resp.Checksum != mismatch.Actual || resp.ChecksumResult != mismatch.Result || resp.ChecksumReason != mismatch.Reason {
		t.Fatalf("the response must hold the outcome, got %v", resp)
	}
	if !resp.ChecksumStrict {
		t.Fatal("a strict check must say so in the response")
	}
	warn := &daemon.RestoreResp{}
	if err := (&Check{Expected: "crc32c:aaaaaaaa"}).Record(warn, mismatch); err != nil {
		t.Fatalf("warn mismatch must not fail: %v", err)
	}
	if warn.ChecksumStrict {
		t.Fatal("a warn check must not say strict")
	}
	match := &daemon.RestoreResp{}
	(&Check{Expected: "crc32c:aaaaaaaa", Strict: true}).Record(match, Outcome{Actual: "crc32c:aaaaaaaa", Result: daemon.ChecksumResult_CHECKSUM_MATCH})
	if !match.ChecksumStrict {
		t.Fatal("the mode goes with every result, a match too")
	}
}

func TestForReadsTheModeAndTheRequest(t *testing.T) {
	previous := config.Global.Checkpoint.ChecksumVerify
	t.Cleanup(func() { config.Global.Checkpoint.ChecksumVerify = previous })

	req := &daemon.RestoreReq{Checksum: "crc32c:aaaaaaaa"}
	for mode, want := range map[string]*Check{
		config.CHECKSUM_VERIFY_OFF:    nil,
		config.CHECKSUM_VERIFY_WARN:   {Expected: req.Checksum},
		config.CHECKSUM_VERIFY_STRICT: {Expected: req.Checksum, Strict: true},
		"":                            {Expected: req.Checksum},
		"bogus":                       {Expected: req.Checksum},
	} {
		config.Global.Checkpoint.ChecksumVerify = mode
		got := For(req)
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Errorf("mode %q: For = %+v, want %+v", mode, got, want)
		}
	}
	config.Global.Checkpoint.ChecksumVerify = config.CHECKSUM_VERIFY_STRICT
	if For(&daemon.RestoreReq{}) != nil {
		t.Error("no expected checksum must check nothing")
	}
}

// The callback acts on what the mem-verify plugin reported, at pre-resume
func TestCallbackActsOnThePluginsResult(t *testing.T) {
	ctx := context.Background()
	run := func(strict bool, payload string) (*daemon.RestoreResp, error) {
		c := &Check{Expected: "crc32c:aaaaaaaa", Strict: strict}
		resp := &daemon.RestoreResp{}
		cb := c.Callback(resp)
		cb.PostRestore(ctx, 0) // no task to kill in a test
		if payload != "" {
			if err := cb.MemVerifyResult(ctx, []byte(payload)); err != nil {
				t.Fatal(err)
			}
		}
		return resp, cb.PreResume(ctx)
	}
	result := func(manifest string, mismatched int, failure string) string {
		failures := "[]"
		if failure != "" {
			failures = `["` + failure + `"]`
		}
		return `{"manifest":"` + manifest + `","mode":"warn","files":3,"verified":2,"mismatched":` + string(rune('0'+mismatched)) +
			`,"missing":0,"unverified":1,"gpu":0,"bytes":10,"failures":` + failures + `}`
	}

	// The manifest's checksum equals the recorded one and no file differs
	for _, strict := range []bool{false, true} {
		resp, err := run(strict, result("crc32c:aaaaaaaa", 0, ""))
		if err != nil || resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_MATCH || resp.Checksum != "crc32c:aaaaaaaa" || resp.ChecksumStrict != strict {
			t.Fatalf("strict=%t match: err=%v resp=%v", strict, err, resp)
		}
	}
	// Another manifest: the stored bytes differ
	resp, err := run(false, result("crc32c:bbbbbbbb", 0, ""))
	if err != nil || resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_MISMATCH || resp.ChecksumReason != daemon.ChecksumReason_REASON_STORED {
		t.Fatalf("warn, another manifest: err=%v resp=%v", err, resp)
	}
	resp, err = run(true, result("crc32c:bbbbbbbb", 0, ""))
	if status.Code(err) != codes.FailedPrecondition || resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_MISMATCH {
		t.Fatalf("strict, another manifest: err=%v resp=%v", err, resp)
	}
	// A file that differs, with the manifest's checksum right: strict names it
	resp, err = run(true, result("crc32c:aaaaaaaa", 1, "pages-1.img: expected crc32c:11111111, read crc32c:22222222"))
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), "pages-1.img") || resp.ChecksumReason != daemon.ChecksumReason_REASON_STORED {
		t.Fatalf("strict, a file differs: err=%v resp=%v", err, resp)
	}
	if resp, err := run(false, result("crc32c:aaaaaaaa", 1, "pages-1.img: expected crc32c:11111111, read crc32c:22222222")); err != nil || resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_MISMATCH {
		t.Fatalf("warn, a file differs: err=%v resp=%v", err, resp)
	}
	// No result, as under a CRIU without the plugin: warn goes on unverified, strict does not
	resp, err = run(false, "")
	if err != nil || resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_NOT_CHECKED {
		t.Fatalf("warn, no result: err=%v resp=%v", err, resp)
	}
	resp, err = run(true, "")
	if status.Code(err) != codes.FailedPrecondition || resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_NOT_CHECKED || !resp.ChecksumStrict {
		t.Fatalf("strict, no result: err=%v resp=%v", err, resp)
	}
	// An unreadable result counts as none
	if resp, err := run(false, "garbage"); err != nil || resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_NOT_CHECKED {
		t.Fatalf("unreadable result: err=%v resp=%v", err, resp)
	}
}
