package filesystem

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
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
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		details := req.GetDetails().GetContainerd()

		if !(details.Rootfs || details.RootfsOnly) || req.Action != daemon.DumpAction_DUMP {
			return next(ctx, opts, resp, req)
		}

		image := details.Image

		if image == nil || image.Name == "" {
			return nil, status.Errorf(codes.InvalidArgument, "image name must be provided for rootfs dump")
		}

		client, ok := ctx.Value(containerd_keys.CLIENT_CONTEXT_KEY).(*containerd.Client)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get containerd client from context")
		}

		container, ok := ctx.Value(containerd_keys.CONTAINER_CONTEXT_KEY).(containerd.Container)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get container from context")
		}

		// When doing a rootfs dump only, we can return early after dumping the rootfs

		defer func() {
			if err == nil {
				resp.Messages = append(resp.Messages, "Dumped rootfs to "+image.Name)
			}
		}()

		if details.RootfsOnly {
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

			log.Debug().Str("container", details.ID).Msg("dumping rootfs only")

			_, end := profiling.StartTimingCategory(ctx, "rootfs", dumpRootfs)
			defer end()
			err = dumpRootfs(ctx, client, container, image.Name, image.Username, image.Secret)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to dump rootfs: %v", err)
			}

			return nil, nil // return early without calling next handler
		}

		// When doing a full dump, we instead start a rootfs dump async with CRIU and wait for it to finish

		rootfsErr := make(chan error, 1)

		opts.CRIUCallback.Include(&criu.NotifyCallback{
			Name: "rootfs",
			PreDumpFunc: func(ctx context.Context, opts *criu_proto.CriuOpts) error {
				go func() {
					rootfsErr <- dumpRootfs(ctx, client, container, image.Name, image.Username, image.Secret)
					close(rootfsErr)
				}()
				return nil
			},
			PostDumpFunc: func(ctx context.Context, opts *criu_proto.CriuOpts) error {
				return <-rootfsErr
			},
			FinalizeDumpFunc: func(ctx context.Context, opts *criu_proto.CriuOpts) error {
				return <-rootfsErr
			},
		})

		return next(ctx, opts, resp, req)
	}
}

func DumpImageName(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		if req.Action != daemon.DumpAction_DUMP || opts.DumpFs == nil {
			return next(ctx, opts, resp, req)
		}

		container, ok := ctx.Value(containerd_keys.CONTAINER_CONTEXT_KEY).(containerd.Container)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get container from context")
		}
		image, err := container.Image(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get image from container: %v", err)
		}

		file, err := opts.DumpFs.Create(containerd_keys.DUMP_IMAGE_NAME_KEY)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create dump image name file: %v", err)
		}
		defer file.Close()
		_, err = file.WriteString(image.Name())
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to write dump image name file: %v", err)
		}

		return next(ctx, opts, resp, req)
	}
}

func dumpRootfs(ctx context.Context, client *containerd.Client, container containerd.Container, ref, username, secret string) (err error) {
	log.Info().Str("ref", ref).Str("container", container.ID()).Msg("rootfs dump started")
	defer func() {
		if err != nil {
			log.Error().Err(err).Str("ref", ref).Str("container", container.ID()).Msg("rootfs dump failed")
		} else {
			log.Info().Str("ref", ref).Str("container", container.ID()).Msg("rootfs dump completed")
		}
	}()

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

	diffLayerDesc, diffID, err := createDiff(ctx, info.SnapshotKey, contentStore, snapshotter, differ)
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

	// If no username or secret is provided, we can try w/ local docker config as it may be a cli user
	if username == "" || secret == "" {
		log.Warn().Msgf("no username or secret provided, trying to read from docker config")
		username, secret, err = readDockerConfig(ref)
		if err != nil {
			log.Warn().Msgf("failed to read docker config: %v", err)
			return nil
		}
	}

	if err := pushImage(context.WithoutCancel(ctx), client, ref, username, secret); err != nil {
		log.Error().Msgf("failed to push image: %v", err)
	}

	log.Info().Msgf("pushed image %s successful", ref)

	return nil
}

type DockerConfig struct {
	Auths map[string]AuthEntry `json:"auths"`
}

type AuthEntry struct {
	Auth string `json:"auth"`
}

func readDockerConfig(imageName string) (string, string, error) {
	cfgPath := os.ExpandEnv("$HOME/.docker/config.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read docker config: %w", err)
	}

	var config DockerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return "", "", fmt.Errorf("failed to unmarshal docker config: %w", err)
	}

	registry := getRegistry(imageName)

	auth, ok := config.Auths[registry]
	if !ok {
		auth, ok = config.Auths["https://"+registry+"/v1/"]
		if !ok {
			return "", "", fmt.Errorf("no auth found for registry %s", registry)
		}
	}

	username, password, err := decodeAuth(auth.Auth)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode auth: %w", err)
	}
	return username, password, nil
}

func decodeAuth(auth string) (string, string, error) {
	decoded, err := base64.StdEncoding.DecodeString(auth)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode auth: %w", err)
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid auth format")
	}
	return parts[0], parts[1], nil
}

func getRegistry(imageRef string) string {
	parts := strings.Split(imageRef, "/")

	if len(parts) == 1 {
		return "index.docker.io"
	}
	if len(parts) == 2 {
		return "index.docker.io"
	}
	if len(parts) >= 3 && (strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":")) {
		if parts[0] == "docker.io" {
			return "index.docker.io"
		}
		return parts[0]
	}

	return "index.docker.io"
}
