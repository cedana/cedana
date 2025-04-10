package filesystem

import (
	"context"
	"fmt"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/images"
	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
	"github.com/opencontainers/image-spec/identity"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Adds a post-dump CRIU callback to dump the container's rootfs
// Using post-dump ensures that the container is in a frozen state
// Assumes client is already setup in context.
// TODO: Do rootfs dump parallel to CRIU dump, possible using multiple CRIU callbacks and synchronizing them
func DumpRootfs(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		details := req.GetDetails().GetContainerd()

		// Skip rootfs if image ref is not provided
		if details.Image == "" {
			return next(ctx, opts, resp, req)
		}

		client, ok := ctx.Value(containerd_keys.CLIENT_CONTEXT_KEY).(*containerd.Client)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get containerd client from context")
		}

		container, err := client.LoadContainer(ctx, details.ID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to load container %s: %v", details.ID, err)
		}

		info, err := container.Info(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get container info: %v", err)
		}

		if info.Image == details.Image {
			return nil, status.Errorf(codes.InvalidArgument, "dump image cannot be the same as the container image")
		}

		task, err := container.Task(ctx, nil)
		err = task.Pause(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to pause container %s: %v", details.ID, err)
		}
		defer func() {
			err := task.Resume(context.WithoutCancel(ctx))
			if err != nil {
				log.Error().Str("container", details.ID).Msg("failed to resume container")
			}
		}()

		// When doing a rootfs dump only, we can return early after dumping the rootfs

		defer func() {
			if err == nil {
				resp.Messages = append(resp.Messages, fmt.Sprintf("Dumped rootfs to image %s", details.Image))
			}
		}()

		if details.RootfsOnly {
			log.Debug().Str("container", details.ID).Msg("dumping rootfs only")

			_, end := profiling.StartTimingCategory(ctx, "rootfs", dumpRootfs)
			defer end()
			err = dumpRootfs(ctx, client, container, details.Image)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to dump rootfs: %v", err)
			}

			return nil, nil // return early without calling next handler
		}

		// When doing a full dump, we instead start a rootfs dump async and wait for it to finish

		rootfsErr := make(chan error, 1)

		go func() {
			rootfsErr <- dumpRootfs(ctx, client, container, details.Image)
		}()

		exited, err = next(ctx, opts, resp, req)
		if err != nil {
			<-rootfsErr // wait for rootfs dump cleanup
			return nil, err
		}

		// Since we are waiting on the rootfs dump, can add a component for it
		_, end := profiling.StartTimingCategory(ctx, "rootfs", dumpRootfs)
		err = <-rootfsErr
		end()

		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to dump rootfs: %v", err)
		}

		return exited, nil
	}
}

func dumpRootfs(ctx context.Context, client *containerd.Client, container containerd.Container, ref string) error {
	id := container.ID()

	info, err := container.Info(ctx)
	if err != nil {
		return fmt.Errorf("failed to get container info: %v", err)
	}

	baseImgNoPlatform, err := client.ImageService().Get(ctx, info.Image)
	if err != nil {
		return fmt.Errorf("failed to get container base image: %v", err)
	}

	platformLabel := platforms.DefaultString()
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
