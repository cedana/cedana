package crio

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/containers/common/pkg/crutils"
	"github.com/containers/storage"
	archive "github.com/containers/storage/pkg/archive"
	"github.com/containers/storage/pkg/unshare"
	libconfig "github.com/cri-o/cri-o/pkg/config"
	"github.com/docker/docker/pkg/homedir"
	rspec "github.com/opencontainers/runtime-spec/specs-go"
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

func RootfsCheckpoint(ctx context.Context, ctrDir, dest, ctrID string, specgen *rspec.Spec) error {

	includeFiles := []string{
		"bind.mounts",
	}

	config, err := getDefaultConfig()
	if err != nil {
		return err
	}

	rootFsChanges, err := getDiff(config, ctrID, specgen)
	if err != nil {
		return err
	}

	is, err := getImageService(ctx, config)
	if err != nil {
		return err
	}

	mountPoint, err := is.GetStore().Mount(ctrID, specgen.Linux.MountLabel)

	addToTarFiles, err := crutils.CRCreateRootFsDiffTar(&rootFsChanges, mountPoint, dest)
	if err != nil {
		return err
	}

	includeFiles = append(includeFiles, addToTarFiles...)

	_, err = archive.TarWithOptions(ctrDir, &archive.TarOptions{
		// This should be configurable via api.proti
		Compression:      archive.Uncompressed,
		IncludeSourceDir: true,
		IncludeFiles:     includeFiles,
	})

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
