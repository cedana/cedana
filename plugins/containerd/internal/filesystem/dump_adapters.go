package filesystem

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	gocriu "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	"github.com/containerd/containerd"
	containerdTypes "github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/archive"
	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/diff"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/rootfs"
	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/identity"
	is "github.com/opencontainers/image-spec/specs-go"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var MediaTypeCedanaDump = "application/vnd.containerd.container.cedana.checkpoint.cedana.tar"

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
			err := task.Resume(ctx)
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

func writeContent(ctx context.Context, mediaType, ref string, r io.Reader, client *containerd.Client) (*containerdTypes.Descriptor, error) {
	cs := client.ContentStore()
	writer, err := cs.Writer(ctx, content.WithRef(ref), content.WithDescriptor(v1.Descriptor{MediaType: mediaType}))
	if err != nil {
		return nil, err
	}
	defer writer.Close()
	size, err := io.Copy(writer, r)
	if err != nil {
		return nil, err
	}
	if err := writer.Commit(ctx, 0, ""); err != nil {
		return nil, err
	}
	return &containerdTypes.Descriptor{
		MediaType:   mediaType,
		Digest:      writer.Digest().String(),
		Size:        size,
		Annotations: make(map[string]string),
	}, nil
}

func writeIndex(ctx context.Context, client *containerd.Client, id string, index *v1.Index) (d v1.Descriptor, err error) {
	labels := map[string]string{}
	for i, m := range index.Manifests {
		labels[fmt.Sprintf("containerd.io/gc.ref.content.%d", i)] = m.Digest.String()
	}
	buf := bytes.NewBuffer(nil)
	if err := json.NewEncoder(buf).Encode(index); err != nil {
		return v1.Descriptor{}, err
	}
	return writeIndexContent(ctx, client.ContentStore(), v1.MediaTypeImageIndex, id, buf, content.WithLabels(labels))
}
func writeIndexContent(ctx context.Context, store content.Ingester, mediaType, ref string, r io.Reader, opts ...content.Opt) (d v1.Descriptor, err error) {
	writer, err := store.Writer(ctx, content.WithRef(ref))
	if err != nil {
		return d, err
	}
	defer writer.Close()
	size, err := io.Copy(writer, r)
	if err != nil {
		return d, err
	}

	if err := writer.Commit(ctx, size, "", opts...); err != nil {
		if !errdefs.IsAlreadyExists(err) {
			return d, err
		}
	}
	return v1.Descriptor{
		MediaType: mediaType,
		Digest:    writer.Digest(),
		Size:      size,
	}, nil
}

func CreateImage(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		dumpDir := req.GetDir()
		fullDumpDir := filepath.Join(dumpDir, req.Name)

		callback := &criu.NotifyCallback{
			PostDumpFunc: func(ctx context.Context, opts *gocriu.CriuOpts) error {
				client, err := containerd.New("/run/containerd/containerd.sock")
				if err != nil {
					return err
				}
				defer client.Close()
				containerdCtx := namespaces.WithNamespace(context.Background(), "k8s.io")

				ctr, err := client.ContainerService().Get(containerdCtx, req.Details.Runc.ID)
				if err != nil {
					return err
				}

				index := v1.Index{
					Versioned: is.Versioned{
						SchemaVersion: 2,
					},
					Annotations: make(map[string]string),
				}

				tar := archive.Diff(ctx, "", fullDumpDir)
				defer tar.Close()

				log.Debug().Str("container", ctr.ID).Msgf("creating image from dump %s, image media type %s", fullDumpDir, images.MediaTypeContainerd1Checkpoint)

				cp, err := writeContent(containerdCtx, MediaTypeCedanaDump, fullDumpDir, tar, client)
				// close tar first after write
				if err := tar.Close(); err != nil {
					return err
				}
				if err != nil {
					return err
				}

				descriptors := []*containerdTypes.Descriptor{
					cp,
				}

				for _, d := range descriptors {
					index.Manifests = append(index.Manifests, v1.Descriptor{
						MediaType: d.MediaType,
						Size:      d.Size,
						Digest:    digest.Digest(d.Digest),
						Platform: &v1.Platform{
							OS:           runtime.GOOS,
							Architecture: runtime.GOARCH,
						},
						Annotations: d.Annotations,
					})
				}

				index.Annotations["image.name"] = req.Details.Containerd.Image

				if ctr.SnapshotKey != "" {
					opts := []diff.Opt{
						diff.WithReference(fmt.Sprintf("checkpoint-rw-%s", ctr.SnapshotKey)),
					}

					rw, err := rootfs.CreateDiff(ctx, ctr.SnapshotKey, client.SnapshotService(ctr.Snapshotter), client.DiffService(), opts...)
					if err != nil {
						return err
					}

					rw.Platform = &v1.Platform{
						OS:           runtime.GOOS,
						Architecture: runtime.GOARCH,
					}

					index.Manifests = append(index.Manifests, rw)
				}

				log.Debug().Str("container", ctr.ID).Msgf("created image %s, image name %s", cp.Digest, ctr.Image)

				labels := map[string]string{}
				for i, m := range index.Manifests {
					labels[fmt.Sprintf("containerd.io/gc.ref.content.%d", i)] = m.Digest.String()
				}

				buf := bytes.NewBuffer(nil)
				if err := json.NewEncoder(buf).Encode(index); err != nil {
					return err
				}

				desc, err := writeIndex(ctx, client, req.Details.Runc.ID, &index)
				if err != nil {
					return err
				}

				im := images.Image{
					Name:   req.Details.Containerd.Image,
					Target: desc,
					Labels: map[string]string{
						"cedana.ai/checkpoint": "true",
					},
				}

				if im, err = client.ImageService().Create(ctx, im); err != nil {
					return err
				}

				return nil
			},
		}
		opts.CRIUCallback.Include(callback)

		exited, err = next(ctx, opts, resp, req)
		if err != nil {
			return nil, err
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
