package crio

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/cedana/cedana/pkg/api/runc"
	"github.com/containers/buildah/pkg/parse"
	"github.com/containers/buildah/util"
	"github.com/containers/common/pkg/auth"
	"github.com/containers/image/v5/pkg/shortnames"
	is "github.com/containers/image/v5/storage"
	"github.com/containers/image/v5/transports/alltransports"
	"github.com/containers/image/v5/types"
	"github.com/containers/storage"
	archive "github.com/containers/storage/pkg/archive"
	libconfig "github.com/cri-o/cri-o/pkg/config"
	rspec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/zerolog/log"
	"github.com/sirupsen/logrus"
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

func skipBindMount(mountPath string, specgen *rspec.Spec) bool {
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

func getDiff(config *libconfig.Config, ctrID string, specgen *rspec.Spec) (rchanges []archive.Change, err error) {
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

func RootfsCheckpoint(ctx context.Context, ctrDir, dest, ctrID string, specgen *rspec.Spec) (string, error) {
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

func getStoreBuildah() (storage.Store, error) {
	options, err := storage.DefaultStoreOptions()
	if err != nil {
		return nil, err
	}

	// Do not allow to mount a graphdriver that is not vfs if we are creating the userns as part
	// of the mount command.
	// Differently, allow the mount if we are already in a userns, as the mount point will still
	// be accessible once "buildah mount" exits.
	if os.Geteuid() != 0 && options.GraphDriverName != "vfs" {
		return nil, fmt.Errorf("cannot mount using driver %s in rootless mode. You need to run it in a `buildah unshare` session", options.GraphDriverName)
	}

	store, err := storage.GetStore(options)
	if store != nil {
		is.Transport.SetStore(store)
	}
	return store, err
}

// Tail returns a string slice after the first element unless there are
// not enough elements, then it returns an empty slice.  This is to replace
// the urfavecli Tail method for args
func Tail(a []string) []string {
	if len(a) >= 2 {
		return a[1:]
	}
	return []string{}
}

func commit(args []string) error {
	var dest types.ImageReference
	if len(args) == 0 {
		return errors.New("container ID must be specified")
	}

	name := args[0]
	args = Tail(args)
	if len(args) > 1 {
		return errors.New("too many arguments specified")
	}
	image := ""
	if len(args) > 0 {
		image = args[0]
	}

	store, err := getStoreBuildah()
	if err != nil {
		return err
	}

	ctx := context.Background() // was context.to-do earlier

	builder, err := buildah.openBuilder(ctx, store, name)
	if err != nil {
		return fmt.Errorf("reading build container %q: %w", name, err)
	}

	systemContext, err := parse.SystemContextFromOptions(c)
	if err != nil {
		return fmt.Errorf("building system context: %w", err)
	}

	// If the user specified an image, we may need to massage it a bit if
	// no transport is specified.
	if image != "" {
		if dest, err = alltransports.ParseImageName(image); err != nil {
			candidates, err2 := shortnames.ResolveLocally(systemContext, image)
			if err2 != nil {
				return err2
			}
			if len(candidates) == 0 {
				return fmt.Errorf("parsing target image name %q", image)
			}
			dest2, err2 := is.Transport.ParseStoreReference(store, candidates[0].String())
			if err2 != nil {
				return fmt.Errorf("parsing target image name %q: %w", image, err)
			}
			dest = dest2
		}
	}

	id, ref, _, err := builder.Commit(ctx, dest)
	if err != nil {
		return util.GetFailureCause(err, fmt.Errorf("committing container %q to %q: %w", builder.Container, image, err))
	}
	if ref != nil && id != "" {
		logrus.Debugf("wrote image %s with ID %s", ref, id)
	} else if ref != nil {
		logrus.Debugf("wrote image %s", ref)
	} else if id != "" {
		logrus.Debugf("wrote image with ID %s", id)
	} else {
		logrus.Debugf("wrote image")
	}
	// similar to running buildah rm
	//	if iopts.rm {
	//	return builder.Delete()
	// }
	return nil
}

func buildahCommit(args []string) {
	commit(args)
}

// WARN:
// currently we are using buildah CLI for commits to images, there are various bugs in older
// versions of buildah, it is imperative we use the latest buildah binary (v1.37.3) which we
// explicitly checkout and build in the cedana dockerfile. It is assumed this version of buildah
// is used going forward.
func RootfsMerge(ctx context.Context, originalImageRef, newImageRef, rootfsDiffPath, containerStorage, registryAuthToken string) error {
	if _, err := os.Stat(rootfsDiffPath); err != nil {
		return err
	}

	defer func() {
		if err := os.Remove(rootfsDiffPath); err != nil {
			log.Error().Msgf("Unable to delete rootfs diff file %s, %v", rootfsDiffPath, err)
		}
	}()
	//buildah from original base ubuntu image
	if _, err := exec.LookPath("buildah"); err != nil {
		return fmt.Errorf("buildah is not installed")
	}

	systemContext := &types.SystemContext{}
	if registryAuthToken != "" {
		proxyEndpoint, err := getProxyEndpointFromImageName(originalImageRef)
		if err != nil {
			return err
		}
		if err := registryAuthLogin(ctx, systemContext, proxyEndpoint, registryAuthToken); err != nil {
			return err
		}
	}

	cmd := exec.Command("buildah", "from", originalImageRef)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("issue making working container: %s, %s", err.Error(), string(out))
	}

	// Split the output into lines
	lines := strings.Split(string(out), "\n")

	// Grab the last non-empty line
	var containerID string
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			containerID = lines[i]
			break
		}
	}

	defer removeBuildahContainer(containerID)

	// 	containerID = strings.ReplaceAll(containerID, "\n", "")

	//mount container
	log.Debug().Msgf("buildah mount of container %s", containerID)

	cmd = exec.Command("buildah", "mount", containerID)
	out, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("issue mounting working container: %s, %s", err.Error(), string(out))
	}

	containerRootDirectory := string(out)

	containerRootDirectory = strings.ReplaceAll(containerRootDirectory, "\n", "")

	rootfsDiffFile, err := os.Open(rootfsDiffPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to open root file-system diff file: %w", err)
	}

	defer rootfsDiffFile.Close()

	log.Debug().Msgf("applying rootfs diff to %s", containerRootDirectory)

	if err := UntarWithPermissions(rootfsDiffPath, containerRootDirectory); err != nil {
		return fmt.Errorf("failed to apply root file-system diff file %s: %w", rootfsDiffPath, err)
	}

	rwDiffJson := filepath.Join(containerStorage, rwChangesFile)
	rwDiffJsonDest := filepath.Join(containerRootDirectory, rwChangesFile)

	rwDiffFile, err := os.Open(rwDiffJson)
	if err != nil {
		return err
	}
	defer rwDiffFile.Close()

	rwDiffFileDest, err := os.Create(rwDiffJsonDest)
	if err != nil {
		return err
	}
	defer rwDiffFileDest.Close()

	_, err = io.Copy(rwDiffFileDest, rwDiffFile)
	if err != nil {
		return err
	}

	err = rwDiffFileDest.Sync()
	if err != nil {
		return err
	}

	log.Debug().Msgf("committing to %s", newImageRef)
	buildahCommit(containerID, newImageRef)
	if err != nil {
		return fmt.Errorf("issue committing image: %s, %s", err.Error(), string(out))
	}

	return nil
	//untar into storage root
}

func registryAuthLogin(ctx context.Context, systemContext *types.SystemContext, proxyEndpoint, authorizationToken string) error {
	loginOpts := &auth.LoginOptions{}
	loginArgs := []string{}

	decodedAuthBytes, err := base64.StdEncoding.DecodeString(authorizationToken)
	if err != nil {
		return err
	}

	decodedAuthString := string(decodedAuthBytes)

	parts := strings.Split(decodedAuthString, ":")

	if len(parts) != 2 {
		return fmt.Errorf("decoded auth string is not correctly formatted, %v", len(parts))
	}

	var stdoutBuilder strings.Builder

	loginOpts.Username = parts[0]
	loginOpts.Password = parts[1]
	loginOpts.Stdout = &stdoutBuilder

	loginArgs = append(loginArgs, proxyEndpoint)

	if err := auth.Login(ctx, systemContext, loginOpts, loginArgs); err != nil {
		return err
	}
	return nil
}

func getProxyEndpointFromImageName(imageName string) (string, error) {
	parts := strings.Split(imageName, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid image name format")
	}

	registryURL := parts[0]
	return "https://" + registryURL, nil
}

func ImagePush(ctx context.Context, newImageRef, registryAuthToken string) error {
	systemContext := &types.SystemContext{}

	if registryAuthToken != "" {
		proxyEndpoint, err := getProxyEndpointFromImageName(newImageRef)
		if err != nil {
			return err
		}
		if err := registryAuthLogin(ctx, systemContext, proxyEndpoint, registryAuthToken); err != nil {
			return err
		}
	}

	//buildah push
	cmd := exec.Command("buildah", "push", newImageRef)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("issue pushing image: %s, %s", err.Error(), string(out))
	}

	return nil
}

func SysboxChown(ctx context.Context, containerID, root string) error {
	rwChanges := &[]archive.Change{}

	pid, err := runc.GetPidByContainerId(containerID, root)
	if err != nil {
		return err
	}

	processRootfs := filepath.Join("/proc", string(pid), "root")
	rwChangesRootfs := filepath.Join(processRootfs, rwChangesFile)

	rwChangesBytes, err := os.ReadFile(rwChangesRootfs)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(rwChangesBytes, &rwChanges); err != nil {
		return err
	}

	log.Debug().Msgf("getting guid and uid of the init process pid %v", pid)
	guid, uid, err := getGUIDUID(pid)
	if err != nil {
		return err
	}

	log.Debug().Msgf("reverting ownership of rw change files to guid %v and uid %v in %s", guid, uid, processRootfs)
	for _, change := range *rwChanges {
		fullPath := filepath.Join(processRootfs, change.Path)
		if err := os.Chown(fullPath, guid, uid); err != nil {
			fmt.Printf("failed to change ownership for %s: %s", fullPath, err)
		}
	}

	return nil
}
