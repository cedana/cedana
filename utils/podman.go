package utils

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/cedana/cedana/types"
	"github.com/docker/docker/pkg/stringid"
	bolt "go.etcd.io/bbolt"

	"github.com/sirupsen/logrus"

	rspec "github.com/opencontainers/runtime-spec/specs-go"
)

const (
	idRegistryName    = "id-registry"
	nameRegistryName  = "name-registry"
	ctrName           = "ctr"
	allCtrsName       = "all-ctrs"
	podName           = "pod"
	allPodsName       = "allPods"
	volName           = "vol"
	allVolsName       = "allVolumes"
	execName          = "exec"
	aliasesName       = "aliases"
	runtimeConfigName = "runtime-config"
	volumeCtrsName    = "volume-ctrs"

	exitCodeName          = "exit-code"
	exitCodeTimeStampName = "exit-code-time-stamp"

	configName         = "config"
	stateName          = "state"
	dependenciesName   = "dependencies"
	volCtrDependencies = "vol-dependencies"
	netNSName          = "netns"
	containersName     = "containers"
	podIDName          = "pod-id"
	networksName       = "networks"

	staticDirName   = "static-dir"
	tmpDirName      = "tmp-dir"
	runRootName     = "run-root"
	graphRootName   = "graph-root"
	graphDriverName = "graph-driver-name"
	osName          = "os"
	volPathName     = "volume-path"
)

var (
	IDRegistryBkt      = []byte(idRegistryName)
	NameRegistryBkt    = []byte(nameRegistryName)
	CtrBkt             = []byte(ctrName)
	AllCtrsBkt         = []byte(allCtrsName)
	PodBkt             = []byte(podName)
	AllPodsBkt         = []byte(allPodsName)
	VolBkt             = []byte(volName)
	AllVolsBkt         = []byte(allVolsName)
	ExecBkt            = []byte(execName)
	AliasesBkt         = []byte(aliasesName)
	RuntimeConfigBkt   = []byte(runtimeConfigName)
	DependenciesBkt    = []byte(dependenciesName)
	VolDependenciesBkt = []byte(volCtrDependencies)
	NetworksBkt        = []byte(networksName)
	VolCtrsBkt         = []byte(volumeCtrsName)

	ExitCodeBkt          = []byte(exitCodeName)
	ExitCodeTimeStampBkt = []byte(exitCodeTimeStampName)

	ConfigKey     = []byte(configName)
	StateKey      = []byte(stateName)
	NetNSKey      = []byte(netNSName)
	ContainersBkt = []byte(containersName)
	PodIDKey      = []byte(podIDName)

	StaticDirKey   = []byte(staticDirName)
	TmpDirKey      = []byte(tmpDirName)
	RunRootKey     = []byte(runRootName)
	GraphRootKey   = []byte(graphRootName)
	GraphDriverKey = []byte(graphDriverName)
	OsKey          = []byte(osName)
	VolPathKey     = []byte(volPathName)
)

func GetIDBucket(tx *bolt.Tx) (*bolt.Bucket, error) {
	bkt := tx.Bucket(IDRegistryBkt)
	if bkt == nil {
		return nil, fmt.Errorf("id registry bucket not found in DB")
	}
	return bkt, nil
}

func GetNamesBucket(tx *bolt.Tx) (*bolt.Bucket, error) {
	bkt := tx.Bucket(NameRegistryBkt)
	if bkt == nil {
		return nil, fmt.Errorf("name registry bucket not found in DB")
	}
	return bkt, nil
}

func GetCtrBucket(tx *bolt.Tx) (*bolt.Bucket, error) {
	bkt := tx.Bucket(CtrBkt)
	if bkt == nil {
		return nil, fmt.Errorf("containers bucket not found in DB")
	}
	return bkt, nil
}

func GetAllCtrsBucket(tx *bolt.Tx) (*bolt.Bucket, error) {
	bkt := tx.Bucket(AllCtrsBkt)
	if bkt == nil {
		return nil, fmt.Errorf("all containers bucket not found in DB")
	}
	return bkt, nil
}

func GetVolBucket(tx *bolt.Tx) (*bolt.Bucket, error) {
	bkt := tx.Bucket(VolBkt)
	if bkt == nil {
		return nil, fmt.Errorf("volumes bucket not found in DB")
	}
	return bkt, nil
}

func ReadJSONFile(v interface{}, dir, file string) (string, error) {
	file = filepath.Join(dir, file)
	content, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	if err = json.Unmarshal(content, v); err != nil {
		return "", fmt.Errorf("failed to unmarshal %s: %w", file, err)
	}

	return file, nil
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

func NewFromFile(path string) (*rspec.Spec, envCache, error) {
	cf, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("template configuration at %s not found", path)
		}
		return nil, nil, err
	}
	defer cf.Close()

	return NewFromTemplate(cf)
}

type envCache map[string]int

func NewFromTemplate(r io.Reader) (*rspec.Spec, envCache, error) {
	var config rspec.Spec
	if err := json.NewDecoder(r).Decode(&config); err != nil {
		return nil, nil, err
	}

	envCache := map[string]int{}
	if config.Process != nil {
		envCache = createEnvCacheMap(config.Process.Env)
	}

	return &config, envCache, nil
}

func createEnvCacheMap(env []string) map[string]int {
	envMap := make(map[string]int, len(env))
	for i, val := range env {
		envMap[val] = i
	}
	return envMap
}

// CRImportCheckpoint it the function which imports the information
// from checkpoint tarball and re-creates the container from that information
func CRImportCheckpoint(ctx context.Context, dir string) error {

	// Load spec.dump from temporary directory
	dumpSpec := new(rspec.Spec)
	if _, err := ReadJSONFile(dumpSpec, dir, "spec.dump"); err != nil {
		return err
	}

	ctrConfig := new(types.ContainerConfig)
	if _, err := ReadJSONFile(ctrConfig, dir, "config.dump"); err != nil {
		return err
	}
	ctrID := ctrConfig.ID

	ctrState := make(map[string]interface{})

	db := &DB{Conn: nil, DbPath: "/var/lib/containers/storage/libpod/bolt_state.db"}

	if err := db.SetNewDbConn(); err != nil {
		return err
	}

	defer db.Conn.Close()

	err := db.Conn.View(func(tx *bolt.Tx) error {
		bkt, err := GetCtrBucket(tx)
		if err != nil {
			return err
		}

		if err := db.GetContainerStateDB([]byte(ctrID), &ctrState, bkt); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}

	// This should not happen as checkpoints with these options are not exported.
	if len(ctrConfig.Dependencies) > 0 {
		return errors.New("cannot import checkpoints of containers with dependencies")
	}

	err = CreateContainer(&ctrState, ctrConfig, db)
	if err != nil {
		return err
	}

	return nil
}

// func CreateContainerStorage(imageName, containerName string) error {
// 	metadata := types.RuntimeContainerMetadata{
// 		ImageName:     imageName,
// 		ImageID:       "",
// 		ContainerName: containerName,
// 		CreatedAt:     time.Now().Unix(),
// 	}
// 	mdata, err := json.Marshal(&metadata)
// 	if err != nil {
// 		return err
// 	}

// 	return nil
// }

func CreateContainer(ctrState *map[string]interface{}, ctrConfig *types.ContainerConfig, db *DB) error {
	ctrId := stringid.GenerateRandomID()
	configNetworks := ctrConfig.Networks
	configJSON, err := json.Marshal(ctrConfig)
	if err != nil {
		return err
	}
	stateJSON, err := json.Marshal(ctrState)
	if err != nil {
		return err
	}

	ctrName := "test"

	networks := make(map[string][]byte, len(configNetworks))
	for net, opts := range configNetworks {
		// Check that we don't have any empty network names
		if net == "" {
			return fmt.Errorf("network names cannot be an empty string")
		}
		if opts.InterfaceName == "" {
			return fmt.Errorf("network interface name cannot be an empty string")
		}
		optBytes, err := json.Marshal(opts)
		if err != nil {
			return fmt.Errorf("marshalling network options JSON for container %s: %w", ctrId, err)
		}
		networks[net] = optBytes
	}

	err = db.Conn.Update(func(tx *bolt.Tx) error {
		idsBucket, err := GetIDBucket(tx)
		if err != nil {
			return err
		}

		namesBucket, err := GetNamesBucket(tx)
		if err != nil {
			return err
		}

		ctrBucket, err := GetCtrBucket(tx)
		if err != nil {
			return err
		}

		allCtrsBucket, err := GetAllCtrsBucket(tx)
		if err != nil {
			return err
		}

		volBkt, err := GetVolBucket(tx)
		if err != nil {
			return err
		}

		// Check if we already have a container with the given ID and name
		idExist := idsBucket.Get([]byte(ctrId))
		if idExist != nil {
			err = fmt.Errorf("container exists")
			if allCtrsBucket.Get(idExist) == nil {
				err = fmt.Errorf("pod exists")
			}
			return fmt.Errorf("ID \"%s\" is in use: %w", ctrId, err)
		}
		nameExist := namesBucket.Get([]byte(ctrName))
		if nameExist != nil {
			err = fmt.Errorf("container exists")
			if allCtrsBucket.Get(nameExist) == nil {
				err = fmt.Errorf("pod exists")
			}
			return fmt.Errorf("name \"%s\" is in use: %w", ctrName, err)
		}

		// No overlapping containers
		// Add the new container to the DB
		if err := idsBucket.Put([]byte(ctrId), []byte(ctrName)); err != nil {
			return fmt.Errorf("adding container %s ID to DB: %w", ctrId, err)
		}
		if err := namesBucket.Put([]byte(ctrId), []byte(ctrId)); err != nil {
			return fmt.Errorf("adding container %s name (%s) to DB: %w", ctrId, ctrName, err)
		}
		if err := allCtrsBucket.Put([]byte(ctrId), []byte(ctrName)); err != nil {
			return fmt.Errorf("adding container %s to all containers bucket in DB: %w", ctrId, err)
		}

		newCtrBkt, err := ctrBucket.CreateBucket([]byte(ctrId))
		if err != nil {
			return fmt.Errorf("adding container %s bucket to DB: %w", ctrId, err)
		}

		if err := newCtrBkt.Put(ConfigKey, configJSON); err != nil {
			return fmt.Errorf("adding container %s config to DB: %w", ctrId, err)
		}
		if err := newCtrBkt.Put(StateKey, stateJSON); err != nil {
			return fmt.Errorf("adding container %s state to DB: %w", ctrId, err)
		}

		if len(networks) > 0 {
			ctrNetworksBkt, err := newCtrBkt.CreateBucket(NetworksBkt)
			if err != nil {
				return fmt.Errorf("creating networks bucket for container %s: %w", ctrId, err)
			}
			for network, opts := range networks {
				if err := ctrNetworksBkt.Put([]byte(network), opts); err != nil {
					return fmt.Errorf("adding network %q to networks bucket for container %s: %w", network, ctrId, err)
				}
			}
		}

		if _, err := newCtrBkt.CreateBucket(DependenciesBkt); err != nil {
			return fmt.Errorf("creating dependencies bucket for container %s: %w", ctrId, err)
		}

		// Add dependencies for the container

		// Add ctr to pod

		// Add container to named volume dependencies buckets
		for _, vol := range ctrConfig.NamedVolumes {
			volDB := volBkt.Bucket([]byte(vol.Name))
			if volDB == nil {
				return fmt.Errorf("no volume with name %s found in database when adding container %s", vol.Name, ctrId)
			}

			ctrDepsBkt, err := volDB.CreateBucketIfNotExists(VolDependenciesBkt)
			if err != nil {
				return fmt.Errorf("creating volume %s dependencies bucket to add container %s: %w", vol.Name, ctrId, err)
			}
			if depExists := ctrDepsBkt.Get([]byte(ctrId)); depExists == nil {
				if err := ctrDepsBkt.Put([]byte(ctrId), []byte(ctrId)); err != nil {
					return fmt.Errorf("adding container %s to volume %s dependencies: %w", ctrId, vol.Name, err)
				}
			}
		}

		return nil
	})

	return nil
}

type DB struct {
	Conn   *bolt.DB
	DbLock sync.Mutex
	DbPath string
}

func (db *DB) SetNewDbConn() error {
	// We need an in-memory lock to avoid issues around POSIX file advisory
	// locks as described in the link below:
	// https://www.sqlite.org/src/artifact/c230a7a24?ln=994-1081
	db.DbLock.Lock()

	database, err := bolt.Open(db.DbPath, 0600, nil)
	if err != nil {
		return fmt.Errorf("opening database %s: %w", db.DbPath, err)
	}

	db.Conn = database

	return nil
}

func (s *DB) GetContainerConfigFromDB(id []byte, config *map[string]interface{}, ctrsBkt *bolt.Bucket) error {
	ctrBkt := ctrsBkt.Bucket(id)
	if ctrBkt == nil {
		return fmt.Errorf("container %s not found in DB", string(id))
	}

	configBytes := ctrBkt.Get(ConfigKey)
	if configBytes == nil {
		return fmt.Errorf("container %s missing config key in DB", string(id))
	}

	if err := json.Unmarshal(configBytes, &config); err != nil {
		return fmt.Errorf("unmarshalling container %s config: %w", string(id), err)
	}

	// TODO Move over these types
	// // convert ports to the new format if needed
	// if len(config.ContainerNetworkConfig.OldPortMappings) > 0 && len(config.ContainerNetworkConfig.PortMappings) == 0 {
	// 	config.ContainerNetworkConfig.PortMappings = ocicniPortsToNetTypesPorts(config.ContainerNetworkConfig.OldPortMappings)
	// 	// keep the OldPortMappings in case an user has to downgrade podman

	// 	// indicate that the config was modified and should be written back to the db when possible
	// 	config.rewrite = true
	// }

	return nil
}

func (s *DB) GetContainerStateDB(id []byte, state *map[string]interface{}, ctrsBkt *bolt.Bucket) error {
	ctrToUpdate := ctrsBkt.Bucket(id)

	newStateBytes := ctrToUpdate.Get(StateKey)
	if newStateBytes == nil {
		return fmt.Errorf("container does not have a state key in DB")
	}

	if err := json.Unmarshal(newStateBytes, &state); err != nil {
		return fmt.Errorf("unmarshalling container state: %w", err)
	}

	return nil
}

func podmanPatch() {

}

func initContainerVariables(rSpec *rspec.Spec, config *types.ContainerConfig) (*types.ContainerConfig, *types.ContainerState, error) {
	if rSpec == nil {
		return nil, nil, fmt.Errorf("must provide a valid runtime spec to create container")
	}
	config = new(types.ContainerConfig)
	state := new(types.ContainerState)

	if err := JSONDeepCopy(config, config); err != nil {
		return nil, nil, fmt.Errorf("copying container config for restore: %w", err)
	}
	// If the ID is empty a new name for the restored container was requested
	if config.ID == "" {
		config.ID = stringid.GenerateRandomID()
	}
	// Reset the log path to point to the default
	config.LogPath = ""
	// Later in validate() the check is for nil. JSONDeepCopy sets it to an empty
	// object. Resetting it to nil if it was nil before.
	if config.StaticMAC == nil {
		config.StaticMAC = nil
	}

	config.Spec = rSpec
	config.CreatedTime = time.Now()

	state.BindMounts = make(map[string]string)

	config.OCIRuntime = "/usr/bin/runc"

	return config, state, nil
}

func JSONDeepCopy(from, to interface{}) error {
	tmp, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(tmp, to)
}

// func setupContainer(ctx context.Context, ctrConfig *types.ContainerConfig, ctrState *types.ContainerState) (_ *Container, retErr error) {
// 	// Validate the container
// 	if ctrConfig.IsInfra {
// 		ctrConfig.StopTimeout = 10
// 	}

// 	// Inhibit shutdown until creation succeeds
// 	shutdown.Inhibit()
// 	defer shutdown.Uninhibit()

// 	// Allocate a lock for the container
// 	// lock, err := r.lockManager.AllocateLock()
// 	// if err != nil {
// 	// 	return nil, fmt.Errorf("allocating lock for new container: %w", err)
// 	// }
// 	// ctr.lock = lock
// 	// ctrConfig.LockID = ctr.lock.ID()
// 	// logrus.Debugf("Allocated lock %d for container %s", ctr.lock.ID(), ctrId)

// 	// defer func() {
// 	// 	if retErr != nil {
// 	// 		if err := ctr.lock.Free(); err != nil {
// 	// 			logrus.Errorf("Freeing lock for container after creation failed: %v", err)
// 	// 		}
// 	// 	}
// 	// }()

// 	ctrState.State = 1

// 	// Check Cgroup parent sanity, and set it if it was not set.
// 	// Only if we're actually configuring Cgroups.
// 	if !ctrConfig.NoCgroups {
// 		ctrConfig.CgroupManager = "systemd"

// 		if len(ctrConfig.CgroupParent) < 6 || !strings.HasSuffix(path.Base(ctrConfig.CgroupParent), ".slice") {
// 			return nil, fmt.Errorf("did not receive systemd slice as cgroup parent when using systemd to manage cgroups: %w", define.ErrInvalidArg)
// 		}

// 	}

// 	// Remove information about bind mount
// 	// for new container from imported checkpoint

// 	// NewFromSpec() is deprecated according to its comment
// 	// however the recommended replace just causes a nil map panic
// 	g := generate.NewFromSpec(ctrConfig.Spec)
// 	g.RemoveMount("/dev/shm")
// 	ctrConfig.ShmDir = ""
// 	g.RemoveMount("/etc/resolv.conf")
// 	g.RemoveMount("/etc/hostname")
// 	g.RemoveMount("/etc/hosts")
// 	g.RemoveMount("/run/.containerenv")
// 	g.RemoveMount("/run/secrets")
// 	g.RemoveMount("/var/run/.containerenv")
// 	g.RemoveMount("/var/run/secrets")

// 	// Regenerate Cgroup paths so they don't point to the old
// 	// container ID.
// 	cgroupPath, err := getOCICgroupPath(ctrConfig)
// 	if err != nil {
// 		return nil, err
// 	}

// 	// Set up storage for the container
// 	if err := setupStorage(ctrState, ctrConfig, ctx); err != nil {
// 		return nil, err
// 	}
// 	defer func() {
// 		if retErr != nil {
// 			if err := ctr.teardownStorage(); err != nil {
// 				logrus.Errorf("Removing partially-created container root filesystem: %v", err)
// 			}
// 		}
// 	}()

// 	ctrConfig.SecretsPath = filepath.Join(ctrConfig.StaticDir, "secrets")
// 	err = os.MkdirAll(ctrConfig.SecretsPath, 0755)
// 	if err != nil {
// 		return nil, err
// 	}
// 	for _, secr := range ctrConfig.Secrets {
// 		err = ctr.extractSecretToCtrStorage(secr)
// 		if err != nil {
// 			return nil, err
// 		}
// 	}

// 	if ctrConfig.ConmonPidFile == "" {
// 		ctrConfig.ConmonPidFile = filepath.Join(ctr.state.RunDir, "conmon.pid")
// 	}

// 	if ctrConfig.PidFile == "" {
// 		ctrConfig.PidFile = filepath.Join(ctr.state.RunDir, "pidfile")
// 	}

// 	// Go through named volumes and add them.
// 	// If they don't exist they will be created using basic options.
// 	// Maintain an array of them - we need to lock them later.
// 	ctrNamedVolumes := make([]*Volume, 0, len(ctrConfig.NamedVolumes))
// 	for _, vol := range ctrConfig.NamedVolumes {
// 		isAnonymous := false
// 		if vol.Name == "" {
// 			// Anonymous volume. We'll need to create it.
// 			// It needs a name first.
// 			vol.Name = stringid.GenerateRandomID()
// 			isAnonymous = true
// 		} else {
// 			// Check if it already exists
// 			dbVol, err := r.state.Volume(vol.Name)
// 			if err == nil {
// 				ctrNamedVolumes = append(ctrNamedVolumes, dbVol)
// 				// The volume exists, we're good
// 				continue
// 			} else if !errors.Is(err, define.ErrNoSuchVolume) {
// 				return nil, fmt.Errorf("retrieving named volume %s for new container: %w", vol.Name, err)
// 			}
// 		}
// 		if vol.IsAnonymous {
// 			// If SetAnonymous is true, make this an anonymous volume
// 			// this is needed for emptyDir volumes from kube yamls
// 			isAnonymous = true
// 		}

// 		logrus.Debugf("Creating new volume %s for container", vol.Name)

// 		// The volume does not exist, so we need to create it.
// 		volOptions := []VolumeCreateOption{
// 			WithVolumeName(vol.Name),
// 			WithVolumeMountLabel(ctr.MountLabel()),
// 		}
// 		if isAnonymous {
// 			volOptions = append(volOptions, withSetAnon())
// 		}

// 		needsChown := true

// 		// If volume-opts are set, parse and add driver opts.
// 		if len(vol.Options) > 0 {
// 			isDriverOpts := false
// 			driverOpts := make(map[string]string)
// 			for _, opts := range vol.Options {
// 				if opts == "idmap" {
// 					needsChown = false
// 				}
// 				if strings.HasPrefix(opts, "volume-opt") {
// 					isDriverOpts = true
// 					driverOptKey, driverOptValue, err := util.ParseDriverOpts(opts)
// 					if err != nil {
// 						return nil, err
// 					}
// 					driverOpts[driverOptKey] = driverOptValue
// 				}
// 			}
// 			if isDriverOpts {
// 				parsedOptions := []VolumeCreateOption{WithVolumeOptions(driverOpts)}
// 				volOptions = append(volOptions, parsedOptions...)
// 			}
// 		}

// 		if needsChown {
// 			volOptions = append(volOptions, WithVolumeUID(ctr.RootUID()), WithVolumeGID(ctr.RootGID()))
// 		} else {
// 			volOptions = append(volOptions, WithVolumeNoChown())
// 		}

// 		newVol, err := r.newVolume(ctx, false, volOptions...)
// 		if err != nil {
// 			return nil, fmt.Errorf("creating named volume %q: %w", vol.Name, err)
// 		}

// 		ctrNamedVolumes = append(ctrNamedVolumes, newVol)
// 	}

// 	switch ctrConfig.LogDriver {
// 	case define.NoLogging, define.PassthroughLogging, define.JournaldLogging:
// 		break
// 	default:
// 		if ctrConfig.LogPath == "" {
// 			ctrConfig.LogPath = filepath.Join(ctrConfig.StaticDir, "ctr.log")
// 		}
// 	}

// 	if useDevShm && !MountExists(ctrConfig.Spec.Mounts, "/dev/shm") && ctrConfig.ShmDir == "" && !ctrConfig.NoShm {
// 		ctrConfig.ShmDir = filepath.Join(ctr.bundlePath(), "shm")
// 		if err := os.MkdirAll(ctrConfig.ShmDir, 0700); err != nil {
// 			if !os.IsExist(err) {
// 				return nil, fmt.Errorf("unable to create shm dir: %w", err)
// 			}
// 		}
// 		ctrConfig.Mounts = append(ctrConfig.Mounts, ctrConfig.ShmDir)
// 	}

// 	// Lock all named volumes we are adding ourself to, to ensure we can't
// 	// use a volume being removed.
// 	volsLocked := make(map[string]bool)
// 	for _, namedVol := range ctrNamedVolumes {
// 		toLock := namedVol
// 		// Ensure that we don't double-lock a named volume that is used
// 		// more than once.
// 		if volsLocked[namedVol.Name()] {
// 			continue
// 		}
// 		volsLocked[namedVol.Name()] = true
// 		toLock.lock.Lock()
// 		defer toLock.lock.Unlock()
// 	}
// 	// Add the container to the state
// 	// TODO: May be worth looking into recovering from name/ID collisions here
// 	if ctrConfig.Pod != "" {
// 		// Lock the pod to ensure we can't add containers to pods
// 		// being removed
// 		pod.lock.Lock()
// 		defer pod.lock.Unlock()

// 		if err := r.state.AddContainerToPod(pod, ctr); err != nil {
// 			return nil, err
// 		}
// 	} else if err := r.state.AddContainer(ctr); err != nil {
// 		return nil, err
// 	}

// 	if ctr.runtime.config.Engine.EventsContainerCreateInspectData {
// 		if err := ctr.newContainerEventWithInspectData(events.Create, true); err != nil {
// 			return nil, err
// 		}
// 	} else {
// 		ctr.newContainerEvent(events.Create)
// 	}
// 	return ctr, nil
// }

func getOCICgroupPath(config *types.ContainerConfig) (string, error) {

	// When the OCI runtime is set to use Systemd as a cgroup manager, it
	// expects cgroups to be passed as follows:
	// slice:prefix:name
	systemdCgroups := fmt.Sprintf("%s:libpod:%s", path.Base(config.CgroupParent))
	logrus.Debugf("Setting Cgroups for container to %s", systemdCgroups)
	return systemdCgroups, nil
}

// func setupStorage(ctrState *types.ContainerState, ctrConfig *types.ContainerConfig, ctx context.Context) error {
// 	if ctrState.State != define.ContainerStateConfigured {
// 		return fmt.Errorf("container %s must be in Configured state to have storage set up: %w", c.ID(), define.ErrCtrStateInvalid)
// 	}

// 	// Need both an image ID and image name, plus a bool telling us whether to use the image configuration
// 	if ctrConfig.Rootfs == "" && (ctrConfig.RootfsImageID == "" || ctrConfig.RootfsImageName == "") {
// 		return fmt.Errorf("must provide image ID and image name to use an image: %w", define.ErrInvalidArg)
// 	}
// 	options := storage.ContainerOptions{
// 		IDMappingOptions: storage.IDMappingOptions{
// 			HostUIDMapping: true,
// 			HostGIDMapping: true,
// 		},
// 		LabelOpts: ctrConfig.LabelOpts,
// 	}

// 	options.StorageOpt = ctrConfig.StorageOpts

// 	options.Volatile = ctrConfig.Volatile

// 	setupStorageMapping(&options.IDMappingOptions, &ctrConfig.IDMappings)

// 	// Unless the user has specified a name, use a randomly generated one.
// 	// Note that name conflicts may occur (see #11735), so we need to loop.
// 	generateName := ctrConfig.Name == ""
// 	var containerInfo ContainerInfo
// 	var containerInfoErr error
// 	for {
// 		if generateName {
// 			name, err := GenerateName()
// 			if err != nil {
// 				return err
// 			}
// 			ctrConfig.Name = name
// 		}
// 		containerInfo, containerInfoErr = CreateContainerStorage(ctx, c.runtime.imageContext, ctrConfig.RootfsImageName, ctrConfig.RootfsImageID, ctrConfig.Name, ctrConfig.ID, options)

// 		if !generateName || !errors.Is(containerInfoErr, storage.ErrDuplicateName) {
// 			break
// 		}
// 	}
// 	if containerInfoErr != nil {
// 		return fmt.Errorf("creating container storage: %w", containerInfoErr)
// 	}

// 	// Only reconfig IDMappings if layer was mounted from storage.
// 	// If it's an external overlay do not reset IDmappings.
// 	if !ctrConfig.RootfsOverlay {
// 		ctrConfig.IDMappings.UIDMap = containerInfo.UIDMap
// 		ctrConfig.IDMappings.GIDMap = containerInfo.GIDMap
// 	}

// 	processLabel, err := c.processLabel(containerInfo.ProcessLabel)
// 	if err != nil {
// 		return err
// 	}
// 	ctrConfig.ProcessLabel = processLabel
// 	ctrConfig.MountLabel = containerInfo.MountLabel
// 	ctrConfig.StaticDir = containerInfo.Dir
// 	ctrState.RunDir = containerInfo.RunDir

// 	if len(ctrConfig.IDMappings.UIDMap) != 0 || len(ctrConfig.IDMappings.GIDMap) != 0 {
// 		if err := os.Chown(containerInfo.RunDir, c.RootUID(), c.RootGID()); err != nil {
// 			return err
// 		}

// 		if err := os.Chown(containerInfo.Dir, c.RootUID(), c.RootGID()); err != nil {
// 			return err
// 		}
// 	}

// 	// Set the default Entrypoint and Command
// 	if containerInfo.Config != nil {
// 		// Set CMD in the container to the default configuration only if ENTRYPOINT is not set by the user.
// 		if ctrConfig.Entrypoint == nil && ctrConfig.Command == nil {
// 			ctrConfig.Command = containerInfo.Config.Config.Cmd
// 		}
// 		if ctrConfig.Entrypoint == nil {
// 			ctrConfig.Entrypoint = containerInfo.Config.Config.Entrypoint
// 		}
// 	}

// 	artifacts := filepath.Join(ctrConfig.StaticDir, artifactsDir)
// 	if err := os.MkdirAll(artifacts, 0755); err != nil {
// 		return fmt.Errorf("creating artifacts directory: %w", err)
// 	}

// 	return nil
// }

// func setupStorageMapping(dest, from *storage.IDMappingOptions, ctrConfig *types.ContainerConfig) {
// 	*dest = *from
// 	// If we are creating a container inside a pod, we always want to inherit the
// 	// userns settings from the infra container. So clear the auto userns settings
// 	// so that we don't request storage for a new uid/gid map.

// 	if ctrConfig.Spec.Linux != nil {
// 		dest.UIDMap = nil
// 		for _, r := range ctrConfig.Spec.Linux.UIDMappings {
// 			u := idtools.IDMap{
// 				ContainerID: int(r.ContainerID),
// 				HostID:      int(r.HostID),
// 				Size:        int(r.Size),
// 			}
// 			dest.UIDMap = append(dest.UIDMap, u)
// 		}
// 		dest.GIDMap = nil
// 		for _, r := range ctrConfig.Spec.Linux.GIDMappings {
// 			g := idtools.IDMap{
// 				ContainerID: int(r.ContainerID),
// 				HostID:      int(r.HostID),
// 				Size:        int(r.Size),
// 			}
// 			dest.GIDMap = append(dest.GIDMap, g)
// 		}
// 		dest.HostUIDMapping = false
// 		dest.HostGIDMapping = false
// 	}
// }

// func GenerateName() (string, error) {
// 	for {
// 		name := namesgenerator.GetRandomName(0)
// 		// Make sure container with this name does not exist
// 		// if _, err := r.state.LookupContainer(name); err == nil {
// 		// 	continue
// 		// } else if !errors.Is(err, define.ErrNoSuchCtr) {
// 		// 	return "", err
// 		// }
// 		// // Make sure pod with this name does not exist
// 		// if _, err := r.state.LookupPod(name); err == nil {
// 		// 	continue
// 		// } else if !errors.Is(err, define.ErrNoSuchPod) {
// 		// 	return "", err
// 		// }
// 		return name, nil
// 	}
// 	// The code should never reach here.
// }

// func CreateContainerStorage(ctx context.Context, systemContext *types.SystemContext, imageName, imageID, containerName, containerID string, options storage.ContainerOptions) (_ ContainerInfo, retErr error) {
// 	var imageConfig *v1.Image

// 	// Build metadata to store with the container.
// 	metadata := RuntimeContainerMetadata{
// 		ImageName:     imageName,
// 		ImageID:       imageID,
// 		ContainerName: containerName,
// 		CreatedAt:     time.Now().Unix(),
// 	}
// 	mdata, err := json.Marshal(&metadata)
// 	if err != nil {
// 		return ContainerInfo{}, err
// 	}

// 	// Build the container.
// 	names := []string{containerName}

// 	container, err := CreateContainer(containerID, names, imageID, "", string(mdata), &options)
// 	if err != nil {
// 		logrus.Debugf("Failed to create container %s(%s): %v", metadata.ContainerName, containerID, err)

// 		return ContainerInfo{}, err
// 	}
// 	logrus.Debugf("Created container %q", container.ID)

// 	// If anything fails after this point, we need to delete the incomplete
// 	// container before returning.
// 	defer func() {
// 		if retErr != nil {
// 			if err := r.store.DeleteContainer(container.ID); err != nil {
// 				logrus.Infof("Error deleting partially-created container %q: %v", container.ID, err)

// 				return
// 			}
// 			logrus.Infof("Deleted partially-created container %q", container.ID)
// 		}
// 	}()

// 	// Add a name to the container's layer so that it's easier to follow
// 	// what's going on if we're just looking at the storage-eye view of things.
// 	layerName := metadata.ContainerName + "-layer"
// 	names, err = r.store.Names(container.LayerID)
// 	if err != nil {
// 		return ContainerInfo{}, err
// 	}
// 	names = append(names, layerName)
// 	err = r.store.SetNames(container.LayerID, names)
// 	if err != nil {
// 		return ContainerInfo{}, err
// 	}

// 	// Find out where the container work directories are, so that we can return them.
// 	containerDir, err := r.store.ContainerDirectory(container.ID)
// 	if err != nil {
// 		return ContainerInfo{}, err
// 	}
// 	logrus.Debugf("Container %q has work directory %q", container.ID, containerDir)

// 	containerRunDir, err := r.store.ContainerRunDirectory(container.ID)
// 	if err != nil {
// 		return ContainerInfo{}, err
// 	}
// 	logrus.Debugf("Container %q has run directory %q", container.ID, containerRunDir)

// 	return ContainerInfo{
// 		UIDMap:       container.UIDMap,
// 		GIDMap:       container.GIDMap,
// 		Dir:          containerDir,
// 		RunDir:       containerRunDir,
// 		Config:       imageConfig,
// 		ProcessLabel: container.ProcessLabel(),
// 		MountLabel:   container.MountLabel(),
// 	}, nil
// }

// func CreateContainer(id string, names []string, image, layer, metadata string, cOptions *ContainerOptions) (*Container, error) {
// 	var options ContainerOptions
// 	if cOptions != nil {
// 		options = *cOptions
// 		options.IDMappingOptions.UIDMap = copyIDMap(cOptions.IDMappingOptions.UIDMap)
// 		options.IDMappingOptions.GIDMap = copyIDMap(cOptions.IDMappingOptions.GIDMap)
// 		options.LabelOpts = copyStringSlice(cOptions.LabelOpts)
// 		options.Flags = copyStringInterfaceMap(cOptions.Flags)
// 		options.MountOpts = copyStringSlice(cOptions.MountOpts)
// 		options.StorageOpt = copyStringStringMap(cOptions.StorageOpt)
// 		options.BigData = copyContainerBigDataOptionSlice(cOptions.BigData)
// 	}
// 	if options.HostUIDMapping {
// 		options.UIDMap = nil
// 	}
// 	if options.HostGIDMapping {
// 		options.GIDMap = nil
// 	}
// 	options.Metadata = metadata
// 	rlstore, lstores, err := s.bothLayerStoreKinds() // lstores will be locked read-only if image != ""
// 	if err != nil {
// 		return nil, err
// 	}

// 	var imageTopLayer *Layer
// 	imageID := ""

// 	if options.AutoUserNs || options.UIDMap != nil || options.GIDMap != nil {
// 		// Prevent multiple instances to retrieve the same range when AutoUserNs
// 		// are used.
// 		// It doesn't prevent containers that specify an explicit mapping to overlap
// 		// with AutoUserNs.
// 		s.usernsLock.Lock()
// 		defer s.usernsLock.Unlock()
// 	}

// 	if options.AutoUserNs {
// 		var err error
// 		options.UIDMap, options.GIDMap, err = s.getAutoUserNS(&options.AutoUserNsOpts, cimage, rlstore, lstores)
// 		if err != nil {
// 			return nil, err
// 		}
// 	}

// 	uidMap := options.UIDMap
// 	gidMap := options.GIDMap

// 	idMappingsOptions := options.IDMappingOptions

// 	layerOptions := &LayerOptions{
// 		// Normally layers for containers are volatile only if the container is.
// 		// But in transient store mode, all container layers are volatile.
// 		Volatile: options.Volatile || s.transientStore,
// 	}
// 	if s.canUseShifting(uidMap, gidMap) {
// 		layerOptions.IDMappingOptions = types.IDMappingOptions{
// 			HostUIDMapping: true,
// 			HostGIDMapping: true,
// 			UIDMap:         nil,
// 			GIDMap:         nil,
// 		}
// 	} else {
// 		layerOptions.IDMappingOptions = types.IDMappingOptions{
// 			HostUIDMapping: idMappingsOptions.HostUIDMapping,
// 			HostGIDMapping: idMappingsOptions.HostGIDMapping,
// 			UIDMap:         copyIDMap(uidMap),
// 			GIDMap:         copyIDMap(gidMap),
// 		}
// 	}
// 	if options.Flags == nil {
// 		options.Flags = make(map[string]interface{})
// 	}
// 	plabel, _ := options.Flags[processLabelFlag].(string)
// 	mlabel, _ := options.Flags[mountLabelFlag].(string)
// 	if (plabel == "" && mlabel != "") || (plabel != "" && mlabel == "") {
// 		return nil, errors.New("ProcessLabel and Mountlabel must either not be specified or both specified")
// 	}

// 	if plabel == "" {
// 		processLabel, mountLabel, err := label.InitLabels(options.LabelOpts)
// 		if err != nil {
// 			return nil, err
// 		}
// 		mlabel = mountLabel
// 		options.Flags[processLabelFlag] = processLabel
// 		options.Flags[mountLabelFlag] = mountLabel
// 	}

// 	clayer, _, err := rlstore.create(layer, imageTopLayer, nil, mlabel, options.StorageOpt, layerOptions, true, nil)
// 	if err != nil {
// 		return nil, err
// 	}
// 	layer = clayer.ID

// 	// Normally only `--rm` containers are volatile, but in transient store mode all containers are volatile
// 	if s.transientStore {
// 		options.Volatile = true
// 	}

//		return writeToContainerStore(s, func() (*Container, error) {
//			options.IDMappingOptions = types.IDMappingOptions{
//				HostUIDMapping: len(options.UIDMap) == 0,
//				HostGIDMapping: len(options.GIDMap) == 0,
//				UIDMap:         copyIDMap(options.UIDMap),
//				GIDMap:         copyIDMap(options.GIDMap),
//			}
//			container, err := s.containerStore.create(id, names, imageID, layer, &options)
//			if err != nil || container == nil {
//				if err2 := rlstore.Delete(layer); err2 != nil {
//					if err == nil {
//						err = fmt.Errorf("deleting layer %#v: %w", layer, err2)
//					} else {
//						logrus.Errorf("While recovering from a failure to create a container, error deleting layer %#v: %v", layer, err2)
//					}
//				}
//			}
//			return container, err
//		})
//	}
// func copyIDMap(idmap []idtools.IDMap) []idtools.IDMap {
// 	m := []idtools.IDMap{}
// 	if idmap != nil {
// 		m = make([]idtools.IDMap, len(idmap))
// 		copy(m, idmap)
// 	}
// 	if len(m) > 0 {
// 		return m[:]
// 	}
// 	return nil
// }
// func copyStringSlice(slice []string) []string {
// 	if len(slice) == 0 {
// 		return nil
// 	}
// 	ret := make([]string, len(slice))
// 	copy(ret, slice)
// 	return ret
// }

// func copyStringStringMap(m map[string]string) map[string]string {
// 	ret := make(map[string]string, len(m))
// 	for k, v := range m {
// 		ret[k] = v
// 	}
// 	return ret
// }

// func copyContainerBigDataOptionSlice(slice []ContainerBigDataOption) []ContainerBigDataOption {
// 	ret := make([]ContainerBigDataOption, len(slice))
// 	for i := range slice {
// 		ret[i].Key = slice[i].Key
// 		ret[i].Data = append([]byte{}, slice[i].Data...)
// 	}
// 	return ret
// }

// copyStringInterfaceMap still forces us to assume that the interface{} is
// a non-pointer scalar value
// func copyStringInterfaceMap(m map[string]interface{}) map[string]interface{} {
// 	ret := make(map[string]interface{}, len(m))
// 	for k, v := range m {
// 		ret[k] = v
// 	}
// 	return ret
// }

// type ContainerInfo struct {
// 	Dir          string
// 	RunDir       string
// 	Config       *v1.Image
// 	ProcessLabel string
// 	MountLabel   string
// 	UIDMap       []idtools.IDMap
// 	GIDMap       []idtools.IDMap
// }

// type RuntimeContainerMetadata struct {
// 	// The provided name and the ID of the image that was used to
// 	// instantiate the container.
// 	ImageName string `json:"image-name"` // Applicable to both PodSandboxes and Containers
// 	ImageID   string `json:"image-id"`   // Applicable to both PodSandboxes and Containers
// 	// The container's name, which for an infrastructure container is usually PodName + "-infra".
// 	ContainerName string `json:"name"`                 // Applicable to both PodSandboxes and Containers, mandatory
// 	CreatedAt     int64  `json:"created-at"`           // Applicable to both PodSandboxes and Containers
// 	MountLabel    string `json:"mountlabel,omitempty"` // Applicable to both PodSandboxes and Containers
// }

// type ContainerOptions struct {
// 	// IDMappingOptions specifies the type of ID mapping which should be
// 	// used for this container's layer.  If nothing is specified, the
// 	// container's layer will inherit settings from the image's top layer
// 	// or, if it is not being created based on an image, the Store object.
// 	types.IDMappingOptions
// 	LabelOpts []string
// 	// Flags is a set of named flags and their values to store with the container.
// 	// Currently these can only be set when the container record is created, but that
// 	// could change in the future.
// 	Flags      map[string]interface{}
// 	MountOpts  []string
// 	Volatile   bool
// 	StorageOpt map[string]string
// 	// Metadata is caller-specified metadata associated with the container.
// 	Metadata string
// 	// BigData is a set of items which should be stored for the container.
// 	BigData []ContainerBigDataOption
// }

// type ContainerBigDataOption struct {
// 	Key  string
// 	Data []byte
// }
