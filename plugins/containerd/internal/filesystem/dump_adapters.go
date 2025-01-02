package filesystem

import (
	"context"
	"fmt"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/images"
	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
	"github.com/opencontainers/image-spec/identity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const CEDANA_PLATFORM = "cedana/platform"

// Adds a post-dump CRIU callback to dump the container's rootfs
// Using post-dump ensures that the container is in a frozen state
// Assumes client is already setup in context.
// TODO: Do rootfs dump parallel to CRIU dump, possible using multiple CRIU callbacks and synchronizing them
func AddRootfsToDump(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		// Skip rootfs if image ref is not provided
		if req.GetDetails().GetContainerd().GetImage() == "" {
			return next(ctx, server, resp, req)
		}

		details := req.GetDetails().GetContainerd()
		id := details.ID
		ref := details.Image

		client, ok := ctx.Value(containerd_keys.CLIENT_CONTEXT_KEY).(*containerd.Client)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get containerd client from context")
		}

		container, err := client.LoadContainer(ctx, id)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to load container %s: %v", id, err)
		}

		// Add CRIU callback for dumping rootfs

		callback := &criu.NotifyCallback{Name: "rootfs"}

		callback.PostDumpFunc = func(ctx context.Context, opts *criu_proto.CriuOpts) error {
			info, err := container.Info(ctx)
			if err != nil {
				return fmt.Errorf("failed to get container info: %v", err)
			}

			baseImgNoPlatform, err := client.ImageService().Get(ctx, info.Image)
			if err != nil {
				return fmt.Errorf("failed to get container image: %v", err)
			}

			platformLabel := info.Labels[CEDANA_PLATFORM]
			if platformLabel == "" {
				platformLabel = platforms.DefaultString()
			}

			ocispecPlatform, err := platforms.Parse(platformLabel)
			if err != nil {
				return err
			}

			platform := platforms.Only(ocispecPlatform)

			baseImg := containerd.NewImageWithPlatform(client, baseImgNoPlatform, platform)

			baseImgConfig, _, err := readImageConfig(ctx, baseImg)
			if err != nil {
				return err
			}

			var (
				differ       = client.DiffService()
				snapshotter  = client.SnapshotService(info.Snapshotter)
				contentStore = client.ContentStore()
			)

			diffLayerDesc, diffID, err := createDiff(ctx, id, contentStore, snapshotter, differ)
			if err != nil {
				return fmt.Errorf("failed to export layer: %w", err)
			}

			imageConfig, err := generateCommitImageConfig(ctx, container, baseImgConfig, diffID)
			if err != nil {
				return fmt.Errorf("failed to generate commit image config: %w", err)
			}
			rootfsID := identity.ChainID(imageConfig.RootFS.DiffIDs).String()

			if err := applyDiffLayer(ctx, rootfsID, baseImgConfig, snapshotter, differ, diffLayerDesc); err != nil {
				return fmt.Errorf("failed to apply diff: %w", err)
			}

			commitManifestDesc, _, err := writeContentsForImage(ctx, info.Snapshotter, baseImg, imageConfig, diffLayerDesc)
			if err != nil {
				return err
			}

			img := images.Image{
				Name:      ref,
				Target:    commitManifestDesc,
				CreatedAt: time.Now(),
			}

			if _, err := client.ImageService().Update(ctx, img); err != nil {
				if !errdefs.IsNotFound(err) {
					return err
				}

				if _, err := client.ImageService().Create(ctx, img); err != nil {
					return fmt.Errorf("failed to create new image %s: %w", ref, err)
				}
			}

			return nil
		}

		server.CRIUCallback.Include(callback)

		return next(ctx, server, resp, req)
	}
}
