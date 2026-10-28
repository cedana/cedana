package db

// Remote implementation of the DB, that uses the propagator service as a backend

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	propagatorsdk "github.com/cedana/cedana-propagator-sdk/go"
	"github.com/cedana/cedana-propagator-sdk/go/models"
	v1 "github.com/cedana/cedana-propagator-sdk/go/v1"
	"github.com/cedana/cedana/pkg/config"
	"github.com/microsoft/kiota-abstractions-go/serialization"
)

type PropagatorDB struct {
	propagator *v1.V1RequestBuilder

	// Fallback DBs are used for all unimplemented methods
	fallback []DB
}

func NewPropagatorDB(ctx context.Context, connection config.Connection, fallback ...DB) *PropagatorDB {
	return &PropagatorDB{
		propagatorsdk.NewClient(connection.URL, connection.AuthToken).V1(),
		fallback,
	}
}

// toModel converts a JSON-serializable value into an SDK model, preserving the
// exact wire format the propagator expects.
func toModel[T serialization.Parsable](v any, factory serialization.ParsableFactory) (T, error) {
	var zero T
	data, err := json.Marshal(v)
	if err != nil {
		return zero, err
	}
	parsed, err := serialization.Deserialize("application/json", data, factory)
	if err != nil {
		return zero, err
	}
	model, ok := parsed.(T)
	if !ok {
		return zero, fmt.Errorf("unexpected model type %T", parsed)
	}
	return model, nil
}

// fromModel converts an SDK model back into a JSON-deserializable value.
func fromModel(model serialization.Parsable, out any) error {
	data, err := serialization.Serialize("application/json", model)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

///////////
/// Job ///
///////////

func (db *PropagatorDB) PutJob(ctx context.Context, job *daemon.Job) error {
	body, err := toModel[models.Jobable](job, models.CreateJobFromDiscriminatorValue)
	if err != nil {
		return err
	}

	if _, err := db.propagator.Cedana().Jobs().ByJid(job.JID).Put(ctx, body, nil); err != nil {
		return fmt.Errorf("failed to put job: %w", err)
	}

	return nil
}

func (db *PropagatorDB) ListJobs(ctx context.Context, jids ...string) ([]*daemon.Job, error) {
	var requestConfiguration *v1.CedanaJobsRequestBuilderGetRequestConfiguration
	if len(jids) > 0 {
		jidsParam := strings.Join(jids, ",")
		requestConfiguration = &v1.CedanaJobsRequestBuilderGetRequestConfiguration{
			QueryParameters: &v1.CedanaJobsRequestBuilderGetQueryParameters{Jids: &jidsParam},
		}
	}

	list, err := db.propagator.Cedana().Jobs().Get(ctx, requestConfiguration)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	jobs := make([]*daemon.Job, 0, len(list))
	for _, model := range list {
		job := &daemon.Job{}
		if err := fromModel(model, job); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

func (db *PropagatorDB) ListJobsByHostIDs(ctx context.Context, hostIDs ...string) ([]*daemon.Job, error) {
	var requestConfiguration *v1.CedanaJobsRequestBuilderGetRequestConfiguration
	if len(hostIDs) > 0 {
		hostIDsParam := strings.Join(hostIDs, ",")
		requestConfiguration = &v1.CedanaJobsRequestBuilderGetRequestConfiguration{
			QueryParameters: &v1.CedanaJobsRequestBuilderGetQueryParameters{Host_ids: &hostIDsParam},
		}
	}

	list, err := db.propagator.Cedana().Jobs().Get(ctx, requestConfiguration)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	jobs := make([]*daemon.Job, 0, len(list))
	for _, model := range list {
		job := &daemon.Job{}
		if err := fromModel(model, job); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

func (db *PropagatorDB) DeleteJob(ctx context.Context, jid string) error {
	if _, err := db.propagator.Cedana().Jobs().ByJid(jid).Delete(ctx, nil); err != nil {
		return fmt.Errorf("failed to delete job: %w", err)
	}

	return nil
}

//////////////////
/// Checkpoint ///
//////////////////

func (db *PropagatorDB) PutCheckpoint(ctx context.Context, checkpoint *daemon.Checkpoint) error {
	body, err := toModel[models.CedanaJobCheckpointable](checkpoint, models.CreateCedanaJobCheckpointFromDiscriminatorValue)
	if err != nil {
		return err
	}

	if _, err := db.propagator.Cedana().Job().Checkpoints().ById(checkpoint.JID).Put(ctx, body, nil); err != nil {
		return fmt.Errorf("failed to create checkpoint: %w", err)
	}

	return nil
}

func (db *PropagatorDB) ListCheckpoints(ctx context.Context, ids ...string) ([]*daemon.Checkpoint, error) {
	var requestConfiguration *v1.CedanaJobCheckpointsRequestBuilderGetRequestConfiguration
	if len(ids) > 0 {
		idsParam := strings.Join(ids, ",")
		requestConfiguration = &v1.CedanaJobCheckpointsRequestBuilderGetRequestConfiguration{
			QueryParameters: &v1.CedanaJobCheckpointsRequestBuilderGetQueryParameters{Ids: &idsParam},
		}
	}

	list, err := db.propagator.Cedana().Job().Checkpoints().Get(ctx, requestConfiguration)
	if err != nil {
		return nil, fmt.Errorf("failed to list checkpoints: %w", err)
	}

	checkpoints := make([]*daemon.Checkpoint, 0, len(list))
	for _, model := range list {
		checkpoint := &daemon.Checkpoint{}
		if err := fromModel(model, checkpoint); err != nil {
			return nil, err
		}
		checkpoints = append(checkpoints, checkpoint)
	}

	return checkpoints, nil
}

func (db *PropagatorDB) ListCheckpointsByJIDs(ctx context.Context, jids ...string) ([]*daemon.Checkpoint, error) {
	var requestConfiguration *v1.CedanaJobCheckpointsRequestBuilderGetRequestConfiguration
	if len(jids) > 0 {
		jidsParam := strings.Join(jids, ",")
		requestConfiguration = &v1.CedanaJobCheckpointsRequestBuilderGetRequestConfiguration{
			QueryParameters: &v1.CedanaJobCheckpointsRequestBuilderGetQueryParameters{Jids: &jidsParam},
		}
	}

	list, err := db.propagator.Cedana().Job().Checkpoints().Get(ctx, requestConfiguration)
	if err != nil {
		return nil, fmt.Errorf("failed to list checkpoints: %w", err)
	}

	checkpoints := make([]*daemon.Checkpoint, 0, len(list))
	for _, model := range list {
		checkpoint := &daemon.Checkpoint{}
		if err := fromModel(model, checkpoint); err != nil {
			return nil, err
		}
		checkpoints = append(checkpoints, checkpoint)
	}

	return checkpoints, nil
}

func (db *PropagatorDB) DeleteCheckpoint(ctx context.Context, id string) error {
	if _, err := db.propagator.Cedana().Job().Checkpoints().ById(id).Delete(ctx, nil); err != nil {
		return fmt.Errorf("failed to delete checkpoint: %w", err)
	}

	return nil
}

/////////////
/// Hosts ///
/////////////

func (db *PropagatorDB) PutHost(ctx context.Context, host *daemon.Host) error {
	body, err := toModel[models.Hostable](host, models.CreateHostFromDiscriminatorValue)
	if err != nil {
		return err
	}

	if _, err := db.propagator.Hosts().ById(host.ID).Put(ctx, body, nil); err != nil {
		return fmt.Errorf("failed to put host: %w", err)
	}

	return nil
}

func (db *PropagatorDB) ListHosts(ctx context.Context, ids ...string) ([]*daemon.Host, error) {
	var requestConfiguration *v1.HostsRequestBuilderGetRequestConfiguration
	if len(ids) > 0 {
		idsParam := strings.Join(ids, ",")
		requestConfiguration = &v1.HostsRequestBuilderGetRequestConfiguration{
			QueryParameters: &v1.HostsRequestBuilderGetQueryParameters{Ids: &idsParam},
		}
	}

	list, err := db.propagator.Hosts().Get(ctx, requestConfiguration)
	if err != nil {
		return nil, fmt.Errorf("failed to list hosts: %w", err)
	}

	hosts := make([]*daemon.Host, 0, len(list))
	for _, model := range list {
		host := &daemon.Host{}
		if err := fromModel(model, host); err != nil {
			return nil, err
		}
		hosts = append(hosts, host)
	}

	return hosts, nil
}

func (db *PropagatorDB) DeleteHost(ctx context.Context, id string) error {
	if _, err := db.propagator.Hosts().ById(id).Delete(ctx, nil); err != nil {
		return fmt.Errorf("failed to delete host: %w", err)
	}

	return nil
}
