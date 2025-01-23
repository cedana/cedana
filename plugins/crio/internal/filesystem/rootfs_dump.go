package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"syscall"

	"fmt"
	"os"
	"path/filepath"

	// "time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	archive "github.com/containers/storage/pkg/archive"

	libconfig "github.com/cri-o/cri-o/pkg/config"
	// "github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	// "github.com/cedana/cedana/plugins/crio/pkg/keys"
	// "github.com/opencontainers/image-spec/identity"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func skipBindMount(mountPath string, specgen *specs.Spec) bool {
	for _, m := range specgen.Mounts {
		if m.Type != bindMount {
			continue
		}
		if m.Destination == mountPath {
			return true
		}
	}

	return false
}

// Adds a post-dump CRIU callback to dump the container's rootfs
// Using post-dump ensures that the container is in a frozen state
// Assumes client is already setup in context.
// TODO: Do rootfs dump parallel to CRIU dump, possible using multiple CRIU callbacks and synchronizing them
func DumpRootfs(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		details := req.GetDetails().GetCrio()
		var spec *specs.Spec
		root := filepath.Join("/var/lib/containers/storage/overlay-containers/", details.ID, "userdata/config.json")

		configFile, err := os.ReadFile(filepath.Join(root))
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(configFile, &spec); err != nil {
			return nil, err
		}

		diffPath, err := RootfsCheckpoint(ctx, details.ContainerStorage, req.Dir, details.ID, spec)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to dump rootfs: %v", err)
		}
		_ = diffPath

		return exited, nil
	}
}

// Copied from libpod/diff.go
var containerMounts = map[string]bool{
	"/dev":               true,
	"/dev/shm":           true,
	"/proc":              true,
	"/run":               true,
	"/run/.containerenv": true,
	"/run/secrets":       true,
	"/sys":               true,
}

const bindMount = "bind"
const rwChangesFile = "cedana-rwchanges.json"

func getImageService(ctx context.Context, libConfig *libconfig.Config) (imageService ImageServer, err error) {
	config := libConfig.GetData()
	store, err := libConfig.GetStore()
	if err != nil {
		return imageService, err
	}

	imageService, err = GetImageService(ctx, store, nil, config)
	if err != nil {
		return imageService, err
	}

	return imageService, err
}

func getDiff(config *libconfig.Config, ctrID string, specgen *specs.Spec) (rchanges []archive.Change, err error) {
	store, err := config.GetStore()
	if err != nil {
		return rchanges, err
	}

	ctr, err := store.Container(ctrID)
	if err != nil {
		return rchanges, err
	}

	layerID := ctr.LayerID

	changes, err := store.Changes("", layerID)
	if err == nil {
		for _, c := range changes {
			if skipBindMount(c.Path, specgen) {
				continue
			}
			if containerMounts[c.Path] {
				continue
			}
			rchanges = append(rchanges, c)
		}
	}

	return rchanges, err
}

const (
	// container archive
	ConfigDumpFile             = "config.dump"
	SpecDumpFile               = "spec.dump"
	StatusDumpFile             = "status.dump"
	NetworkStatusFile          = "network.status"
	CheckpointDirectory        = "checkpoint"
	CheckpointVolumesDirectory = "volumes"
	DevShmCheckpointTar        = "devshm-checkpoint.tar"
	RootFsDiffTar              = "rootfs-diff.tar"
	DeletedFilesFile           = "deleted.files"
	DumpLogFile                = "dump.log"
	RestoreLogFile             = "restore.log"
	// pod archive
	PodOptionsFile = "pod.options"
	PodDumpFile    = "pod.dump"
	// containerd only
	StatusFile = "status"
	// CRIU Images
	PagesPrefix       = "pages-"
	AmdgpuPagesPrefix = "amdgpu-pages-"
)

func getDefaultConfig() (*libconfig.Config, error) {
	config, err := libconfig.DefaultConfig()
	if err != nil {
		return config, fmt.Errorf("error loading server config: %w", err)
	}

	_, err = config.GetStore()
	if err != nil {
		return config, err
	}

	return config, nil
}

func RootfsCheckpoint(ctx context.Context, ctrDir, dest, ctrID string, specgen *specs.Spec) (string, error) {
	rwChangesPath := filepath.Join(ctrDir, rwChangesFile)

	includeFiles := []string{
		rwChangesFile,
	}

	config, err := getDefaultConfig()
	if err != nil {
		return "", err
	}

	rootFsChanges, err := getDiff(config, ctrID, specgen)
	if err != nil {
		return "", err
	}

	rootFsChangesJson, err := json.Marshal(rootFsChanges)
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(rwChangesPath, rootFsChangesJson, 0777); err != nil {
		return "", err
	}

	// 	defer os.Remove(rwChangesPath)

	is, err := getImageService(ctx, config)
	if err != nil {
		return "", err
	}

	mountPoint, err := is.GetStore().Mount(ctrID, specgen.Linux.MountLabel)
	if err != nil {
		return "", err
	}

	tmpFile, err := os.CreateTemp(ctrDir, "rootfs-tar-*.tar")
	if err != nil {
		return "", err
	}

	defer func() {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
	}()

	addToTarFiles, err := CRCreateRootFsDiffTar(&rootFsChanges, mountPoint, ctrDir, tmpFile)
	if err != nil {
		return "", err
	}

	includeFiles = append(includeFiles, addToTarFiles...)

	_, err = archive.TarWithOptions(ctrDir, &archive.TarOptions{
		// This should be configurable via api.proti
		Compression:      archive.Uncompressed,
		IncludeSourceDir: true,
		IncludeFiles:     includeFiles,
	})
	if err != nil {
		return "", fmt.Errorf("failed to tar newly created rootfs diff: %v", err)
	}

	rootfsDiffFile, err := os.CreateTemp(ctrDir, "rootfs-diff-*.tar")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("failed to create temporary file: %v\n", err)
	}

	log.Info().Msgf("Rootfs diff file name %s", rootfsDiffFile.Name())

	defer func() {
		if err := rootfsDiffFile.Close(); err != nil {
			log.Error().Msgf("Unable to close rootfs diff file %v", err)
		}
		if err := os.RemoveAll(rootfsDiffFile.Name()); err != nil {
			log.Error().Msgf("Unable to delete rootfs diff file %v", err)
		}
	}()

	tmpRootfsChangesDir, err := os.MkdirTemp("", "rootfs-changes-")
	if err != nil {
		return "", err
	}

	defer os.RemoveAll(tmpRootfsChangesDir)
	if err := UntarWithPermissions(tmpFile.Name(), tmpRootfsChangesDir); err != nil {
		return "", fmt.Errorf("failed to apply root file-system diff file %s: %w", tmpFile.Name(), err)
	}

	// We have to iterate over changes and change the ownership of files for containers that
	// may be user namespaced, like sysbox containers, which will have uid and gids of their init
	// process. This is an issue on restore as those files will have nogroup and will cause errors
	// when io is done. To avoid this we change the ownership of those files to 0:0 which will get
	// remapped via /proc/<pid>/gid_map and /proc/<pid>/uid_map orchestrated by crio userns feature.
	for _, change := range rootFsChanges {
		fullPath := filepath.Join(tmpRootfsChangesDir, change.Path)

		fileInfo, err := os.Lstat(fullPath)
		if err != nil {
			log.Debug().Msgf("failed to get file info for %s: %s", fullPath, err)
			continue
		}

		originalMode := fileInfo.Mode()

		perm := originalMode.Perm()

		// Extract the three missing special bits
		setuid := originalMode & os.ModeSetuid
		setgid := originalMode & os.ModeSetgid
		sticky := originalMode & os.ModeSticky

		if fileInfo.Mode()&os.ModeSymlink != 0 {
			// use syscall.Lchown to change the ownership of the link itself
			// syscall.chown only changes the ownership of the target file in a symlink.
			// In order to change the ownership of the symlink itself, we must use syscall.Lchown
			if err := syscall.Lchown(fullPath, 0, 0); err != nil {
				log.Debug().Msgf("failed to change ownership of symlink %s: %s", fullPath, err)
			}
			log.Debug().Msgf("\t mode is symlink: %s", fullPath)
		} else {
			if err := os.Chown(fullPath, 0, 0); err != nil {
				log.Debug().Msgf("failed to change ownership for %s: %s", fullPath, err)
			}
			log.Debug().Msgf("\t mode is regular: %s", fullPath)

			newMode := perm | setuid | setgid | sticky
			if err := os.Chmod(fullPath, newMode); err != nil {
				log.Error().Msgf("failed to restore permissions for %s: %s", fullPath, err)
			}
		}
	}

	if _, err := os.Stat(tmpRootfsChangesDir); os.IsNotExist(err) {
		log.Error().Msgf("Source directory %s does not exist: %v", tmpRootfsChangesDir, err)
	}

	// Create the tarball
	if err := createTarball(tmpRootfsChangesDir, rootfsDiffFile.Name()); err != nil {
		log.Error().Msgf("Error creating tarball: %v", err)
	}

	rootfsTar, err := archive.TarWithOptions(tmpRootfsChangesDir, &archive.TarOptions{
		// This should be configurable via api.proti
		Compression:      archive.Uncompressed,
		IncludeSourceDir: true,
	})
	if err != nil {
		return "", fmt.Errorf("untaring for rootfs file failed %q: %w", tmpRootfsChangesDir, err)
	}

	rootfsDiffFileMerge, err := os.CreateTemp(ctrDir, "rootfs-diff-*.tar")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("creating root file-system diff file %q: %w", rootfsDiffFileMerge.Name(), err)
	}

	log.Info().Msgf("Rootfs diff file name %s", rootfsDiffFileMerge.Name())

	if _, err = io.Copy(rootfsDiffFileMerge, rootfsTar); err != nil {
		return "", err
	}

	return rootfsDiffFileMerge.Name(), nil
}

// CRCreateRootFsDiffTar goes through the 'changes' and can create two files:
// * metadata.RootFsDiffTar will contain all new and changed files
// * metadata.DeletedFilesFile will contain a list of deleted files
// With these two files it is possible to restore the container file system to the same
// state it was during checkpointing.
// Changes to directories (owner, mode) are not handled.
func CRCreateRootFsDiffTar(changes *[]archive.Change, mountPoint, ctrDir string, rootfsDiffFile *os.File) (includeFiles []string, err error) {
	rootfsFileName := filepath.Base(rootfsDiffFile.Name())

	log.Info().Msg(rootfsDiffFile.Name())
	if len(*changes) == 0 {
		return includeFiles, nil
	}

	var rootfsIncludeFiles []string
	var deletedFiles []string

	for _, file := range *changes {
		if file.Kind == archive.ChangeAdd {
			rootfsIncludeFiles = append(rootfsIncludeFiles, file.Path)
			continue
		}
		if file.Kind == archive.ChangeDelete {
			deletedFiles = append(deletedFiles, file.Path)
			continue
		}
		fileName, err := os.Stat(file.Path)
		if err != nil {
			continue
		}
		if !fileName.IsDir() && file.Kind == archive.ChangeModify {
			rootfsIncludeFiles = append(rootfsIncludeFiles, file.Path)
			continue
		}
	}

	if len(rootfsIncludeFiles) > 0 {
		rootfsTar, err := archive.TarWithOptions(mountPoint, &archive.TarOptions{
			Compression:      archive.Uncompressed,
			IncludeSourceDir: true,
			IncludeFiles:     rootfsIncludeFiles,
		})
		if err != nil {
			return includeFiles, fmt.Errorf("exporting root file-system diff to %q: %w", rootfsDiffFile.Name(), err)
		}

		if _, err = io.Copy(rootfsDiffFile, rootfsTar); err != nil {
			return includeFiles, err
		}

		includeFiles = append(includeFiles, rootfsFileName)
	}

	if len(deletedFiles) == 0 {
		return includeFiles, nil
	}

	if _, err := WriteJSONFile(deletedFiles, ctrDir, DeletedFilesFile); err != nil {
		return includeFiles, nil
	}

	includeFiles = append(includeFiles, rootfsFileName)

	return includeFiles, nil
}

func WriteJSONFile(v interface{}, dir, file string) (string, error) {
	fileJSON, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("error marshalling JSON: %w", err)
	}
	file = filepath.Join(dir, file)
	if err := os.WriteFile(file, fileJSON, 0o600); err != nil {
		return "", err
	}

	return file, nil
}

func createTarball(sourceDir, tarPath string) error {
	baseDir := filepath.Dir(sourceDir)
	baseDirName := filepath.Base(sourceDir)

	cmd := exec.Command("tar", "-cvf", tarPath, baseDirName)
	cmd.Dir = baseDir
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to create tarball: %w", err)
	}
	return nil
}

func removeBuildahContainer(containerID string) error {
	removeCmd := exec.Command("buildah", "rm", containerID)
	err := removeCmd.Run()
	if err != nil {
		return err
	}

	return nil
}

func UntarWithPermissions(tarFile, destDir string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory %s: %w", destDir, err)
	}

	cmd := exec.Command("tar", "-xpf", tarFile, "-C", destDir)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to extract tarball %s: %w\nOutput: %s", tarFile, err, string(output))
	}

	return nil
}
