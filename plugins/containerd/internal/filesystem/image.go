package filesystem

// Utilties for container rootfs images

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/diff"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/remotes/docker"
	"github.com/containerd/containerd/rootfs"
	"github.com/containerd/containerd/snapshots"
	"github.com/containerd/errdefs"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/identity"
	"github.com/opencontainers/image-spec/specs-go"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog/log"
)

const (
	EMPTY_GZ_LAYER = digest.Digest("sha256:4f4fb700ef54461cfa02571ae0db9a0dc1e0cdb5577484a6d75e68dc38e8acc1")
	EMPTY_DIGEST   = digest.Digest("")
)

// ReadImage reads the image configuration from the containerd image
// and returns the OCI-compatible image configuration and descriptor
func readImageConfig(ctx context.Context, img containerd.Image) (v1.Image, v1.Descriptor, error) {
	var config v1.Image

	configDesc, err := img.Config(ctx) // aware of img.platform
	if err != nil {
		return config, configDesc, err
	}
	p, err := content.ReadBlob(ctx, img.ContentStore(), configDesc)
	if err != nil {
		return config, configDesc, err
	}
	if err := json.Unmarshal(p, &config); err != nil {
		return config, configDesc, err
	}
	return config, configDesc, nil
}

// CreateDiff creates a layer diff into containerd's content store.
func createDiff(ctx context.Context, id string, cs content.Store, sn snapshots.Snapshotter, differ diff.Comparer) (v1.Descriptor, digest.Digest, error) {
	newDesc, err := rootfs.CreateDiff(ctx, id, sn, differ)
	if err != nil {
		return v1.Descriptor{}, digest.Digest(""), err
	}

	info, err := cs.Info(ctx, newDesc.Digest)
	if err != nil {
		return v1.Descriptor{}, digest.Digest(""), err
	}

	diffIDStr, ok := info.Labels["containerd.io/uncompressed"]
	if !ok {
		return v1.Descriptor{}, digest.Digest(""), fmt.Errorf("invalid differ response with no diffID")
	}

	diffID, err := digest.Parse(diffIDStr)
	if err != nil {
		return v1.Descriptor{}, digest.Digest(""), err
	}

	return v1.Descriptor{
		MediaType: images.MediaTypeDockerSchema2LayerGzip,
		Digest:    newDesc.Digest,
		Size:      info.Size,
	}, diffID, nil
}

func generateCommitImageConfig(ctx context.Context, container containerd.Container, imgConfig v1.Image, diffID digest.Digest) (v1.Image, error) {
	spec, err := container.Spec(ctx)
	if err != nil {
		return v1.Image{}, err
	}

	createdBy := ""
	if spec.Process != nil {
		createdBy = strings.Join(spec.Process.Args, " ")
	}

	createdTime := time.Now()
	arch := imgConfig.Architecture
	if arch == "" {
		arch = runtime.GOARCH
	}
	os := imgConfig.OS
	if os == "" {
		os = runtime.GOOS
	}

	return v1.Image{
		Platform: v1.Platform{
			Architecture: arch,
			OS:           os,
		},

		Created: &createdTime,
		Author:  "cedana",
		Config:  imgConfig.Config,
		RootFS: v1.RootFS{
			Type:    "layers",
			DiffIDs: append(imgConfig.RootFS.DiffIDs, diffID),
		},
		History: append(imgConfig.History, v1.History{
			Created:    &createdTime,
			CreatedBy:  createdBy,
			Author:     "cedana",
			Comment:    "",
			EmptyLayer: (diffID == EMPTY_GZ_LAYER),
		}),
	}, nil
}

// ApplyDiffLayer will apply diff layer content created by createDiff into the snapshotter.
func applyDiffLayer(ctx context.Context, name string, baseImg v1.Image, sn snapshots.Snapshotter, differ diff.Applier, diffDesc v1.Descriptor) (retErr error) {
	var (
		key    = uniquePart() + "-" + name
		parent = identity.ChainID(baseImg.RootFS.DiffIDs).String()
	)

	mount, err := sn.Prepare(ctx, key, parent)
	if err != nil {
		return err
	}

	defer func() {
		if retErr != nil {
			// NOTE: the snapshotter should be hold by lease. Even
			// if the cleanup fails, the containerd gc can delete it.
			if err := sn.Remove(ctx, key); err != nil {
				log.Ctx(ctx).Debug().Msgf("failed to cleanup aborted apply %s: %s", key, err)
			}
		}
	}()

	if _, err = differ.Apply(ctx, diffDesc, mount); err != nil {
		return err
	}

	if err = sn.Commit(ctx, name, key); err != nil {
		if errdefs.IsAlreadyExists(err) {
			return nil
		}
		return err
	}

	return nil
}

// WriteContentsForImage will commit oci image config and manifest into containerd's content store.
func writeContentsForImage(ctx context.Context, snName string, baseImg containerd.Image, newConfig v1.Image, diffLayerDesc v1.Descriptor) (v1.Descriptor, digest.Digest, error) {
	newConfigJSON, err := json.Marshal(newConfig)
	if err != nil {
		return v1.Descriptor{}, EMPTY_DIGEST, err
	}

	configDesc := v1.Descriptor{
		MediaType: images.MediaTypeDockerSchema2Config,
		Digest:    digest.FromBytes(newConfigJSON),
		Size:      int64(len(newConfigJSON)),
	}

	cs := baseImg.ContentStore()
	baseMfst, _, err := readManifest(ctx, baseImg)
	if err != nil {
		return v1.Descriptor{}, EMPTY_DIGEST, err
	}
	layers := append(baseMfst.Layers, diffLayerDesc)

	newMfst := struct {
		MediaType string `json:"mediaType,omitempty"`
		v1.Manifest
	}{
		MediaType: images.MediaTypeDockerSchema2Manifest,
		Manifest: v1.Manifest{
			Versioned: specs.Versioned{
				SchemaVersion: 2,
			},
			Config: configDesc,
			Layers: layers,
		},
	}

	newMfstJSON, err := json.MarshalIndent(newMfst, "", "    ")
	if err != nil {
		return v1.Descriptor{}, EMPTY_DIGEST, err
	}

	newMfstDesc := v1.Descriptor{
		MediaType: images.MediaTypeDockerSchema2Manifest,
		Digest:    digest.FromBytes(newMfstJSON),
		Size:      int64(len(newMfstJSON)),
	}

	// new manifest should reference the layers and config content
	labels := map[string]string{
		"containerd.io/gc.ref.content.0": configDesc.Digest.String(),
	}
	for i, l := range layers {
		labels[fmt.Sprintf("containerd.io/gc.ref.content.%d", i+1)] = l.Digest.String()
	}

	err = content.WriteBlob(ctx, cs, newMfstDesc.Digest.String(), bytes.NewReader(newMfstJSON), newMfstDesc, content.WithLabels(labels))
	if err != nil {
		return v1.Descriptor{}, EMPTY_DIGEST, err
	}

	// config should reference to snapshotter
	labelOpt := content.WithLabels(map[string]string{
		fmt.Sprintf("containerd.io/gc.ref.snapshot.%s", snName): identity.ChainID(newConfig.RootFS.DiffIDs).String(),
	})
	err = content.WriteBlob(ctx, cs, configDesc.Digest.String(), bytes.NewReader(newConfigJSON), configDesc, labelOpt)
	if err != nil {
		return v1.Descriptor{}, EMPTY_DIGEST, err
	}

	return newMfstDesc, configDesc.Digest, nil
}

func readManifest(ctx context.Context, img containerd.Image) (*v1.Manifest, *v1.Descriptor, error) {
	cs := img.ContentStore()
	targetDesc := img.Target()
	if images.IsManifestType(targetDesc.MediaType) {
		b, err := content.ReadBlob(ctx, img.ContentStore(), targetDesc)
		if err != nil {
			return nil, &targetDesc, err
		}
		var mani v1.Manifest
		if err := json.Unmarshal(b, &mani); err != nil {
			return nil, &targetDesc, err
		}
		return &mani, &targetDesc, nil
	}
	if images.IsIndexType(targetDesc.MediaType) {
		idx, _, err := readIndex(ctx, img)
		if err != nil {
			return nil, nil, err
		}
		configDesc, err := img.Config(ctx) // aware of img.platform
		if err != nil {
			return nil, nil, err
		}

		for _, maniDesc := range idx.Manifests {
			maniDesc := maniDesc
			if b, err := content.ReadBlob(ctx, cs, maniDesc); err == nil {
				var mani v1.Manifest
				if err := json.Unmarshal(b, &mani); err != nil {
					return nil, nil, err
				}
				if reflect.DeepEqual(configDesc, mani.Config) {
					return &mani, &maniDesc, nil
				}
			}
		}
	}
	// no manifest was found
	return nil, nil, nil
}

// ReadIndex returns image index, or nil for non-indexed image.
func readIndex(ctx context.Context, img containerd.Image) (*v1.Index, *v1.Descriptor, error) {
	desc := img.Target()
	if !images.IsIndexType(desc.MediaType) {
		return nil, nil, nil
	}
	b, err := content.ReadBlob(ctx, img.ContentStore(), desc)
	if err != nil {
		return nil, &desc, err
	}
	var idx v1.Index
	if err := json.Unmarshal(b, &idx); err != nil {
		return nil, &desc, err
	}

	return &idx, &desc, nil
}

// yanked from github.com/containerd/containerd/rootfs/apply.go
func uniquePart() string {
	t := time.Now()
	var b [3]byte
	// Ignore read failures, just decreases uniqueness
	rand.Read(b[:])
	return fmt.Sprintf("%d-%s", t.Nanosecond(), base64.URLEncoding.EncodeToString(b[:]))
}

var PushTracker = docker.NewInMemoryTracker()

func pushImage(ctx context.Context, client *containerd.Client, ref, username, secret string) error {
	img, err := client.ImageService().Get(ctx, ref)
	if err != nil {
		return fmt.Errorf("unable to resolve image to manifest: %w", err)
	}

	resolver := docker.NewResolver(docker.ResolverOptions{
		Authorizer: docker.NewDockerAuthorizer(
			docker.WithAuthCreds(func(host string) (string, string, error) {
				return username, secret, nil
			}),
		),
		Tracker: PushTracker,
	})

	ropts := []containerd.RemoteOpt{
		containerd.WithResolver(resolver),
	}

	desc := img.Target

	return client.Push(ctx, ref, desc, ropts...)
}
