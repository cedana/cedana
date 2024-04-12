package containerdhelper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/archive"
	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/diff"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/pkg/epoch"
	"github.com/containerd/containerd/protobuf"
	"github.com/containerd/containerd/rootfs"
	"github.com/containerd/errdefs"
	"github.com/containerd/typeurl/v2"
	ver "github.com/opencontainers/image-spec/specs-go"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"google.golang.org/protobuf/proto"
)

const (
	checkpointImageNameLabel       = "org.opencontainers.image.ref.name"
	checkpointRuntimeNameLabel     = "ai.cedana.checkpoint.runtime"
	checkpointSnapshotterNameLabel = "ai.cedana.checkpoint.snapshotter"

	MediaTypeCedanaCheckpoint       = "application/ai.cedana.container.criu.checkpoint.criu.tar"
	MediaTypeCedanaCheckpointConfig = "application/ai.cedana.container.checkpoint.config.v1+proto"
)

// Checkpoint the RW image layers for migration and baseline image start rw layers
func CheckpointRW(ctx context.Context, client *containerd.Client, c *containers.Container, index *v1.Index) error {
	diffOpts := []diff.Opt{
		diff.WithReference(fmt.Sprintf("checkpoint-rw-%s", c.SnapshotKey)),
	}
	rw, err := rootfs.CreateDiff(ctx,
		c.SnapshotKey,
		client.SnapshotService(c.Snapshotter),
		client.DiffService(),
		diffOpts...,
	)
	if err != nil {
		return err

	}
	rw.Platform = &v1.Platform{
		OS:           runtime.GOOS,
		Architecture: runtime.GOARCH,
	}
	index.Manifests = append(index.Manifests, rw)
	return nil
}

// CheckpointImage just pulls the original image
func CheckpointImage(ctx context.Context, client *containerd.Client, c *containers.Container, index *v1.Index) error {
	ir, err := client.ImageService().Get(ctx, c.Image)
	if err != nil {
		return err
	}
	index.Manifests = append(index.Manifests, ir.Target)
	return nil
}

func PrepareIndex(ctx context.Context, c containerd.Container, client *containerd.Client, ref string) (*v1.Index, error) {
	index := &v1.Index{
		Versioned: ver.Versioned{
			SchemaVersion: 2,
		},
		Annotations: make(map[string]string),
	}

	// TODO verify all proper key values from context are being passed.
	info, err := c.Info(ctx)
	if err != nil {
		return nil, err
	}

	img, err := c.Image(ctx)
	if err != nil {
		return nil, nil
	}

	index.Annotations[checkpointImageNameLabel] = img.Name()
	index.Annotations[checkpointRuntimeNameLabel] = info.Runtime.Name
	index.Annotations[checkpointSnapshotterNameLabel] = info.Snapshotter

	if err := CheckpointImage(ctx, client, &info, index); err != nil {
		return nil, err
	}
	if err := CheckpointRW(ctx, client, &info, index); err != nil {
		return nil, err
	}

	return index, nil
}

func NewContainerdClient(ctx context.Context, opts ...containerd.ClientOpt) (*containerd.Client, context.Context, context.CancelFunc, error) {
	timeoutOpt := containerd.WithTimeout(0)
	containerdEndpoint := "/run/containerd/containerd.sock"
	if _, err := os.Stat(containerdEndpoint); err != nil {
		containerdEndpoint = "/host/run/k3s/containerd/containerd.sock"
	}
	opts = append(opts, timeoutOpt)

	client, err := containerd.New(containerdEndpoint, opts...)
	if err != nil {
		fmt.Print("failed to create client")
	}
	ctx, cancel := AppContext(ctx)
	return client, ctx, cancel, err
}

// AppContext returns the context for a command. Should only be called once per
// command, near the start.
//
// This will ensure the namespace is picked up and set the timeout, if one is
// defined.
func AppContext(kubeCtx context.Context) (context.Context, context.CancelFunc) {
	var (
		ctx       = context.Background()
		timeout   = 0
		namespace = "k8s.io"
		cancel    context.CancelFunc
	)
	ctx = namespaces.WithNamespace(ctx, namespace)
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	if tm, err := epoch.SourceDateEpoch(); err != nil {
	} else if tm != nil {
		ctx = epoch.WithSourceDateEpoch(ctx, tm)
	}
	return ctx, cancel
}

func writeIndex(ctx context.Context, index *v1.Index, client *containerd.Client, ref string) (d v1.Descriptor, err error) {
	labels := map[string]string{}
	for i, m := range index.Manifests {
		labels[fmt.Sprintf("containerd.io/gc.ref.content.%d", i)] = m.Digest.String()
	}
	data, err := json.Marshal(index)
	if err != nil {
		return v1.Descriptor{}, err
	}
	return writeContent(ctx, client.ContentStore(), v1.MediaTypeImageIndex, ref, bytes.NewReader(data), content.WithLabels(labels))
}

func writeContent(ctx context.Context, store content.Ingester, mediaType, ref string, r io.Reader, opts ...content.Opt) (d v1.Descriptor, err error) {
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

func WriteImageToContentStore(ctx context.Context, image string, client *containerd.Client) (d v1.Descriptor, err error) {
	tar := archive.Diff(ctx, "", image)
	cp, err := writeContent(ctx, client.ContentStore(), MediaTypeCedanaCheckpoint, image, tar)
	// close tar first after write
	if err := tar.Close(); err != nil {
		return v1.Descriptor{}, err
	}
	if err != nil {
		return v1.Descriptor{}, err
	}

	return cp, nil
}

func WriteSpecToContentStore(ctx context.Context, containerSpec typeurl.Any, image string, client *containerd.Client) (d v1.Descriptor, err error) {
	pbany := protobuf.FromAny(containerSpec)
	data, err := proto.Marshal(pbany)
	if err != nil {
		return v1.Descriptor{}, err
	}
	spec := bytes.NewReader(data)
	specD, err := writeContent(ctx, client.ContentStore(), MediaTypeCedanaCheckpointConfig, filepath.Join(image, "spec"), spec)
	if err != nil {
		return v1.Descriptor{}, errdefs.ToGRPC(err)
	}
	return specD, nil
}

func WriteIndex(ctx context.Context, index *v1.Index, client *containerd.Client, ref string) (d v1.Descriptor, err error) {
	labels := map[string]string{}
	for i, m := range index.Manifests {
		labels[fmt.Sprintf("containerd.io/gc.ref.content.%d", i)] = m.Digest.String()
	}
	data, err := json.Marshal(index)
	if err != nil {
		return v1.Descriptor{}, err
	}
	return writeContent(ctx, client.ContentStore(), v1.MediaTypeImageIndex, ref, bytes.NewReader(data), content.WithLabels(labels))

}
