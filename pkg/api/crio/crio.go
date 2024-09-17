package crio

import (
	"bytes"
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
	metadata "github.com/checkpoint-restore/checkpointctl/lib"
	"github.com/containers/common/pkg/auth"
	"github.com/containers/common/pkg/crutils"
	"github.com/containers/image/v5/types"
	archive "github.com/containers/storage/pkg/archive"
	libconfig "github.com/cri-o/cri-o/pkg/config"
	rspec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/zerolog/log"
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

func RootfsCheckpoint(ctx context.Context, ctrDir, dest, ctrID string, specgen *rspec.Spec) (string, error) {
	diffPath := filepath.Join(ctrDir, "rootfs-diff.tar")
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

	addToTarFiles, err := crutils.CRCreateRootFsDiffTar(&rootFsChanges, mountPoint, ctrDir)
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
		return "", err
	}

	_, err = os.Stat(diffPath)
	if err != nil {
		return "", err
	}

	rootfsDiffFile, err := os.Open(filepath.Join(ctrDir, metadata.RootFsDiffTar))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("failed to open root file-system diff file: %w", err)
	}
	defer rootfsDiffFile.Close()

	tmpRootfsChangesDir := filepath.Join(ctrDir, "rootfs-diff")
	if err := os.Mkdir(tmpRootfsChangesDir, 0777); err != nil {
		return "", err
	}

	defer os.RemoveAll(tmpRootfsChangesDir)
	if err := UntarWithPermissions(filepath.Join(ctrDir, metadata.RootFsDiffTar), tmpRootfsChangesDir); err != nil {
		return "", fmt.Errorf("failed to apply root file-system diff file %s: %w", ctrDir, err)
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

	if err := os.Remove(diffPath); err != nil {
		return "", err
	}

	if _, err := os.Stat(tmpRootfsChangesDir); os.IsNotExist(err) {
		log.Error().Msgf("Source directory %s does not exist: %v", tmpRootfsChangesDir, err)
	}

	// Create the tarball
	if err := createTarball(tmpRootfsChangesDir, diffPath); err != nil {
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

	rootfsDiffFile, err = os.Create(diffPath)
	if err != nil {
		return "", fmt.Errorf("creating root file-system diff file %q: %w", diffPath, err)
	}
	defer rootfsDiffFile.Close()
	if _, err = io.Copy(rootfsDiffFile, rootfsTar); err != nil {
		return "", err
	}

	_, err = os.Stat(diffPath)
	if err != nil {
		return "", err
	}

	return diffPath, nil
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

func removeAllContainers() {
	idsCmd := exec.Command("buildah", "containers", "-q")
	var out bytes.Buffer
	idsCmd.Stdout = &out

	err := idsCmd.Run()
	if err != nil {
		log.Error().Msgf("Failed to get container IDs: %s\n", err)
	}

	// Step 2: Remove each container by ID
	ids := strings.Fields(out.String())
	for _, id := range ids {
		removeCmd := exec.Command("buildah", "rm", id)
		err := removeCmd.Run()
		if err != nil {
			log.Error().Msgf("Failed to remove container %s: %s\n", id, err)
		} else {
			log.Debug().Msgf("Successfully removed container %s\n", id)
		}
	}

	log.Debug().Msgf("Finished removing all Buildah containers.")
}

func RootfsMerge(ctx context.Context, originalImageRef, newImageRef, rootfsDiffPath, containerStorage, registryAuthToken string) error {
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

	defer removeAllContainers()

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
	cmd = exec.Command("buildah", "commit", containerID, newImageRef)
	out, err = cmd.CombinedOutput()
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
