package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/container"
	"github.com/cedana/cedana/types"
	"github.com/cedana/cedana/utils"
	"github.com/checkpoint-restore/go-criu/v6/rpc"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	bolt "go.etcd.io/bbolt"
	"google.golang.org/protobuf/proto"
)

func (c *Client) prepareRestore(opts *rpc.CriuOpts, args *task.RestoreArgs, checkpointPath string) (*string, error) {
	c.remoteStore = utils.NewCedanaStore(c.config)
	zipFile, err := c.remoteStore.GetCheckpoint(args.CheckpointId)
	if err != nil {
		return nil, err
	}

	tmpdir := "cedana_restore"
	// make temporary folder to decompress into
	err = os.Mkdir(tmpdir, 0755)
	if err != nil {
		return nil, err
	}

	c.logger.Info().Msgf("decompressing %s to %s", *zipFile, tmpdir)
	err = utils.UnzipFolder(*zipFile, tmpdir)
	if err != nil {
		// hack: error here is not fatal due to EOF (from a badly written utils.Compress)
		c.logger.Info().Err(err).Msg("error decompressing checkpoint")
	}

	// read serialized cedanaCheckpoint
	_, err = os.Stat(filepath.Join(tmpdir, "checkpoint_state.json"))
	if err != nil {
		c.logger.Fatal().Err(err).Msg("checkpoint_state.json not found, likely error in creating checkpoint")
		return nil, err
	}

	data, err := os.ReadFile(filepath.Join(tmpdir, "checkpoint_state.json"))
	if err != nil {
		c.logger.Fatal().Err(err).Msg("error reading checkpoint_state.json")
		return nil, err
	}

	var checkpointState task.ProcessState
	err = json.Unmarshal(data, &checkpointState)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("error unmarshaling checkpoint_state.json")
		return nil, err
	}

	// check open_fds. Useful for checking if process being restored
	// is a pts slave and for determining how to handle files that were being written to.
	// TODO: We should be looking at the images instead
	open_fds := checkpointState.ProcessInfo.OpenFds
	var isShellJob bool
	for _, f := range open_fds {
		if strings.Contains(f.Path, "pts") {
			isShellJob = true
			break
		}
	}
	opts.ShellJob = proto.Bool(isShellJob)

	c.restoreFiles(&checkpointState, tmpdir)

	// TODO: network restore logic
	// TODO: checksum val

	// Remove for now for testing
	// err = os.Remove(zipFile)
	if err != nil {
		return nil, err
	}

	return &tmpdir, nil
}

func (c *Client) ContainerRestore(imgPath string, containerId string) error {
	logger := utils.GetLogger()
	logger.Info().Msgf("restoring container %s from %s", containerId, imgPath)
	err := container.Restore(imgPath, containerId)
	if err != nil {
		c.logger.Fatal().Err(err)
	}
	return nil
}

// restoreFiles looks at the files copied during checkpoint and copies them back to the
// original path, creating folders along the way.
func (c *Client) restoreFiles(ps *task.ProcessState, dir string) {
	_, err := os.Stat(dir)
	if err != nil {
		return
	}
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		for _, f := range ps.ProcessInfo.OpenWriteOnlyFilePaths {
			if info.Name() == filepath.Base(f) {
				// copy file to path
				err = os.MkdirAll(filepath.Dir(f), 0755)
				if err != nil {
					return err
				}

				c.logger.Info().Msgf("copying file %s to %s", path, f)
				// copyFile copies to folder, so grab folder path
				err := utils.CopyFile(path, filepath.Dir(f))
				if err != nil {
					return err
				}

			}
		}
		return nil
	})

	if err != nil {
		c.logger.Fatal().Err(err).Msg("error copying files")
	}
}

func (c *Client) prepareRestoreOpts() *rpc.CriuOpts {
	opts := rpc.CriuOpts{
		LogLevel:       proto.Int32(2),
		LogFile:        proto.String("restore.log"),
		TcpEstablished: proto.Bool(true),
	}

	return &opts

}

func (c *Client) criuRestore(opts *rpc.CriuOpts, nfy utils.Notify, dir string) (*int32, error) {

	img, err := os.Open(dir)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("could not open directory")
	}
	defer img.Close()

	opts.ImagesDirFd = proto.Int32(int32(img.Fd()))

	resp, err := c.CRIU.Restore(opts, &nfy)
	if err != nil {
		// cleanup along the way
		os.RemoveAll(dir)
		c.logger.Warn().Msgf("error restoring process: %v", err)
		return nil, err
	}

	c.logger.Info().Msgf("process restored: %v", resp)

	// clean up
	err = os.RemoveAll(dir)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("error removing directory")
	}
	c.cleanupClient()
	return resp.Restore.Pid, nil
}

func (c *Client) pyTorchRestore() error {
	// TODO Not implemented yet
	return nil
}

func getPodmanConfigs(checkpointDir string) (*types.ContainerConfig, *types.ContainerState, error) {
	dumpSpec := new(spec.Spec)
	if _, err := utils.ReadJSONFile(dumpSpec, checkpointDir, "spec.dump"); err != nil {
		return nil, nil, err
	}

	return nil, nil, nil
}

func (c *Client) patchPodman(config *types.ContainerConfig, state *types.ContainerState) error {
	configNetworks := config.Networks
	configJSON, err := json.Marshal(config)
	if err != nil {
		return err
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return err
	}

	ctrID := []byte("test")
	ctrName := []byte("test")
	networks := make(map[string][]byte, len(configNetworks))
	for net, opts := range configNetworks {
		// Check that we don't have any empty network names
		if net == "" {
			return fmt.Errorf("network names cannot be an empty string")
		}
		if opts.InterfaceName == "" {
			return fmt.Errorf("network interface name cannot be an empty string")
		}
		optBytes, err := json.Marshal(opts)
		if err != nil {
			return fmt.Errorf("marshalling network options JSON for container %s: %w", ctrID, err)
		}
		networks[net] = optBytes
	}

	db := &utils.DB{Conn: nil, DbPath: "/var/lib/containers/storage/libpod/bolt_state.db"}

	if err := db.SetNewDbConn(); err != nil {
		return err
	}

	defer db.Conn.Close()

	db.Conn.Update(func(tx *bolt.Tx) error {

		idsBucket, err := utils.GetIDBucket(tx)
		if err != nil {
			return err
		}

		namesBucket, err := utils.GetNamesBucket(tx)
		if err != nil {
			return err
		}

		ctrBucket, err := utils.GetCtrBucket(tx)
		if err != nil {
			return err
		}

		allCtrsBucket, err := utils.GetAllCtrsBucket(tx)
		if err != nil {
			return err
		}

		// If a pod was given, check if it exists

		idExist := idsBucket.Get(ctrID)

		if idExist != nil {
			err = fmt.Errorf("ID \"%s\" is in use", ctrID)
			if allCtrsBucket.Get(idExist) == nil {
				err = fmt.Errorf("ID \"%s\" is in use but not found in all containers bucket", ctrID)
			}
			return fmt.Errorf("ID \"%s\" is in use: %w", string(ctrID), err)
		}

		nameExist := namesBucket.Get(ctrName)
		if nameExist != nil {
			err = fmt.Errorf("name \"%s\" is in use", string(ctrName))
			if allCtrsBucket.Get(nameExist) == nil {
				err = fmt.Errorf("name \"%s\" is in use but not found in all containers bucket", string(ctrName))
			}
			return fmt.Errorf("name \"%s\" is in use: %w", string(ctrName), err)
		}

		if err := idsBucket.Put(ctrID, ctrName); err != nil {
			return fmt.Errorf("adding container %s ID to DB: %w", string(ctrID), err)
		}
		if err := namesBucket.Put(ctrName, ctrID); err != nil {
			return fmt.Errorf("adding container %s name (%s) to DB: %w", string(ctrID), string(ctrName), err)
		}
		if err := allCtrsBucket.Put(ctrID, ctrName); err != nil {
			return fmt.Errorf("adding container %s to all containers bucket in DB: %w", string(ctrID), err)
		}

		newCtrBkt, err := ctrBucket.CreateBucket(ctrID)
		if err != nil {
			return fmt.Errorf("adding container %s bucket to DB: %w", string(ctrID), err)
		}

		if err := newCtrBkt.Put(utils.ConfigKey, configJSON); err != nil {
			return fmt.Errorf("adding container %s config to DB: %w", string(ctrID), err)
		}
		if err := newCtrBkt.Put(utils.StateKey, stateJSON); err != nil {
			return fmt.Errorf("adding container %s state to DB: %w", string(ctrID), err)
		}

		if len(networks) > 0 {
			ctrNetworksBkt, err := newCtrBkt.CreateBucket(utils.NetworksBkt)
			if err != nil {
				return fmt.Errorf("creating networks bucket for container %s: %w", string(ctrID), err)
			}
			for network, opts := range networks {
				if err := ctrNetworksBkt.Put([]byte(network), opts); err != nil {
					return fmt.Errorf("adding network %q to networks bucket for container %s: %w", network, string(ctrID), err)
				}
			}
		}

		if _, err := newCtrBkt.CreateBucket(utils.DependenciesBkt); err != nil {
			return fmt.Errorf("creating dependencies bucket for container %s: %w", string(ctrID), err)
		}
		// TODO missing add ctr to pod
		// TODO missing named config.NamedVolumes loop
		return nil
	})

	return nil
}

func (c *Client) RuncRestore(imgPath, containerId string, opts *container.RuncOpts) error {

	ctx := context.Background()

	if !opts.Detatch {
		jsonData, err := os.ReadFile(opts.Bundle + "config.json")
		if err != nil {
			return err
		}

		var data map[string]interface{}

		if err := json.Unmarshal(jsonData, &data); err != nil {
			return err
		}

		data["process"].(map[string]interface{})["terminal"] = false
		updatedJSON, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(opts.Bundle+"config.json", updatedJSON, 0644); err != nil {
			return err
		}
	}
	// Here is the podman patch
	utils.CRImportCheckpoint(ctx, filepath.Join(opts.Bundle, "checkpoint"))

	err := container.RuncRestore(imgPath, containerId, *opts)
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) Restore(args *task.RestoreArgs) (*int32, error) {
	defer c.timeTrack(time.Now(), "restore")
	var dir *string
	var pid *int32

	opts := c.prepareRestoreOpts()
	nfy := utils.Notify{
		Config:          c.config,
		Logger:          c.logger,
		PreDumpAvail:    true,
		PostDumpAvail:   true,
		PreRestoreAvail: true,
	}

	dir, err := c.prepareRestore(opts, nil, args.Dir)
	if err != nil {
		return nil, err
	}
	pid, err = c.criuRestore(opts, nfy, *dir)
	if err != nil {
		return nil, err
	}

	return pid, nil
}
