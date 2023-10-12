package api

import (
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	"github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/types"
	bolt "go.etcd.io/bbolt"
)

type DB struct {
	conn   *bolt.DB
	dbLock sync.Mutex
	dbPath string
}

func NewDB(db *bolt.DB) *DB {
	return &DB{conn: db}
}

func (db *DB) Close() error {
	return db.conn.Close()
}

// KISS for now - but we may want to separate out into subbuckets as we add more
// checkpointing functionality (like incremental checkpointing or GPU checkpointing)
// structure is default -> xid, xid -> pid, pid: state (arrows denote buckets)
func (db *DB) CreateOrUpdateCedanaProcess(id string, state *task.ProcessState) error {
	return db.conn.Update(func(tx *bolt.Tx) error {
		root, err := tx.CreateBucketIfNotExists([]byte("default"))
		if err != nil {
			return err
		}

		job, err := root.CreateBucketIfNotExists([]byte(id))
		if err != nil {
			return err
		}

		marshaledState, err := json.Marshal(state)
		if err != nil {
			return err
		}

		pid := state.PID
		if pid == 0 {
			return fmt.Errorf("pid 0 returned from state - is process running?")
		}

		err = job.Put([]byte(strconv.Itoa(int(pid))), marshaledState)
		if err != nil {
			return err
		}

		return nil
	})
}

// This automatically gets the latest entry in the job bucket
func (db *DB) GetStateFromID(id string) (*task.ProcessState, error) {
	var state task.ProcessState

	err := db.conn.View(func(tx *bolt.Tx) error {
		root := tx.Bucket([]byte("default"))
		if root == nil {
			return fmt.Errorf("could not find bucket")
		}

		job := root.Bucket([]byte(id))
		if job == nil {
			return fmt.Errorf("could not find job")
		}

		c := job.Cursor()
		_, marshaledState := c.Last()
		return json.Unmarshal(marshaledState, &state)
	})

	return &state, err
}

func (db *DB) UpdateProcessStateWithID(id string, state *task.ProcessState) error {
	return db.conn.Update(func(tx *bolt.Tx) error {
		root, err := tx.CreateBucketIfNotExists([]byte("default"))
		if err != nil {
			return err
		}

		marshaledState, err := json.Marshal(state)
		if err != nil {
			return err
		}

		pid := root.Get([]byte(id))
		if pid == nil {
			return fmt.Errorf("could not find pid")
		}

		return root.Put(pid, marshaledState)
	})
}

func (db *DB) UpdateProcessStateWithPID(pid int32, state *task.ProcessState) error {
	return db.conn.Update(func(tx *bolt.Tx) error {
		root := tx.Bucket([]byte("default"))
		if root == nil {
			return fmt.Errorf("could not find bucket")
		}

		root.ForEachBucket(func(k []byte) error {
			job := root.Bucket(k)
			if job == nil {
				return fmt.Errorf("could not find job")
			}
			job.ForEach(func(k, v []byte) error {
				if string(k) == strconv.Itoa(int(pid)) {
					marshaledState, err := json.Marshal(state)
					if err != nil {
						return err
					}
					return job.Put(k, marshaledState)
				}
				return nil
			})
			return nil
		})
		return nil
	})
}

func (db *DB) setNewDbConn() error {
	// We need an in-memory lock to avoid issues around POSIX file advisory
	// locks as described in the link below:
	// https://www.sqlite.org/src/artifact/c230a7a24?ln=994-1081
	db.dbLock.Lock()

	database, err := bolt.Open(db.dbPath, 0600, nil)
	if err != nil {
		return fmt.Errorf("opening database %s: %w", db.dbPath, err)
	}

	db.conn = database

	return nil
}

func (s *DB) getContainerConfigFromDB(id []byte, config *types.ContainerConfig, ctrsBkt *bolt.Bucket) error {
	ctrBkt := ctrsBkt.Bucket(id)
	if ctrBkt == nil {
		return fmt.Errorf("container %s not found in DB", string(id))
	}

	configBytes := ctrBkt.Get(configKey)
	if configBytes == nil {
		return fmt.Errorf("container %s missing config key in DB", string(id))
	}

	if err := json.Unmarshal(configBytes, config); err != nil {
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

func (s *DB) getContainerStateDB(id []byte, state *types.ContainerState, ctrsBkt *bolt.Bucket) error {
	newState := new(types.ContainerState)
	ctrToUpdate := ctrsBkt.Bucket(id)

	newStateBytes := ctrToUpdate.Get(stateKey)
	if newStateBytes == nil {
		return fmt.Errorf("container does not have a state key in DB")
	}

	if err := json.Unmarshal(newStateBytes, newState); err != nil {
		return fmt.Errorf("unmarshalling container state: %w", err)
	}

	// backwards compat, previously we used an extra bucket for the netns so try to get it from there
	netNSBytes := ctrToUpdate.Get(netNSKey)
	if netNSBytes != nil && newState.NetNS == "" {
		newState.NetNS = string(netNSBytes)
	}

	state = newState

	return nil
}

func (db *DB) GetPID(id string) (int32, error) {
	var pid int32
	err := db.conn.View(func(tx *bolt.Tx) error {
		root := tx.Bucket([]byte("default"))
		if root == nil {
			return fmt.Errorf("could not find bucket")
		}

		job := root.Bucket([]byte(id))
		if job == nil {
			return fmt.Errorf("could not find job")
		}

		c := job.Cursor()
		pidBytes, _ := c.Last()
		if pidBytes == nil {
			return fmt.Errorf("could not find pid")
		}

		pid64, err := strconv.ParseInt(string(pidBytes), 10, 32)
		if err != nil {
			return err
		}

		pid = int32(pid64)

		return err
	})
	return pid, err
}

func (db *DB) ReturnAllEntries() ([]map[string]string, error) {
	var out []map[string]string
	err := db.conn.View(func(tx *bolt.Tx) error {
		root := tx.Bucket([]byte("default"))
		if root == nil {
			return fmt.Errorf("could not find bucket")
		}

		root.ForEach(func(k, v []byte) error {
			out = append(out, map[string]string{
				string(k): string(v),
			})
			return nil
		})
		return nil
	})
	return out, err
}
