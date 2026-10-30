package criu

// The checksum the mem-verify CRIU plugin sends when it has written the manifest of
// a checkpoint's files (cedana-designs/mem-verify.org).

import (
	"context"

	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/rs/zerolog/log"
)

// ManifestChecksum includes in nfy the callback that keeps the manifest's checksum
// the plugin sends on the notification "mem-verify-manifest", from CRIU's fini,
// before CRIU exits, and returns the getter: "" until it has come. An unreadable
// notice is logged and ignored, so that a dump never fails for its checksum.
func ManifestChecksum(nfy *NotifyCallbackMulti) func() string {
	var checksum string
	nfy.Include(&NotifyCallback{
		Name: "mem-verify",
		MemVerifyManifestFunc: func(ctx context.Context, payload []byte) error {
			notice, err := cedana_io.ParseManifestNotice(payload)
			if err != nil {
				log.Warn().Err(err).Msg("unreadable manifest notice from the mem-verify plugin")
				return nil
			}
			log.Info().Str("checksum", notice.Manifest).Int("files", notice.Files).Msg("the mem-verify plugin wrote the checkpoint's manifest")
			checksum = notice.Manifest
			return nil
		},
	})
	return func() string { return checksum }
}
