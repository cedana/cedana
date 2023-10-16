package api

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/cedana/cedana/api/services/task"
	bolt "go.etcd.io/bbolt"
)

type DB struct {
}

func NewDB() (*bolt.DB, error) {
	// set up embedded key-value db
	conn, err := bolt.Open("/tmp/cedana.db", 0600, nil)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// KISS for now - but we may want to separate out into subbuckets as we add more
// checkpointing functionality (like incremental checkpointing or GPU checkpointing)
// structure is default -> xid, xid -> pid: state (arrows denote buckets)
func (db *DB) CreateOrUpdateCedanaProcess(id string, state *task.ProcessState) error {
	conn, err := NewDB()
	if err != nil {
		return err
	}

	defer conn.Close()

	return conn.Update(func(tx *bolt.Tx) error {
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

	conn, err := NewDB()
	if err != nil {
		return nil, err
	}

	defer conn.Close()

	err = conn.View(func(tx *bolt.Tx) error {
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

func (db *DB) GetStateFromPID(pid int32) (*task.ProcessState, error) {
	var state task.ProcessState

	conn, err := NewDB()
	if err != nil {
		return nil, err
	}

	defer conn.Close()

	err = conn.View(func(tx *bolt.Tx) error {
		root := tx.Bucket([]byte("default"))
		if root == nil {
			return fmt.Errorf("could not find bucket")
		}

		root.ForEachBucket(func(k []byte) error {
			job := root.Bucket(k)
			job.ForEach(func(k, v []byte) error {
				if string(k) == strconv.Itoa(int(pid)) {
					return json.Unmarshal(v, &state)
				}
				return nil
			})
			return nil
		})
		return nil
	})

	return &state, err
}

func (db *DB) UpdateProcessStateWithID(id string, state *task.ProcessState) error {
	conn, err := NewDB()
	if err != nil {
		return err
	}

	defer conn.Close()

	return conn.Update(func(tx *bolt.Tx) error {
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
	conn, err := NewDB()
	if err != nil {
		return err
	}

	defer conn.Close()

	return conn.Update(func(tx *bolt.Tx) error {
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

func (db *DB) GetPID(id string) (int32, error) {
	var pid int32

	conn, err := NewDB()
	if err != nil {
		return 0, err
	}

	defer conn.Close()

	err = conn.View(func(tx *bolt.Tx) error {
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
