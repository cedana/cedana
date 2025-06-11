package runc

import (
	"os"
	"strconv"
	"strings"

	"github.com/moby/sys/userns"
	"github.com/opencontainers/cgroups/systemd"
)

func ShouldUseRootlessCgroupManager(rootless string, systemdCgroup bool) (bool, error) {
	b, err := parseBoolOrAuto(rootless)
	if err != nil {
		return false, err
	}
	// nil b stands for "auto detect"
	if b != nil {
		return *b, nil
	}
	if os.Geteuid() != 0 {
		return true, nil
	}
	if !userns.RunningInUserNS() {
		// euid == 0 , in the initial ns (i.e. the real root)
		return false, nil
	}
	// euid = 0, in a userns.
	//
	// [systemd driver]
	// We can call DetectUID() to parse the OwnerUID value from `busctl --user --no-pager status` result.
	// The value corresponds to sd_bus_creds_get_owner_uid(3).
	// If the value is 0, we have rootful systemd inside userns, so we do not need the rootless cgroup manager.
	//
	// On error, we assume we are root. An error may happen during shelling out to `busctl` CLI,
	// mostly when $DBUS_SESSION_BUS_ADDRESS is unset.
	if systemdCgroup {
		ownerUID, err := systemd.DetectUID()
		if err != nil {
			ownerUID = 0
		}
		return ownerUID != 0, nil
	}
	// [cgroupfs driver]
	// As we are unaware of cgroups path, we can't determine whether we have the full
	// access to the cgroups path.
	// Either way, we can safely decide to use the rootless cgroups manager.
	return true, nil
}

// parseBoolOrAuto returns (nil, nil) if s is empty or "auto"
func parseBoolOrAuto(s string) (*bool, error) {
	if s == "" || strings.ToLower(s) == "auto" {
		return nil, nil
	}
	b, err := strconv.ParseBool(s)
	return &b, err
}
