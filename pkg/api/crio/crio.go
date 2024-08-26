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
	"regexp"
	"strings"
	"syscall"

	"github.com/cedana/cedana/pkg/api/runc"
	"github.com/cedana/cedana/pkg/utils"
	metadata "github.com/checkpoint-restore/checkpointctl/lib"
	"github.com/containers/common/pkg/auth"
	"github.com/containers/common/pkg/crutils"
	"github.com/containers/image/v5/types"
	"github.com/containers/storage"
	archive "github.com/containers/storage/pkg/archive"
	"github.com/containers/storage/pkg/unshare"
	libconfig "github.com/cri-o/cri-o/pkg/config"
	"github.com/docker/docker/pkg/homedir"
	rspec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/zerolog"
)

var (
	// configuration, including customizations made in containers.conf
	needToShutdownStore = false
)

// setXDGRuntimeDir sets XDG_RUNTIME_DIR when if it is unset under rootless
func setXDGRuntimeDir() error {
	if unshare.IsRootless() && os.Getenv("XDG_RUNTIME_DIR") == "" {
		runtimeDir, err := homedir.GetRuntimeDir()
		if err != nil {
			return err
		}
		if err := os.Setenv("XDG_RUNTIME_DIR", runtimeDir); err != nil {
			return errors.New("could not set XDG_RUNTIME_DIR")
		}
	}
	return nil
}

func getStore() (store storage.Store, err error) {
	config, err := libconfig.DefaultConfig()
	if err != nil {
		return store, fmt.Errorf("error loading server config: %w", err)
	}

	store, err = config.GetStore()
	if err != nil {
		return store, err
	}

	return store, err
}

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
	logger, err := utils.GetLoggerFromContext(ctx)
	if err != nil {
		fmt.Printf(err.Error())
	}

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
	if err := archive.Untar(rootfsDiffFile, tmpRootfsChangesDir, nil); err != nil {
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
			logger.Debug().Msgf("failed to get file info for %s: %s", fullPath, err)
			continue
		}

		if fileInfo.Mode()&os.ModeSymlink != 0 {
			// use syscall.Lchown to change the ownership of the link itself
			// syscall.chown only changes the ownership of the target file in a symlink.
			// In order to change the ownership of the symlink itself, we must use syscall.Lchown
			if err := syscall.Lchown(fullPath, 0, 0); err != nil {
				logger.Debug().Msgf("failed to change ownership of symlink %s: %s", fullPath, err)
			}
			logger.Debug().Msgf("\t mode is symlink: %s", fullPath)
		} else {
			if err := os.Chown(fullPath, 0, 0); err != nil {
				logger.Debug().Msgf("failed to change ownership for %s: %s", fullPath, err)
			}
			logger.Debug().Msgf("\t mode is regular: %s", fullPath)

		}
	}

	if err := os.Remove(diffPath); err != nil {
		return "", err
	}

	rootfsTar, err := archive.TarWithOptions(tmpRootfsChangesDir, &archive.TarOptions{
		// This should be configurable via api.proti
		Compression:      archive.Uncompressed,
		IncludeSourceDir: true,
	})

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

func removeAllContainers(logger *zerolog.Logger) {
	idsCmd := exec.Command("buildah", "containers", "-q")
	var out bytes.Buffer
	idsCmd.Stdout = &out

	err := idsCmd.Run()
	if err != nil {
		logger.Error().Msgf("Failed to get container IDs: %s\n", err)
	}

	// Step 2: Remove each container by ID
	ids := strings.Fields(out.String())
	for _, id := range ids {
		removeCmd := exec.Command("buildah", "rm", id)
		err := removeCmd.Run()
		if err != nil {
			logger.Error().Msgf("Failed to remove container %s: %s\n", id, err)
		} else {
			logger.Debug().Msgf("Successfully removed container %s\n", id)
		}
	}

	logger.Debug().Msgf("Finished removing all Buildah containers.")
}

func RootfsMerge(ctx context.Context, originalImageRef, newImageRef, rootfsDiffPath, containerStorage, registryAuthToken string) error {
	logger, err := utils.GetLoggerFromContext(ctx)
	if err != nil {
		fmt.Printf(err.Error())
	}
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

	defer removeAllContainers(logger)

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
	logger.Debug().Msgf("buildah mount of container %s", containerID)

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

	logger.Debug().Msgf("applying rootfs diff to %s", containerRootDirectory)

	if err := archive.Untar(rootfsDiffFile, containerRootDirectory, nil); err != nil {
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

	logger.Debug().Msgf("committing to %s", newImageRef)
	cmd = exec.Command("buildah", "commit", containerID, newImageRef)
	out, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("issue committing image: %s, %s", err.Error(), string(out))
	}

	return nil
	//untar into storage root
}

// checks if the given image name is an ECR repository
func isECRRepo(imageName string) bool {
	return strings.Contains(imageName, ".ecr.") && strings.Contains(imageName, ".amazonaws.com")
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

func getRegionFromImageName(imageName string) (string, error) {
	re := regexp.MustCompile(`\.([a-z]+-[a-z]+-\d+)\.`)
	match := re.FindStringSubmatch(imageName)
	if len(match) > 1 {
		return match[1], nil
	}
	return "", fmt.Errorf("region not found in image name")
}

type loginReply struct {
	loginOpts auth.LoginOptions
	getLogin  bool
	tlsVerify bool
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

	logger, err := utils.GetLoggerFromContext(ctx)
	if err != nil {
		return err
	}

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

	logger.Debug().Msgf("getting guid and uid of the init process pid %v", pid)
	guid, uid, err := getGUIDUID(pid)
	if err != nil {
		return err
	}

	logger.Debug().Msgf("reverting ownership of rw change files to guid %v and uid %v in %s", guid, uid, processRootfs)
	for _, change := range *rwChanges {
		fullPath := filepath.Join(processRootfs, change.Path)
		if err := os.Chown(fullPath, guid, uid); err != nil {
			fmt.Printf("failed to change ownership for %s: %s", fullPath, err)
		}
	}

	return nil
}

type commitInputOptions struct {
	authfile           string
	omitHistory        bool
	blobCache          string
	certDir            string
	changes            []string
	configFile         string
	creds              string
	cwOptions          string
	disableCompression bool
	format             string
	iidfile            string
	manifest           string
	omitTimestamp      bool
	timestamp          int64
	quiet              bool
	referenceTime      string
	rm                 bool
	pull               string
	pullAlways         bool
	pullNever          bool
	sbomImgOutput      string
	sbomImgPurlOutput  string
	sbomMergeStrategy  string
	sbomOutput         string
	sbomPreset         string
	sbomPurlOutput     string
	sbomScannerCommand []string
	sbomScannerImage   string
	signaturePolicy    string
	signBy             string
	squash             bool
	tlsVerify          bool
	identityLabel      bool
	encryptionKeys     []string
	encryptLayers      []int
	unsetenvs          []string
	addFile            []string
}

// func Commit(ref string) error {
// 	var iopts commitInputOptions
// 	var dest imageTypes.ImageReference

// 	config, err := getDefaultConfig()
// 	if err != nil {
// 		return err
// 	}

// 	store, err := getStore()
// 	if err != nil {
// 		return err
// 	}

// 	ctx := context.TODO()

// 	builder, err := openBuilder(ctx, store, ref)
// 	if err != nil {
// 		return fmt.Errorf("reading build container %q: %w", ref, err)
// 	}

// 	// is, err := getImageService(ctx, config)
// 	// if err != nil {
// 	// 	return err
// 	// }

// 	systemContext := config.SystemContext
// 	if err != nil {
// 		return fmt.Errorf("building system context: %w", err)
// 	}

// 	// If the user specified an image, we may need to massage it a bit if
// 	// no transport is specified.
// 	if ref != "" {
// 		if dest, err = alltransports.ParseImageName(ref); err != nil {
// 			candidates, err2 := shortnames.ResolveLocally(systemContext, ref)
// 			if err2 != nil {
// 				return err2
// 			}
// 			if len(candidates) == 0 {
// 				return fmt.Errorf("parsing target image name %q", ref)
// 			}
// 			dest2, err2 := storageTransport.Transport.ParseStoreReference(store, candidates[0].String())
// 			if err2 != nil {
// 				return fmt.Errorf("parsing target image name %q: %w", ref, err)
// 			}
// 			dest = dest2
// 		}
// 	}

// 	// Add builder identity information.
// 	if iopts.identityLabel {
// 		builder.SetLabel(buildah.BuilderIdentityAnnotation, define.Version)
// 	}

// 	encConfig, encLayers, err := cli.EncryptConfig(iopts.encryptionKeys, iopts.encryptLayers)
// 	if err != nil {
// 		return fmt.Errorf("unable to obtain encryption config: %w", err)
// 	}

// 	var overrideConfig *manifest.Schema2Config

// 	var addFiles map[string]string
// 	if len(iopts.addFile) > 0 {
// 		addFiles = make(map[string]string)
// 		for _, spec := range iopts.addFile {
// 			specSlice := strings.SplitN(spec, ":", 2)
// 			if len(specSlice) == 1 {
// 				specSlice = []string{specSlice[0], specSlice[0]}
// 			}
// 			if len(specSlice) != 2 {
// 				return fmt.Errorf("parsing add-file argument %q: expected 1 or 2 parts, got %d", spec, len(strings.SplitN(spec, ":", 2)))
// 			}
// 			st, err := os.Stat(specSlice[0])
// 			if err != nil {
// 				return fmt.Errorf("parsing add-file argument %q: source %q: %w", spec, specSlice[0], err)
// 			}
// 			if st.IsDir() {
// 				return fmt.Errorf("parsing add-file argument %q: source %q is not a regular file", spec, specSlice[0])
// 			}
// 			addFiles[specSlice[1]] = specSlice[0]
// 		}
// 	}

// 	format, err := cli.GetFormat(iopts.format)
// 	compress := define.Gzip

// 	options := buildah.CommitOptions{
// 		PreferredManifestType: format,
// 		Manifest:              iopts.manifest,
// 		Compression:           compress,
// 		SignaturePolicyPath:   iopts.signaturePolicy,
// 		SystemContext:         systemContext,
// 		IIDFile:               iopts.iidfile,
// 		Squash:                iopts.squash,
// 		BlobDirectory:         iopts.blobCache,
// 		OmitHistory:           iopts.omitHistory,
// 		SignBy:                iopts.signBy,
// 		OciEncryptConfig:      encConfig,
// 		OciEncryptLayers:      encLayers,
// 		UnsetEnvs:             iopts.unsetenvs,
// 		OverrideChanges:       iopts.changes,
// 		OverrideConfig:        overrideConfig,
// 		ExtraImageContent:     addFiles,
// 	}
// 	exclusiveFlags := 0

// 	if iopts.omitTimestamp {
// 		exclusiveFlags++
// 		timestamp := time.Unix(0, 0).UTC()
// 		options.HistoryTimestamp = &timestamp
// 	}

// 	if exclusiveFlags > 1 {
// 		return errors.New("can not use more then one timestamp option at at time")
// 	}

// 	if !iopts.quiet {
// 		options.ReportWriter = os.Stderr
// 	}
// 	id, imageRef, _, err := builder.Commit(ctx, dest, options)
// 	if err != nil {
// 		return util.GetFailureCause(err, fmt.Errorf("committing container %q to %q: %w", builder.Container, ref, err))
// 	}
// 	if imageRef != nil && id != "" {
// 		logrus.Debugf("wrote image %s with ID %s", ref, id)
// 	} else if imageRef != nil {
// 		logrus.Debugf("wrote image %s", ref)
// 	} else if id != "" {
// 		logrus.Debugf("wrote image with ID %s", id)
// 	} else {
// 		logrus.Debugf("wrote image")
// 	}
// 	if options.IIDFile == "" && id != "" {
// 		fmt.Printf("%s\n", id)
// 	}

// 	if iopts.rm {
// 		return builder.Delete()
// 	}

// 	return nil

// }

// func openBuilder(ctx context.Context, store storage.Store, name string) (builder *buildah.Builder, err error) {
// 	if name != "" {
// 		builder, err = buildah.OpenBuilder(store, name)
// 		if errors.Is(err, os.ErrNotExist) {
// 			options := buildah.ImportOptions{
// 				Container: name,
// 			}
// 			builder, err = buildah.ImportBuilder(ctx, store, options)
// 		}
// 	}
// 	if err != nil {
// 		return nil, err
// 	}
// 	if builder == nil {
// 		return nil, errors.New("finding build container")
// 	}
// 	return builder, nil
// }
