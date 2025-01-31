package pod

import (
	"context"
	"os"
	"path/filepath"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
)

func DumpPod(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		details := req.GetDetails()
		podSpec := details.GetPod().GetPodSpec()
		dumpDir := req.GetDir()

		podSpecDir := filepath.Join(dumpDir, "pod-spec.json")

		os.WriteFile(podSpecDir, []byte(podSpec), 0644)

		exited, err = next(ctx, opts, resp, req)
		if err != nil {
			return nil, err
		}
		return exited, nil
	}
}
