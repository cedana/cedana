package crio

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	systemdDbus "github.com/coreos/go-systemd/v22/dbus"
	"github.com/cri-o/cri-o/utils/cmdrunner"
	dbus "github.com/godbus/dbus/v5"
	"github.com/opencontainers/runc/libcontainer/userns"
)

var (
	dbusC        *systemdDbus.Conn
	dbusMu       sync.RWMutex
	dbusInited   bool
	dbusRootless bool
)

type DbusConnManager struct{}

// newUserSystemdDbus creates a connection for systemd user-instance.
func newUserSystemdDbus() (*systemdDbus.Conn, error) {
	addr, err := DetectUserDbusSessionBusAddress()
	if err != nil {
		return nil, err
	}
	uid, err := DetectUID()
	if err != nil {
		return nil, err
	}

	return systemdDbus.NewConnection(func() (*dbus.Conn, error) {
		conn, err := dbus.Dial(addr)
		if err != nil {
			return nil, fmt.Errorf("error while dialing %q: %w", addr, err)
		}
		methods := []dbus.Auth{dbus.AuthExternal(strconv.Itoa(uid))}
		err = conn.Auth(methods)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("error while authenticating connection, address=%q, UID=%d: %w", addr, uid, err)
		}
		if err = conn.Hello(); err != nil {
			conn.Close()
			return nil, fmt.Errorf("error while sending Hello message, address=%q, UID=%d: %w", addr, uid, err)
		}
		return conn, nil
	})
}

// DetectUID detects UID from the OwnerUID field of `busctl --user status`
// if running in userNS. The value corresponds to sd_bus_creds_get_owner_uid(3) .
//
// Otherwise returns os.Getuid() .
func DetectUID() (int, error) {
	if !userns.RunningInUserNS() {
		return os.Getuid(), nil
	}
	b, err := cmdrunner.Command("busctl", "--user", "--no-pager", "status").CombinedOutput()
	if err != nil {
		return -1, fmt.Errorf("could not execute `busctl --user --no-pager status`: %q: %w", string(b), err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(b))
	for scanner.Scan() {
		s := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(s, "OwnerUID=") {
			uidStr := strings.TrimPrefix(s, "OwnerUID=")
			i, err := strconv.Atoi(uidStr)
			if err != nil {
				return -1, fmt.Errorf("could not detect the OwnerUID: %s: %w", s, err)
			}
			return i, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return -1, err
	}
	return -1, errors.New("could not detect the OwnerUID")
}

// DetectUserDbusSessionBusAddress returns $DBUS_SESSION_BUS_ADDRESS if set.
// Otherwise returns "unix:path=$XDG_RUNTIME_DIR/bus" if $XDG_RUNTIME_DIR/bus exists.
// Otherwise parses the value from `systemctl --user show-environment` .
func DetectUserDbusSessionBusAddress() (string, error) {
	if env := os.Getenv("DBUS_SESSION_BUS_ADDRESS"); env != "" {
		return env, nil
	}
	if xdr := os.Getenv("XDG_RUNTIME_DIR"); xdr != "" {
		busPath := filepath.Join(xdr, "bus")
		if _, err := os.Stat(busPath); err == nil {
			busAddress := "unix:path=" + busPath
			return busAddress, nil
		}
	}
	b, err := cmdrunner.Command("systemctl", "--user", "--no-pager", "show-environment").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("could not execute `systemctl --user --no-pager show-environment`, output=%q: %w", string(b), err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(b))
	for scanner.Scan() {
		s := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(s, "DBUS_SESSION_BUS_ADDRESS=") {
			return strings.TrimPrefix(s, "DBUS_SESSION_BUS_ADDRESS="), nil
		}
	}
	return "", errors.New("could not detect DBUS_SESSION_BUS_ADDRESS from `systemctl --user --no-pager show-environment`. Make sure you have installed the dbus-user-session or dbus-daemon package and then run: `systemctl --user start dbus`")
}

// NewDbusConnManager initializes systemd dbus connection manager.
func NewDbusConnManager(rootless bool) *DbusConnManager {
	dbusMu.Lock()
	defer dbusMu.Unlock()
	if dbusInited && rootless != dbusRootless {
		panic("can't have both root and rootless dbus")
	}
	dbusRootless = rootless
	dbusInited = true
	return &DbusConnManager{}
}

// getConnection lazily initializes and returns systemd dbus connection.
func (d *DbusConnManager) GetConnection() (*systemdDbus.Conn, error) {
	// In the case where dbusC != nil
	// Use the read lock the first time to ensure
	// that Conn can be acquired at the same time.
	dbusMu.RLock()
	if conn := dbusC; conn != nil {
		dbusMu.RUnlock()
		return conn, nil
	}
	dbusMu.RUnlock()

	// In the case where dbusC == nil
	// Use write lock to ensure that only one
	// will be created
	dbusMu.Lock()
	defer dbusMu.Unlock()
	if conn := dbusC; conn != nil {
		return conn, nil
	}

	conn, err := d.newConnection()
	if err != nil {
		return nil, err
	}
	dbusC = conn
	return conn, nil
}

func (d *DbusConnManager) newConnection() (*systemdDbus.Conn, error) {
	if dbusRootless {
		return newUserSystemdDbus()
	}
	return systemdDbus.NewWithContext(context.TODO())
}

// RetryOnDisconnect calls op, and if the error it returns is about closed dbus
// connection, the connection is re-established and the op is retried. This helps
// with the situation when dbus is restarted and we have a stale connection.
func (d *DbusConnManager) RetryOnDisconnect(op func(*systemdDbus.Conn) error) error {
	for {
		conn, err := d.GetConnection()
		if err != nil {
			return err
		}
		err = op(conn)
		if err == nil {
			return nil
		}
		if errors.Is(err, syscall.EAGAIN) {
			continue
		}
		if !errors.Is(err, dbus.ErrClosed) {
			return err
		}
		// dbus connection closed, we should reconnect and try again
		d.resetConnection(conn)
	}
}

// resetConnection resets the connection to its initial state
// (so it can be reconnected if necessary).
func (d *DbusConnManager) resetConnection(conn *systemdDbus.Conn) {
	dbusMu.Lock()
	defer dbusMu.Unlock()
	if dbusC != nil && dbusC == conn {
		dbusC.Close()
		dbusC = nil
	}
}
