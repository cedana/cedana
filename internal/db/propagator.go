package db

// Remote implementation of the DB, that uses the propagator service as a backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
)

type PropagatorDB struct {
	baseUrl   string
	authToken string
	client    *http.Client
}

func NewPropagatorDB(ctx context.Context, connection config.Connection) *PropagatorDB {
	return &PropagatorDB{
		baseUrl:   connection.URL,
		authToken: connection.AuthToken,
		client:    &http.Client{},
	}
}

///////////
/// Job ///
///////////

func (db *PropagatorDB) PutJob(ctx context.Context, job *daemon.Job) error {
	url := fmt.Sprintf("%s/jobs/%s", db.baseUrl, job.JID)

	body, err := json.Marshal(job)
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

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to put job: %s", resp.Status)
	}

	return nil
}

func (db *PropagatorDB) ListJobs(ctx context.Context, jids ...string) ([]*daemon.Job, error) {
	url := fmt.Sprintf("%s/jobs", db.baseUrl)
	if len(jids) > 0 {
		url += fmt.Sprintf("?jids=%s", strings.Join(jids, ","))
	}

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

	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
		return nil, err
	}

	return jobs, nil
}

func (db *PropagatorDB) ListJobsByHostIDs(ctx context.Context, hostIDs ...string) ([]*daemon.Job, error) {
	url := fmt.Sprintf("%s/jobs", db.baseUrl)
	if len(hostIDs) > 0 {
		url += fmt.Sprintf("?host_ids=%s", strings.Join(hostIDs, ","))
	}

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
	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
		return nil, err
	}

	return jobs, nil
}

func (db *PropagatorDB) DeleteJob(ctx context.Context, jid string) error {
	url := fmt.Sprintf("%s/jobs/%s", db.baseUrl, jid)
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

func (db *PropagatorDB) PutCheckpoint(ctx context.Context, checkpoint *daemon.Checkpoint) error {
	url := fmt.Sprintf("%s/checkpoints/%s", db.baseUrl, checkpoint.ID)

	body, err := json.Marshal(checkpoint)
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

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to create checkpoint: %s", resp.Status)
	}

	return nil
}

func (db *PropagatorDB) ListCheckpoints(ctx context.Context, ids ...string) ([]*daemon.Checkpoint, error) {
	url := fmt.Sprintf("%s/checkpoints", db.baseUrl)
	if len(ids) > 0 {
		url += fmt.Sprintf("?ids=%s", strings.Join(ids, ","))
	}

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

func (db *PropagatorDB) ListCheckpointsByJIDs(ctx context.Context, jids ...string) ([]*daemon.Checkpoint, error) {
	url := fmt.Sprintf("%s/checkpoints", db.baseUrl)
	if len(jids) > 0 {
		url += fmt.Sprintf("?jids=%s", strings.Join(jids, ","))
	}

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

func (db *PropagatorDB) DeleteCheckpoint(ctx context.Context, id string) error {
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

/////////////
/// Hosts ///
/////////////

func (db *PropagatorDB) PutHost(ctx context.Context, host *daemon.Host) error {
	url := fmt.Sprintf("%s/hosts/%s", db.baseUrl, host.ID)

	body, err := json.Marshal(host)
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

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to put host: %s", resp.Status)
	}

	return nil
}

func (db *PropagatorDB) ListHosts(ctx context.Context, ids ...string) ([]*daemon.Host, error) {
	url := fmt.Sprintf("%s/hosts", db.baseUrl)
	if len(ids) > 0 {
		url += fmt.Sprintf("?ids=%s", strings.Join(ids, ","))
	}

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
		return nil, fmt.Errorf("failed to list hosts: %s", resp.Status)
	}

	var hosts []*daemon.Host
	if err := json.NewDecoder(resp.Body).Decode(&hosts); err != nil {
		return nil, err
	}

	return hosts, nil
}

func (db *PropagatorDB) DeleteHost(ctx context.Context, id string) error {
	url := fmt.Sprintf("%s/hosts/%s", db.baseUrl, id)
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
		return fmt.Errorf("failed to delete host: %s", resp.Status)
	}

	return nil
}
