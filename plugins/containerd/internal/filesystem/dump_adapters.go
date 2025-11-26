package filesystem

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	containerd_proto "buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/containerd"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/images"
	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
	proto_proto "github.com/golang/protobuf/proto"
	"github.com/opencontainers/image-spec/identity"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func DumpRWLayer(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		client, ok := ctx.Value(containerd_keys.CLIENT_CONTEXT_KEY).(*containerd.Client)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get containerd client from context")
		}

		container, ok := ctx.Value(containerd_keys.CONTAINER_CONTEXT_KEY).(containerd.Container)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get container from context")
		}

		defer func() {
			if err == nil {
				log.Info().Str("container", container.ID()).Msg("Dumped rw layer successfully")
			}
		}()

		rwLayerErr := make(chan error, 1)

		opts.CRIUCallback.Include(&criu.NotifyCallback{
			Name: "rw-layer",
			PreDumpFunc: func(ctx context.Context, criuOpts *criu_proto.CriuOpts) error {
				go func() {
					log.Debug().Str("container", container.ID()).Msg("dumping rw layer")
					storagePath := req.Dir + "/" + req.Name
					rwLayerErr <- dumpRWLayer(ctx, opts.Storage, storagePath, client, container)
				}()
				return nil
			},
			PostDumpFunc: func(ctx context.Context, criuOpts *criu_proto.CriuOpts) error {
				return <-rwLayerErr
			},
		})

		return next(ctx, opts, resp, req)
	}
}

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

func writeDelimitedMessage(w io.Writer, rwLayerFile *containerd_proto.RWFile) error {
	data, err := proto_proto.Marshal(rwLayerFile)
	if err != nil {
		return err
	}

	size := uint32(len(data))
	if err := binary.Write(w, binary.LittleEndian, size); err != nil {
		return fmt.Errorf("failed to write message size: %w", err)
	}

	_, err = w.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write message data: %w", err)
	}
	return nil
}

type fileNode struct {
	path     string
	fi       os.FileInfo
	children []*fileNode
}

func buildFileTree(upperDir string, mapOfMounts map[string]string) (*fileNode, error) {
	root := &fileNode{path: upperDir}
	pathToNode := make(map[string]*fileNode)
	pathToNode[upperDir] = root

	err := filepath.WalkDir(upperDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if path == upperDir {
			return nil
		}

		if mapOfMounts[path] != "" {
			log.Warn().Str("file", path).Msg("skipping mount point in rw layer")
			return filepath.SkipDir
		}

		fi, err := d.Info()
		if err != nil {
			return fmt.Errorf("failed to get file info for %s: %v", path, err)
		}

		node := &fileNode{
			path: path,
			fi:   fi,
		}

		parentPath := filepath.Dir(path)
		parentNode, ok := pathToNode[parentPath]
		if !ok {
			return fmt.Errorf("parent node not found for %s", path)
		}

		parentNode.children = append(parentNode.children, node)
		pathToNode[path] = node

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to build file tree: %v", err)
	}

	return root, nil
}

func dumpRWLayer(ctx context.Context, storage cedana_io.Storage, storagePath string, client *containerd.Client, container containerd.Container) (err error) {
	log.Info().Str("container", container.ID()).Msg("rw layer dump started")
	defer func() {
		if err != nil {
			log.Error().Err(err).Str("container", container.ID()).Msg("rw layer dump failed")
		} else {
			log.Info().Str("container", container.ID()).Msg("rw layer dump completed")
		}
	}()

	info, err := container.Info(ctx)
	if err != nil {
		return fmt.Errorf("failed to get container info: %v", err)
	}

	snapshotter := client.SnapshotService(info.Snapshotter)

	mounts, err := snapshotter.Mounts(ctx, info.SnapshotKey)
	if err != nil {
		return fmt.Errorf("failed to get container mounts: %v", err)
	}

	spec, err := container.Spec(ctx)

	mapOfMounts := make(map[string]string)
	for _, m := range spec.Mounts {
		mapOfMounts[m.Destination] = m.Destination
	}

	var upperDir string
	for _, m := range mounts {
		if m.Type == "overlay" {
			for _, o := range m.Options {
				var found bool
				upperDir, found = strings.CutPrefix(o, "upperdir=")
				if found {
					break
				}
			}
		}
	}

	if upperDir == "" {
		log.Warn().Str("container", container.ID()).Msg("no upperdir found, skipping rw layer dump")
		return nil
	}

	log.Info().Str("upperDir", upperDir).Str("container", container.ID()).Msg("dumping rw layer from upperdir")

	log.Info().Msg("building file tree")
	root, err := buildFileTree(upperDir, mapOfMounts)
	if err != nil {
		return err
	}
	log.Info().Msg("file tree built successfully")

	const (
		NODES_PER_BATCH = 1000
		CHUNK_SIZE      = 64 * 1024
	)

	var (
		batchIndex    = 0
		nodeCount     = 0
		currentBatch  = make([]*containerd_proto.RWFile, 0, NODES_PER_BATCH)
		currentFiles  = make([]string, 0, NODES_PER_BATCH)
	)

	flushBatch := func() error {
		if len(currentBatch) == 0 {
			return nil
		}

		batchFileName := fmt.Sprintf("rw-layer-%d.img", batchIndex)
		filePath := storagePath + "/" + batchFileName
		outFile, err := storage.Create(filePath)
		if err != nil {
			return fmt.Errorf("failed to create batch file %s: %v", filePath, err)
		}
		defer outFile.Close()

		writer := bufio.NewWriterSize(outFile, CHUNK_SIZE)
		defer writer.Flush()

		for i, entry := range currentBatch {
			if err := writeDelimitedMessageBuffered(writer, entry); err != nil {
				return fmt.Errorf("failed to write entry metadata: %v", err)
			}

			if currentFiles[i] != "" {
				if err := writeFileContentBuffered(writer, currentFiles[i]); err != nil {
					return err
				}
			}
		}

		if err := writer.Flush(); err != nil {
			return fmt.Errorf("failed to flush writer: %v", err)
		}

		log.Debug().Str("file", batchFileName).Int("entries", len(currentBatch)).Msg("wrote rw layer batch")
		currentBatch = currentBatch[:0]
		currentFiles = currentFiles[:0]
		batchIndex++
		return nil
	}

	var traverse func(*fileNode) error
	traverse = func(node *fileNode) error {
		if node.path == upperDir {
			for _, child := range node.children {
				if err := traverse(child); err != nil {
					return err
				}
			}
			return nil
		}

		relPath, err := filepath.Rel(upperDir, node.path)
		if err != nil {
			return fmt.Errorf("failed to get relative path for %s: %v", node.path, err)
		}

		if strings.Contains(relPath, "libSvtAv1Enc") {
			log.Info().Str("relPath", relPath).Str("fullPath", node.path).Msg("FOUND libSvtAv1Enc during dump walk")
		}

		rwLayerFile := &containerd_proto.RWFile{}

		xattrSize, err := unix.Llistxattr(node.path, nil)
		if err != nil {
			return fmt.Errorf("failed to list xattrs for %s: %v", node.path, err)
		}

		if xattrSize == 0 {
			log.Debug().Str("file", node.path).Msg("no xattrs found")
		}

		xattrList := make([]byte, xattrSize)
		_, err = unix.Llistxattr(node.path, xattrList)
		if err != nil {
			return fmt.Errorf("failed to list xattrs for %s: %v", node.path, err)
		}

		xattrResults := make(map[string]string)
		offset := 0
		for offset < xattrSize {
			end := offset
			for end < xattrSize && xattrList[end] != 0 {
				end++
			}
			name := string(xattrList[offset:end])
			valueSize, err := unix.Lgetxattr(node.path, name, nil)
			if err != nil {
				return fmt.Errorf("failed to get xattr size for %s on %s: %v", name, node.path, err)
			}
			var value []byte
			if valueSize > 0 {
				value = make([]byte, valueSize)
				_, err = unix.Lgetxattr(node.path, name, value)
				if err != nil {
					return fmt.Errorf("failed to get xattr value for %s on %s: %v", name, node.path, err)
				}
			}
			xattrResults[name] = base64.StdEncoding.EncodeToString(value)
			offset = end + 1
		}
		rwLayerFile.SetXattrs(xattrResults)

		stat := node.fi.Sys().(*syscall.Stat_t)
		rwLayerFile.SetUid(stat.Uid)
		rwLayerFile.SetGid(stat.Gid)
		rwLayerFile.SetMode(uint32(node.fi.Mode()))
		rwLayerFile.SetPath(relPath)
		rwLayerFile.SetMtime(uint64(node.fi.ModTime().UnixNano()))

		if node.fi.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(node.path)
			if err != nil {
				return fmt.Errorf("failed to read symlink target for %s: %v", node.path, err)
			}
			rwLayerFile.SetSymlinkTarget(target)
		} else if node.fi.Mode()&os.ModeDevice != 0 {
			rwLayerFile.SetDevMajor(unix.Major(stat.Rdev))
			rwLayerFile.SetDevMinor(unix.Minor(stat.Rdev))
		} else if node.fi.Mode().IsDir() {
			log.Debug().Str("file", node.path).Str("mode", node.fi.Mode().String()).Msg("directory in rw layer")
		} else if node.fi.Mode().IsRegular() {
			log.Debug().Str("file", node.path).Str("mode", node.fi.Mode().String()).Msg("regular file in rw layer")
		} else {
			log.Warn().Str("file", node.path).Str("mode", node.fi.Mode().String()).Msg("unsupported file type in rw layer, skipping")
			return nil
		}

		currentBatch = append(currentBatch, rwLayerFile)
		if node.fi.Mode().IsRegular() {
			currentFiles = append(currentFiles, node.path)
			if strings.Contains(node.path, "libSvtAv1Enc") {
				log.Info().Str("fullPath", node.path).Str("relPath", relPath).Msg("ADDED libSvtAv1Enc to batch for dump")
			}
		} else {
			currentFiles = append(currentFiles, "")
		}

		nodeCount++

		if len(currentBatch) >= NODES_PER_BATCH {
			if err := flushBatch(); err != nil {
				return err
			}
		}

		for _, child := range node.children {
			if err := traverse(child); err != nil {
				return err
			}
		}

		return nil
	}

	if err := traverse(root); err != nil {
		return err
	}

	if len(currentBatch) > 0 {
		if err := flushBatch(); err != nil {
			return err
		}
	}

	manifestPath := storagePath + "/rw-layer-manifest.img"
	manifestFile, err := storage.Create(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to create manifest: %v", err)
	}
	defer manifestFile.Close()

	manifest := map[string]interface{}{
		"total_nodes":     nodeCount,
		"total_batches":   batchIndex,
		"nodes_per_batch": NODES_PER_BATCH,
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %v", err)
	}
	_, err = manifestFile.Write(manifestData)
	if err != nil {
		return fmt.Errorf("failed to write manifest: %v", err)
	}
	log.Info().Int("batches", batchIndex).Int("nodes", nodeCount).Msg("wrote rw layer manifest")

	return nil
}

func writeDelimitedMessageBuffered(w *bufio.Writer, rwLayerFile *containerd_proto.RWFile) error {
	data, err := proto_proto.Marshal(rwLayerFile)
	if err != nil {
		return err
	}

	size := uint32(len(data))
	if err := binary.Write(w, binary.LittleEndian, size); err != nil {
		return fmt.Errorf("failed to write message size: %w", err)
	}

	_, err = w.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write message data: %w", err)
	}
	return nil
}

func writeFileContentBuffered(w *bufio.Writer, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file %s for reading: %v", filePath, err)
	}
	defer f.Close()

	buf := make([]byte, 4*1024*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			chunkMsg := &containerd_proto.RWFile{}
			chunkData := make([]byte, n)
			copy(chunkData, buf[:n])
			chunkMsg.Content = [][]byte{chunkData}
			if err := writeDelimitedMessageBuffered(w, chunkMsg); err != nil {
				return fmt.Errorf("failed to write content chunk: %v", err)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read file content: %v", err)
		}
	}

	terminatorMsg := &containerd_proto.RWFile{}
	if err := writeDelimitedMessageBuffered(w, terminatorMsg); err != nil {
		return fmt.Errorf("failed to write terminator message: %v", err)
	}

	return nil
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
