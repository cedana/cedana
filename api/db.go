package api

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strconv"

	cedana "github.com/cedana/cedana/types"
	bolt "go.etcd.io/bbolt"
)

type DB struct {
	conn *bolt.DB
}

func NewDB(db *bolt.DB) *DB {
	return &DB{conn: db}
}

func (db *DB) Close() error {
	return db.conn.Close()
}

// KISS for now - but we may want to separate out into subbuckets as we add more
// checkpointing functionality (like incremental checkpointing or GPU checkpointing)
// structure is xid: pid, pid: state
func (db *DB) CreateOrUpdateCedanaProcess(id string, state *cedana.ProcessState) error {
	return db.conn.Update(func(tx *bolt.Tx) error {
		root, err := tx.CreateBucketIfNotExists([]byte("default"))
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

		err = root.Put([]byte(id), []byte(strconv.Itoa(int(pid))))
		if err != nil {
			return err
		}

		err = root.Put([]byte(strconv.Itoa(int(pid))), marshaledState)
		if err != nil {
			return err
		}

		return nil
	})
}

func (db *DB) GetStateFromID(id string) (*cedana.ProcessState, error) {
	var state cedana.ProcessState

	err := db.conn.View(func(tx *bolt.Tx) error {
		root := tx.Bucket([]byte("default"))
		if root == nil {
			return fmt.Errorf("could not find bucket")
		}

		pid := root.Get([]byte(id))
		if pid == nil {
			return fmt.Errorf("could not find pid")
		}

		marshaledState := root.Get(pid)
		if marshaledState == nil {
			return fmt.Errorf("could not find state")
		}

		return json.Unmarshal(marshaledState, &state)
	})

	return &state, err
}

func (db *DB) UpdateProcessStateWithID(id string, state *cedana.ProcessState) error {
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

func (db *DB) UpdateProcessStateWithPID(pid int32, state *cedana.ProcessState) error {
	return db.conn.Update(func(tx *bolt.Tx) error {
		root, err := tx.CreateBucketIfNotExists([]byte("default"))
		if err != nil {
			return err
		}

		marshaledState, err := json.Marshal(state)
		if err != nil {
			return err
		}

		return root.Put([]byte(strconv.Itoa(int(pid))), marshaledState)
	})
}

func (db *DB) GetPID(id string) (int32, error) {
	var pid int32
	err := db.conn.View(func(tx *bolt.Tx) error {
		root := tx.Bucket([]byte("default"))
		if root == nil {
			return fmt.Errorf("could not find bucket")
		}

		pidBytes := root.Get([]byte(id))
		if pidBytes == nil {
			return fmt.Errorf("could not find pid")
		}
		pid = int32(binary.BigEndian.Uint32(pidBytes))

		return nil
	})
	return pid, err
}
