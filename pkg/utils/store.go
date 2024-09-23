package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/cedana/cedana/pkg/api/services/task"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Abstraction for storing and retrieving checkpoints
type Store interface {
	GetCheckpoint(ctx context.Context, cid string) (*string, error) // returns filepath to downloaded chekcpoint
	PushCheckpoint(ctx context.Context, filepath string) error
	ListCheckpoints(ctx context.Context) (*[]CheckpointMeta, error) // fix
}

type CheckpointMeta struct {
	ID       string
	Name     string
	Bucket   string
	ModTime  time.Time
	Size     uint64
	Checksum string
}

type S3Store struct{}

func (s *S3Store) GetCheckpoint() (*string, error) {
	return nil, nil
}

func (s *S3Store) PushCheckpoint(filepath string) error {
	return nil
}

type UploadResponse struct {
	UploadID  string `json:"upload_id"`
	PartSize  int64  `json:"part_size"`
	PartCount int64  `json:"part_count"`
}

// For pushing and pulling from a cedana managed endpoint
type CedanaStore struct {
	url string
}

func NewCedanaStore() *CedanaStore {
	url := "https://" + viper.GetString("connection.cedana_url")
	return &CedanaStore{
		url: url,
	}
}

// TODO NR - unimplemented stubs for now
func (cs *CedanaStore) ListCheckpoints(ctx context.Context) (*[]CheckpointMeta, error) {
	return nil, nil
}

func (cs *CedanaStore) FullMultipartUpload(ctx context.Context, checkpointPath string) (*UploadResponse, error) {
	file, err := os.Open(checkpointPath)
	if err != nil {
		err := status.Error(codes.Unavailable, "StartMultiPartUpload failed")
		return &UploadResponse{}, err
	}
	defer file.Close()

	// Get the file size
	fileInfo, err := file.Stat()
	if err != nil {
		err = status.Error(codes.NotFound, "checkpoint zip not found")
		return &UploadResponse{}, err
	}

	// Get the size
	size := fileInfo.Size()

	checkpointFullSize := int64(size)

	multipartCheckpointResp, cid, err := cs.CreateMultiPartUpload(ctx, checkpointFullSize)
	if err != nil {
		err := status.Error(codes.Unavailable, "CreateMultiPartUpload failed")
		return &UploadResponse{}, err
	}

	err = cs.StartMultiPartUpload(ctx, cid, multipartCheckpointResp, checkpointPath)
	if err != nil {
		err := status.Error(codes.Unavailable, "StartMultiPartUpload failed")
		return &UploadResponse{}, err
	}

	err = cs.CompleteMultiPartUpload(ctx, *multipartCheckpointResp, cid)
	if err != nil {
		err := status.Error(codes.Unavailable, "CompleteMultiPartUpload failed")
		return &UploadResponse{}, err
	}
	return multipartCheckpointResp, nil
}

func (cs *CedanaStore) GetCheckpoint(ctx context.Context, cid string) (*string, error) {
	url := cs.url + "/checkpoint/" + cid
	downloadPath := "checkpoint.tar"
	file, err := os.Create(downloadPath)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	httpClient := &http.Client{}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", viper.GetString("connection.cedana_auth_token")))

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("unexpected status code: %v", resp.Status)
	}

	defer resp.Body.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return nil, err
	}

	return &downloadPath, nil
}

func (cs *CedanaStore) PushCheckpoint(ctx context.Context, filepath string) error {
	return nil
}

func (cs *CedanaStore) CreateMultiPartUpload(ctx context.Context, fullSize int64) (*UploadResponse, string, error) {
	var uploadResp UploadResponse

	cid := uuid.New().String()

	data := struct {
		Name     string `json:"name"`
		FullSize int64  `json:"full_size"`
		PartSize int    `json:"part_size"`
	}{
		// TODO BS Need to get TaskID properly...
		Name:     "test",
		FullSize: fullSize,
		PartSize: 0,
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return &uploadResp, "", err
	}

	httpClient := &http.Client{}
	url := cs.url + "/checkpoint/" + cid + "/upload"

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payload))
	if err != nil {
		return &uploadResp, "", err
	}

	req.Header.Set("Content-Type", "application/json")

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", viper.GetString("connection.cedana_auth_token")))

	resp, err := httpClient.Do(req)
	if err != nil {
		return &uploadResp, "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &uploadResp, "", fmt.Errorf("unexpected status code: %v", resp.Status)
	}

	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &uploadResp, "", err
	}

	log.Info().Msgf("response body: %s", string(respBody))

	// Parse the JSON response into the struct
	if err := json.Unmarshal(respBody, &uploadResp); err != nil {
		fmt.Println("Error parsing JSON response:", err)
		return &uploadResp, "", err
	}

	return &uploadResp, cid, nil
}

func (cs *CedanaStore) StartMultiPartUpload(ctx context.Context, cid string, uploadResp *UploadResponse, checkpointPath string) error {
	binaryOfFile, err := os.ReadFile(checkpointPath)
	if err != nil {
		fmt.Println("Error reading zip file:", err)
		return err
	}

	maxConcurrentUploads := 10
	semaphore := make(chan struct{}, maxConcurrentUploads)

	chunkSize := uploadResp.PartSize
	numOfParts := uploadResp.PartCount

	errChan := make(chan error, numOfParts)
	var wg sync.WaitGroup

	for i := 0; i < int(numOfParts); i++ {
		wg.Add(1)

		semaphore <- struct{}{}

		go func(partNumber int) {
			defer wg.Done()
			defer func() { <-semaphore }()

			start := partNumber * int(chunkSize)
			end := (partNumber + 1) * int(chunkSize)
			if end > len(binaryOfFile) {
				end = len(binaryOfFile)
			}

			partData := binaryOfFile[start:end]
			buffer := bytes.NewBuffer(partData)

			httpClient := &http.Client{}
			url := cs.url + "/checkpoint/" + cid + "/upload/" + uploadResp.UploadID + "/part/" + fmt.Sprintf("%d", partNumber+1)

			req, err := http.NewRequest("PUT", url, buffer)
			if err != nil {
				errChan <- err
				return
			}

			req.Header.Set("Content-Type", "application/octet-stream")
			req.Header.Set("Transfer-Encoding", "chunked")
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", viper.GetString("connection.cedana_auth_token")))

			resp, err := httpClient.Do(req)
			if err != nil {
				errChan <- err
				return
			}
			if resp.StatusCode < 200 || resp.StatusCode > 299 {
				errChan <- fmt.Errorf("unexpected status code: %v", resp.Status)
				return
			}

			defer resp.Body.Close()

			respBody, err := io.ReadAll(resp.Body)
			if err != nil {
				errChan <- err
				return
			}
			log.Info().Msgf("Part %d uploaded: %s", partNumber+1, string(respBody))
		}(i)
	}

	wg.Wait()
	close(errChan)

	// check for errs
	// check for errors
	for err := range errChan {
		if err != nil {
			return err
		}
	}

	return nil
}

func (cs *CedanaStore) CompleteMultiPartUpload(ctx context.Context, uploadResp UploadResponse, cid string) error {
	httpClient := &http.Client{}
	url := cs.url + "/checkpoint/" + cid + "/upload/" + uploadResp.UploadID + "/complete"

	req, err := http.NewRequest("PUT", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", viper.GetString("connection.cedana_auth_token")))

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("unexpected status code: %v", resp.Status)
	}

	defer resp.Body.Close()
	return nil
}

type MockStore struct {
	fs *afero.Afero // we can use an in-memory store for testing
}

func (ms *MockStore) GetCheckpoint() (*string, error) {
	// gets a mock checkpoint from the local filesystem - useful for testing
	return nil, nil
}

func (ms *MockStore) PushCheckpoint(filepath string) error {
	// pushes a mock checkpoint to the local filesystem
	return nil
}

func (ms *MockStore) ListCheckpoints(ctx context.Context) (*[]CheckpointMeta, error) {
	return nil, nil
}

func UploadCheckpoint(ctx context.Context, path string, store *CedanaStore) (string, string, error) {
	start := time.Now()
	stats, ok := ctx.Value(DumpStatsKey).(*task.DumpStats)
	if !ok {
		return "", "", status.Error(codes.Internal, "failed to get dump stats")
	}

	file, err := os.Open(path)
	if err != nil {
		st := status.New(codes.NotFound, "checkpoint zip not found")
		st.WithDetails(&errdetails.ErrorInfo{
			Reason: err.Error(),
		})
		return "", "", st.Err()
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		st := status.New(codes.Internal, "checkpoint zip stat failed")
		st.WithDetails(&errdetails.ErrorInfo{
			Reason: err.Error(),
		})
		return "", "", st.Err()
	}

	// Get the size
	size := fileInfo.Size()

	checkpointFullSize := int64(size)

	multipartCheckpointResp, cid, err := store.CreateMultiPartUpload(ctx, checkpointFullSize)
	if err != nil {
		st := status.New(codes.Internal, fmt.Sprintf("CreateMultiPartUpload failed with error: %s", err.Error()))
		return "", "", st.Err()
	}

	err = store.StartMultiPartUpload(ctx, cid, multipartCheckpointResp, path)
	if err != nil {
		st := status.New(codes.Internal, fmt.Sprintf("StartMultiPartUpload failed with error: %s", err.Error()))
		return "", "", st.Err()
	}

	err = store.CompleteMultiPartUpload(ctx, *multipartCheckpointResp, cid)
	if err != nil {
		st := status.New(codes.Internal, fmt.Sprintf("CompleteMultiPartUpload failed with error: %s", err.Error()))
		return "", "", st.Err()
	}

	stats.UploadDuration = time.Since(start).Milliseconds()
	return cid, multipartCheckpointResp.UploadID, err
}
