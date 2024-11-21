package db

// Remote implementation of the DB, that talks to the propagator.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cedana/cedana/pkg/db/models"
	"github.com/spf13/viper"
)

type RemoteDB struct {
	baseUrl string
	client  *http.Client
}

func NewRemoteDB(ctx context.Context, baseUrl string) DB {
	return &RemoteDB{
		baseUrl: baseUrl,
		client:  &http.Client{},
	}
}

/////////////
// Getters //
/////////////

func (db *RemoteDB) GetJob(ctx context.Context, jid []byte) (models.Job, error) {
	url := fmt.Sprintf("%s/%s", db.baseUrl, jid)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return models.Job{}, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", viper.GetString("connection.cedana_auth_token")))

	resp, err := db.client.Do(req)
	if err != nil {
		return models.Job{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return models.Job{}, fmt.Errorf("failed to get job: %s", resp.Status)
	}

	var job models.Job
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return models.Job{}, err
	}

	return job, nil
}

/////////////
// Setters //
/////////////

func (db *RemoteDB) PutJob(ctx context.Context, jid []byte, state []byte) error {
	url := fmt.Sprintf("%s/%s", db.baseUrl, jid)

	jobData := map[string][]byte{
		"jid":   jid,
		"state": state,
	}
	body, err := json.Marshal(jobData)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", viper.GetString("connection.cedana_auth_token")))

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

func (db *RemoteDB) ListJobs(ctx context.Context) ([]models.Job, error) {
	url := fmt.Sprintf("%s", db.baseUrl)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", viper.GetString("connection.cedana_auth_token")))

	resp, err := db.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list jobs: %s", resp.Status)
	}

	var jobs []models.Job
	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
		return nil, err
	}

	return jobs, nil
}
