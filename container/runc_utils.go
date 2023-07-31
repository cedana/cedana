package container

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/checkpoint-restore/go-criu/v5"
	criurpc "github.com/checkpoint-restore/go-criu/v5/rpc"
	"github.com/opencontainers/runc/libcontainer/cgroups"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

// This file contains functions lifted from https://github.com/opencontainers/runc/blob/main/libcontainer/container_linux.go,
// to allow directly using criu for container checkpointing the way runc does it, with some slight modifications.

var criuFeatures *criurpc.CriuFeatures

func (c *RuncContainer) checkCriuFeatures(criuOpts *CriuOpts, rpcOpts *criurpc.CriuOpts, criuFeat *criurpc.CriuFeatures) error {
	t := criurpc.CriuReqType_FEATURE_CHECK

	// make sure the features we are looking for are really not from
	// some previous check
	criuFeatures = nil

	req := &criurpc.CriuReq{
		Type: &t,
		// Theoretically this should not be necessary but CRIU
		// segfaults if Opts is empty.
		// Fixed in CRIU  2.12
		Opts:     rpcOpts,
		Features: criuFeat,
	}

	err := c.criuSwrk(nil, req, criuOpts, nil)
	if err != nil {
		logrus.Debugf("%s", err)
		return errors.New("CRIU feature check failed")
	}

	missingFeatures := false

	// The outer if checks if the fields actually exist
	if (criuFeat.MemTrack != nil) &&
		(criuFeatures.MemTrack != nil) {
		// The inner if checks if they are set to true
		if *criuFeat.MemTrack && !*criuFeatures.MemTrack {
			missingFeatures = true
			logrus.Debugf("CRIU does not support MemTrack")
		}
	}

	// This needs to be repeated for every new feature check.
	// Is there a way to put this in a function. Reflection?
	if (criuFeat.LazyPages != nil) &&
		(criuFeatures.LazyPages != nil) {
		if *criuFeat.LazyPages && !*criuFeatures.LazyPages {
			missingFeatures = true
			logrus.Debugf("CRIU does not support LazyPages")
		}
	}

	if missingFeatures {
		return errors.New("CRIU is missing features")
	}

	return nil
}

func compareCriuVersion(criuVersion int, minVersion int) error {
	// simple function to perform the actual version compare
	if criuVersion < minVersion {
		return fmt.Errorf("CRIU version %d must be %d or higher", criuVersion, minVersion)
	}

	return nil
}

// checkCriuVersion checks CRIU version greater than or equal to minVersion.
func (c *RuncContainer) checkCriuVersion(minVersion int) error {
	// If the version of criu has already been determined there is no need
	// to ask criu for the version again. Use the value from c.criuVersion.
	if c.criuVersion != 0 {
		return compareCriuVersion(c.criuVersion, minVersion)
	}

	criu := criu.MakeCriu()
	var err error
	c.criuVersion, err = criu.GetCriuVersion()
	if err != nil {
		return fmt.Errorf("CRIU version check failed: %w", err)
	}

	return compareCriuVersion(c.criuVersion, minVersion)
}

func (c *RuncContainer) criuApplyCgroups(pid int, req *criurpc.CriuReq) error {
	// need to apply cgroups only on restore
	if req.GetType() != criurpc.CriuReqType_RESTORE {
		return nil
	}

	// XXX: Do we need to deal with this case? AFAIK criu still requires root.
	if err := c.cgroupManager.Apply(pid); err != nil {
		return err
	}

	if err := c.cgroupManager.Set(c.config.Cgroups.Resources); err != nil {
		return err
	}

	// TODO(@kolyshkin): should we use c.cgroupManager.GetPaths()
	// instead of reading /proc/pid/cgroup?
	path := fmt.Sprintf("/proc/%d/cgroup", pid)
	cgroupsPaths, err := cgroups.ParseCgroupFile(path)
	if err != nil {
		return err
	}

	for c, p := range cgroupsPaths {
		cgroupRoot := &criurpc.CgroupRoot{
			Ctrl: proto.String(c),
			Path: proto.String(p),
		}
		req.Opts.CgRoot = append(req.Opts.CgRoot, cgroupRoot)
	}

	return nil
}

func getPipeFds(pid int) ([]string, error) {
	fds := make([]string, 3)

	dirPath := filepath.Join("/proc", strconv.Itoa(pid), "/fd")
	for i := 0; i < 3; i++ {
		// XXX: This breaks if the path is not a valid symlink (which can
		//      happen in certain particularly unlucky mount namespace setups).
		f := filepath.Join(dirPath, strconv.Itoa(i))
		target, err := os.Readlink(f)
		if err != nil {
			// Ignore permission errors, for rootless containers and other
			// non-dumpable processes. if we can't get the fd for a particular
			// file, there's not much we can do.
			if os.IsPermission(err) {
				continue
			}
			return fds, err
		}
		fds[i] = target
	}
	return fds, nil
}

// block any external network activity
// we need to implement the network strategy interface for all the different
// types of network devices before taking advantage of locking & unlocking

// there might be a linux level hack here, TODO NR - explore

func lockNetwork(config *configs.Config) error {
	// 	for _, config := range config.Networks {
	// strategy, err := getStrategy(config.Type)
	// if err != nil {
	// return err
	// }

	// if err := strategy.detach(config); err != nil {
	// return err
	// }
	// }
	return nil
}

func unlockNetwork(config *configs.Config) error {
	//	for _, config := range config.Networks {
	//
	// strategy, err := getStrategy(config.Type)
	// if err != nil {
	// return err
	// }
	// if err = strategy.attach(config); err != nil {
	// return err
	// }
	// }
	return nil
}
