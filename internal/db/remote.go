package db

// Remote implementation of the DB, that talks to the propagator.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/db/sql"
	"github.com/cedana/cedana/pkg/config"
)

type RemoteDB struct {
	baseUrl   string
	authToken string
	client    *http.Client

	UnimplementedDB
}

func NewRemoteDB(ctx context.Context, connection config.Connection) *RemoteDB {
	return &RemoteDB{
		baseUrl:   connection.URL,
		authToken: connection.AuthToken,
		client:    &http.Client{},
	}
}

///////////
/// Job ///
///////////

func (db *RemoteDB) PutJob(ctx context.Context, job *daemon.Job) error {
	url := fmt.Sprintf("%s/%s", db.baseUrl, job.JID)

	data := map[string]any{
		"jid":  job.JID,
		"data": job,
	}
	body, err := json.Marshal(data)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", db.authToken))

	resp, err := db.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("failed to put job: %s", resp.Status)
	}

	return nil
}

func (db *RemoteDB) ListJobs(ctx context.Context, jids ...string) ([]*daemon.Job, error) {
	url := fmt.Sprintf("%s", db.baseUrl)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", db.authToken))

	resp, err := db.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list jobs: %s", resp.Status)
	}

	var jobs []*daemon.Job
	var jobsRaw []sql.Job
	if err := json.NewDecoder(resp.Body).Decode(&jobsRaw); err != nil {
		return nil, err
	}
	// for _, jobRaw := range jobsRaw {
	// 	job := daemon.Job{}
	// 	if err := json.Unmarshal(jobRaw.State, &job); err != nil {
	// 		return nil, err
	// 	}
	// 	jobs = append(jobs, &job)
	// }

	return jobs, nil
}

func (db *RemoteDB) ListJobsByHostIDs(ctx context.Context, hostIDs ...string) ([]*daemon.Job, error) {
	url := fmt.Sprintf("%s", db.baseUrl)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", db.authToken))

	resp, err := db.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list jobs: %s", resp.Status)
	}

	var jobs []*daemon.Job
	var jobsRaw []sql.Job
	if err := json.NewDecoder(resp.Body).Decode(&jobsRaw); err != nil {
		return nil, err
	}
	// for _, jobRaw := range jobsRaw {
	// 	job := daemon.Job{}
	// 	if err := json.Unmarshal(jobRaw.State, &job); err != nil {
	// 		return nil, err
	// 	}
	// 	jobs = append(jobs, &job)
	// }

	return jobs, nil
}

func (db *RemoteDB) DeleteJob(ctx context.Context, jid string) error {
	url := fmt.Sprintf("%s/%s", db.baseUrl, jid)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", db.authToken))
	resp, err := db.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to delete job: %s", resp.Status)
	}

	return nil
}

//////////////////
/// Checkpoint ///
//////////////////

func (db *RemoteDB) PutCheckpoint(ctx context.Context, checkpoint *daemon.Checkpoint) error {
	url := fmt.Sprintf("%s/checkpoints", db.baseUrl)

	data := map[string]any{
		"checkpoint": checkpoint,
	}
	body, err := json.Marshal(data)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", db.authToken))

	resp, err := db.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("failed to create checkpoint: %s", resp.Status)
	}

	return nil
}

func (db *RemoteDB) ListCheckpoints(ctx context.Context, jids ...string) ([]*daemon.Checkpoint, error) {
	url := fmt.Sprintf("%s/checkpoints", db.baseUrl)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", db.authToken))

	resp, err := db.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list checkpoints: %s", resp.Status)
	}

	var checkpoints []*daemon.Checkpoint
	if err := json.NewDecoder(resp.Body).Decode(&checkpoints); err != nil {
		return nil, err
	}

	return checkpoints, nil
}

func (db *RemoteDB) ListCheckpointsByJIDs(ctx context.Context, jids ...string) ([]*daemon.Checkpoint, error) {
	url := fmt.Sprintf("%s/checkpoints", db.baseUrl)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", db.authToken))

	resp, err := db.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list checkpoints: %s", resp.Status)
	}

	var checkpoints []*daemon.Checkpoint
	if err := json.NewDecoder(resp.Body).Decode(&checkpoints); err != nil {
		return nil, err
	}

	return checkpoints, nil
}

func (db *RemoteDB) DeleteCheckpoint(ctx context.Context, id string) error {
	url := fmt.Sprintf("%s/checkpoints/%s", db.baseUrl, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", db.authToken))
	resp, err := db.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to delete checkpoint: %s", resp.Status)
	}

	return nil
}
