package jobservice

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/cedana/cedana/pkg/api/containerd"
	"github.com/cedana/cedana/pkg/api/runc"
	"github.com/cedana/cedana/pkg/api/services/task"
	"github.com/cedana/cedana/pkg/jobservice/jobdb"

	"github.com/rs/zerolog/log"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaSetup string

func New() (*JobService, error) {
	// sqlite queue
	db, err := sql.Open("sqlite3", ":memory:?_journal=WAL&_timeout=5000&_fk=true")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if _, err := db.ExecContext(context.Background(), schemaSetup); err != nil {
		return nil, err
	}

	config, err := detectConfig()
	if err != nil {
		// couldn't figure out runtime
		return nil, err
	}

	js := &JobService{
		db:     jobdb.New(db),
		config: config,
	}
	return js, nil
}

type HighLevelRuntime string

const (
	ContainerdRuntime HighLevelRuntime = "containerd"
	CRIORuntime       HighLevelRuntime = "crio"
)

type LowLevelRuntime string

const (
	Runc LowLevelRuntime = "runc"
)

type ContainerConfig struct {
	HLRuntime  HighLevelRuntime
	HLSockAddr string
	LLRuntime  LowLevelRuntime
	LLRoot     string
}

func detectConfig() (ContainerConfig, error) {
	return ContainerConfig{
		HLRuntime: ContainerdRuntime,
	}, nil
}

type JobService struct {
	db     *jobdb.Queries
	config ContainerConfig
}

type CheckpointResult int

func (js *JobService) checkpoint(req string) (string, error) {
	log.Info().Msg("Starting Checkpoint")
	cj := &task.QueueJobCheckpointRequest{}
	err := json.Unmarshal([]byte(req), cj)
	if err != nil {
		return "", err
	}
	log.Info().Msgf("checkpointing (%s) %s %s", cj.PodName, cj.ContainerName, cj.ImageName)
	// check runtime
	switch js.config.HLRuntime {
	case "crio":
		res, err := js.crioCheckpoint(cj.ContainerName, cj.PodName, cj.ImageName, cj.Namespace)
		if err != nil {
			return "", err
		}
		return res, err
	case "containerd":
		res, err := js.containerdCheckpoint(cj.ContainerName, cj.PodName, cj.ImageName, cj.Namespace)
		if err != nil {
			return "", err
		}
		return res.ImageRef, err
	}
	return "", fmt.Errorf("failed to checkpoint, invalid runtime detected, restart the node")
}

func (jqs *JobService) restore(req string) (string, error) {
	log.Info().Msg("Starting Restore")
	cj := &task.QueueJobRestoreRequest{}
	err := json.Unmarshal([]byte(req), cj)
	if err != nil {
		return "", err
	}
	log.Info().Msgf("restoring (%s) %s %s\n", cj.PodName, cj.ContainerName, cj.SourceName)
	// check runtime
	switch jqs.config.HLRuntime {
	case "crio":
		jqs.crioRestore(cj.ContainerName, cj.PodName, cj.SourceName, cj.Namespace)
	case "containerd":
		jqs.containerdRestore(cj.ContainerName, cj.PodName, cj.SourceName, cj.Namespace)
	}
	return "", nil
}

func (jqs *JobService) containerdRestore(containerName, sandboxName, source, namespace string) {
	// TODO: implement this
}

func (jqs *JobService) crioRestore(containerName, sandboxName, source, namespace string) {
	// TODO: implement this
}

func (jqs *JobService) crioCheckpoint(containerName, sandboxName, imageName, namespace string) (string, error) {
	// TODO: implement this
	return "", fmt.Errorf("unimplemented")
}

func (jqs *JobService) containerdCheckpoint(containerName, sandboxName, imageName, namespace string) (*task.ContainerdRootfsDumpResp, error) {
	id, _, err := runc.GetContainerIdByName(containerName, sandboxName, jqs.config.LLRoot)
	if err != nil {
		return nil, err
	}
	return containerd.ContainerdRootfsDump(context.Background(), &task.ContainerdRootfsDumpArgs{
		ContainerID: id,
		ImageRef:    imageName,
		Address:     jqs.config.HLSockAddr,
		Namespace:   namespace,
	})
}

func (js *JobService) Start(ctx context.Context) error {
	log.Info().Msg("Started the Job Service")
	var wg sync.WaitGroup
	wg.Add(2)

	// checkpoints
	go func() {
		defer wg.Done()
		for {
			if ctx.Err() != nil {
				break
			}
			chks, err := js.db.ListCheckpoints(ctx)
			if err != nil || len(chks) == 0 {
				time.Sleep(16 * time.Millisecond)
				continue
			}
			for _, chk := range chks {
				var status int64 = 1
				ref, err := js.checkpoint(chk.Data)
				if err != nil {
					// if err != nil then we failed
					// status == 1 :: completed success
					// status == -1 :: completed failed
					status = -1
				}
				js.db.UpdateStatus(ctx, jobdb.UpdateStatusParams{
					Status: status,
					ID:     chk.ID,
				})
				err = js.notifyJobQueue(chk.ID, ref, err != nil)
				if err != nil {
					log.Error().Err(err).Msg("failed to notify the job scheduler")
				}
			}
		}
	}()

	// restores
	go func() {
		defer wg.Done()
		for {
			if ctx.Err() != nil {
				break
			}
			chks, err := js.db.ListRestores(ctx)
			if err != nil || len(chks) == 0 {
				time.Sleep(16 * time.Millisecond)
				continue
			}
			for _, chk := range chks {
				var status int64 = 1
				ref, err := js.restore(chk.Data)
				if err != nil {
					// if err != nil then we failed
					// status == 1 :: completed success
					// status == -1 :: completed failed
					status = -1
				}
				js.db.UpdateStatus(ctx, jobdb.UpdateStatusParams{
					Status: status,
					ID:     chk.ID,
				})
				err = js.notifyJobQueue(chk.ID, ref, err != nil)
				if err != nil {
					log.Error().Err(err).Msg("failed to notify the job scheduler")
				}
			}
		}
	}()
	wg.Wait()

	return nil
}

func (js *JobService) GetJobQueueUrl() string {
	return ""
}

func (js *JobService) notifyJobQueue(id, ref string, failed bool) error {
	log.Info().Msgf("Notifing the jobqueue scheduler: (failed: %v)", failed)
	type JobQueueCallback struct {
		Id     string `json:"id"`
		Ref    string `json:"ref"`
		Failed bool   `json:"failed"`
	}
	b, err := json.Marshal(JobQueueCallback{
		Id:     id,
		Ref:    ref,
		Failed: failed,
	})
	if err != nil {
		return err
	}
	res, err := http.Post(js.GetJobQueueUrl(), "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	if res.StatusCode == 200 {
		return nil
	} else {
		return fmt.Errorf("failed to notify jobqueue")
	}
}

func (js *JobService) Restore(c *task.QueueJobRestoreRequest) error {
	log.Info().Msg("Restore job enqueue")
	b, err := json.Marshal(c)
	data := string(b)
	if err != nil {
		return err
	}
	js.db.AddJob(
		context.Background(),
		jobdb.AddJobParams{
			ID:     c.Id,
			Type:   1,
			Status: 0,
			Data:   data,
		},
	)
	return nil
}

func (js *JobService) Checkpoint(c *task.QueueJobCheckpointRequest) error {
	log.Info().Msg("Checkpoint job enqueue")
	b, err := json.Marshal(c)
	data := string(b)
	if err != nil {
		return err
	}
	js.db.AddJob(
		context.Background(),
		jobdb.AddJobParams{
			ID:     c.Id,
			Type:   0,
			Status: 0,
			Data:   data,
		},
	)
	return nil
}
