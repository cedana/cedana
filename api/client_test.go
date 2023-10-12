package api

import (
	"testing"

	bolt "go.etcd.io/bbolt"
)

func Test_Dump(t *testing.T) {

}

func TestClient_TryStartJob(t *testing.T) {
	t.Run("TaskFailsOnce", func(t *testing.T) {

		// start a server
		srv, err := StartGRPCServer()

		if err != nil {
			t.Error(err)
		}

		mockDB, err := bolt.Open("test.db", 0600, nil)
		if err != nil {
			t.Error(err)
		}
		defer mockDB.Close()

		srv.GracefulStop()
	})
}
