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
)

type RemoteDB struct {
	baseUrl string
	token   string
	client  *http.Client
}

func NewRemoteDB(ctx context.Context, baseUrl string, token string) DB {
	return &RemoteDB{
		baseUrl: baseUrl,
		token:   token,
		client:  &http.Client{},
	}
}

/////////////
// Getters //
/////////////

func (db *RemoteDB) GetJob(ctx context.Context, jid string) (*daemon.Job, error) {
	url := fmt.Sprintf("%s/%s", db.baseUrl, jid)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", db.token))

	resp, err := db.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get job: %s", resp.Status)
	}

	// unmarsal the bytes into a Job struct
	job := daemon.Job{}
	err = json.NewDecoder(resp.Body).Decode(&job)
	if err != nil {
		return nil, err
	}

	return &job, nil
}

/////////////
// Setters //
/////////////

func (db *RemoteDB) PutJob(ctx context.Context, jid string, job *daemon.Job) error {
	url := fmt.Sprintf("%s/%s", db.baseUrl, jid)

	data := map[string]any{
		"jid":  jid,
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
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", db.token))

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

/////////////
// Listers //
/////////////

func (db *RemoteDB) ListJobs(ctx context.Context, jids ...string) ([]*daemon.Job, error) {
	url := fmt.Sprintf("%s", db.baseUrl)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", db.token))

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
	for _, jobRaw := range jobsRaw {
		job := daemon.Job{}
		if err := json.Unmarshal(jobRaw.State, &job); err != nil {
			return nil, err
		}
		jobs = append(jobs, &job)
	}

	return jobs, nil
}

//////////////
// Deleters //
//////////////

func (db *RemoteDB) DeleteJob(ctx context.Context, jid string) error {
	url := fmt.Sprintf("%s/%s", db.baseUrl, jid)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", db.token))
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
