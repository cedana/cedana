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

func getOCICgroupPath(config *types.ContainerConfig) (string, error) {

	// When the OCI runtime is set to use Systemd as a cgroup manager, it
	// expects cgroups to be passed as follows:
	// slice:prefix:name
	systemdCgroups := fmt.Sprintf("%s:libpod:%s", path.Base(config.CgroupParent))
	logrus.Debugf("Setting Cgroups for container to %s", systemdCgroups)
	return systemdCgroups, nil
}
