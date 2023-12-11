package api

import (
	"context"
	"log"
	"net"
	"os"
	"testing"

	"github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/utils"
	bolt "go.etcd.io/bbolt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func setup(t *testing.T) (task.TaskServiceClient, error) {
	lis := bufconn.Listen(1024 * 1024)
	t.Cleanup(func() {
		lis.Close()
	})

	srv := grpc.NewServer()
	t.Cleanup(func() {
		srv.Stop()
	})

	mockDB, err := bolt.Open("test.db", 0600, nil)
	t.Cleanup(func() {
		mockDB.Close()
	})
	if err != nil {
		t.Error(err)
	}

	c, err := InstantiateClient()
	if err != nil {
		t.Fatal(err)
	}

	logger := utils.GetLogger()

	svc := service{Client: c, logger: &logger}
	task.RegisterTaskServiceServer(srv, &svc)

	go func() {
		if err := srv.Serve(lis); err != nil {
			log.Fatalf("srv.Serve %v", err)
		}
	}()

	dialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}
	conn, err := grpc.DialContext(context.Background(), "", grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials()))
	t.Cleanup(func() {
		conn.Close()
	})
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}

	client := task.NewTaskServiceClient(conn)
	return client, err
}

func Test_Dump(t *testing.T) {
}

func TestClient_RunTask(t *testing.T) {
	t.Run("TaskIsEmpty", func(t *testing.T) {
		client, err := setup(t)
		if err != nil {
			t.Error("error setting up grpc client")
		}

		ctx := context.Background()

		_, err = client.StartTask(ctx, &task.StartTaskArgs{Task: "", Id: ""})

		if err == nil {
			t.Error("expected error but got err == nil")
		}

	})
}

func TestClient_TryStartJob(t *testing.T) {
	// skip CI
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping test in CI")
	}
	t.Run("TaskFailsOnce", func(t *testing.T) {

		client, err := setup(t)
		if err != nil {
			t.Error("error setting up grpc client")
		}
		ctx := context.Background()

		// get uid and gid
		uid := uint32(os.Getuid())
		gid := uint32(os.Getgid())

		_, err = client.StartTask(ctx, &task.StartTaskArgs{Task: "test", Id: "test", LogOutputFile: "somefile", UID: uid, GID: gid})

		if err != nil {
			t.Errorf("failed to start task: %v", err)
		}

	})
}
