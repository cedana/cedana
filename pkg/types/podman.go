package types

import (
	"net"
	"sync"
	"time"

	"github.com/docker/docker/pkg/idtools"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/tchap/go-patricia/patricia"
)

// ContainerStatus represents the current state of a container
type ContainerStatus int

const (
	// ContainerStateUnknown indicates that the container is in an error
	// state where information about it cannot be retrieved
	ContainerStateUnknown ContainerStatus = iota
	// ContainerStateConfigured indicates that the container has had its
	// storage configured but it has not been created in the OCI runtime
	ContainerStateConfigured ContainerStatus = iota
	// ContainerStateCreated indicates the container has been created in
	// the OCI runtime but not started
	ContainerStateCreated ContainerStatus = iota
	// ContainerStateRunning indicates the container is currently executing
	ContainerStateRunning ContainerStatus = iota
	// ContainerStateStopped indicates that the container was running but has
	// exited
	ContainerStateStopped ContainerStatus = iota
	// ContainerStatePaused indicates that the container has been paused
	ContainerStatePaused ContainerStatus = iota
	// ContainerStateExited indicates the container has stopped and been
	// cleaned up
	ContainerStateExited ContainerStatus = iota
	// ContainerStateRemoving indicates the container is in the process of
	// being removed.
	ContainerStateRemoving ContainerStatus = iota
	// ContainerStateStopping indicates the container is in the process of
	// being stopped.
	ContainerStateStopping ContainerStatus = iota
)

type StatusBlock struct {
	// Interfaces contains the created network interface in the container.
	// The map key is the interface name.
	Interfaces map[string]NetInterface `json:"interfaces,omitempty"`
	// DNSServerIPs nameserver addresses which should be added to
	// the containers resolv.conf file.
	DNSServerIPs []net.IP `json:"dns_server_ips,omitempty"`
	// DNSSearchDomains search domains which should be added to
	// the containers resolv.conf file.
	DNSSearchDomains []string `json:"dns_search_domains,omitempty"`
}

type NetInterface struct {
	// Subnets list of assigned subnets with their gateway.
	Subnets []NetAddress `json:"subnets,omitempty"`
	// MacAddress for this Interface.
	MacAddress HardwareAddr `json:"mac_address"`
}
type NetAddress struct {
	// IPNet of this NetAddress. Note that this is a subnet but it has to contain the
	// actual ip of the network interface and not the network address.
	IPNet IPNet `json:"ipnet"`
	// Gateway for the network. This can be empty if there is no gateway, e.g. internal network.
	Gateway net.IP `json:"gateway,omitempty"`
}
type IPNet struct {
	net.IPNet
}

type LockFile struct {
	// The following fields are only set when constructing *LockFile, and must never be modified afterwards.
	// They are safe to access without any other locking.
	file string
	ro   bool

	// rwMutex serializes concurrent reader-writer acquisitions in the same process space
	rwMutex *sync.RWMutex
	// stateMutex is used to synchronize concurrent accesses to the state below
	stateMutex *sync.Mutex
	counter    int64
	lw         LastWrite // A global value valid as of the last .Touch() or .Modified()
	lockType   lockType
	locked     bool
	// The following fields are only modified on transitions between counter == 0 / counter != 0.
	// Thus, they can be safely accessed by users _that currently hold the LockFile_ without locking.
	// In other cases, they need to be protected using stateMutex.
	fd fileHandle
}

type fileHandle uintptr

type lockType byte

type LastWrite struct {
	// Never modify fields of a LastWrite object; it has value semantics.
	state []byte // Contents of the lock file.
}

type containerLocations uint8

// The backing store is split in two json files, one (the volatile)
// that is written without fsync() meaning it isn't as robust to
// unclean shutdown
const (
	stableContainerLocation containerLocations = 1 << iota
	volatileContainerLocation

	numContainerLocationIndex = iota
)

func containerLocationFromIndex(index int) containerLocations {
	return 1 << index
}

type containerStore struct {
	// The following fields are only set when constructing containerStore, and must never be modified afterwards.
	// They are safe to access without any other locking.
	lockfile *LockFile // Synchronizes readers vs. writers of the _filesystem data_, both cross-process and in-process.
	dir      string
	jsonPath [numContainerLocationIndex]string

	inProcessLock sync.RWMutex // Can _only_ be obtained with lockfile held.
	// The following fields can only be read/written with read/write ownership of inProcessLock, respectively.
	// Almost all users should use startReading() or startWriting().
	lastWrite  LastWrite
	containers []*Container
	idindex    *TruncIndex
	byid       map[string]*Container
	bylayer    map[string]*Container
	byname     map[string]*Container
}

type TruncIndex struct {
	sync.RWMutex
	trie *patricia.Trie
	ids  map[string]struct{}
}

type Digest string

type Container struct {
	// ID is either one which was specified at create-time, or a random
	// value which was generated by the library.
	ID string `json:"id"`

	// Names is an optional set of user-defined convenience values.  The
	// container can be referred to by its ID or any of its names.  Names
	// are unique among containers.
	Names []string `json:"names,omitempty"`

	// ImageID is the ID of the image which was used to create the container.
	ImageID string `json:"image"`

	// LayerID is the ID of the read-write layer for the container itself.
	// It is assumed that the image's top layer is the parent of the container's
	// read-write layer.
	LayerID string `json:"layer"`

	// Metadata is data we keep for the convenience of the caller.  It is not
	// expected to be large, since it is kept in memory.
	Metadata string `json:"metadata,omitempty"`

	// BigDataNames is a list of names of data items that we keep for the
	// convenience of the caller.  They can be large, and are only in
	// memory when being read from or written to disk.
	BigDataNames []string `json:"big-data-names,omitempty"`

	// BigDataSizes maps the names in BigDataNames to the sizes of the data
	// that has been stored, if they're known.
	BigDataSizes map[string]int64 `json:"big-data-sizes,omitempty"`

	// BigDataDigests maps the names in BigDataNames to the digests of the
	// data that has been stored, if they're known.
	BigDataDigests map[string]Digest `json:"big-data-digests,omitempty"`

	// Created is the datestamp for when this container was created.  Older
	// versions of the library did not track this information, so callers
	// will likely want to use the IsZero() method to verify that a value
	// is set before using it.
	Created time.Time `json:"created,omitempty"`

	// UIDMap and GIDMap are used for setting up a container's root
	// filesystem for use inside of a user namespace where UID mapping is
	// being used.
	UIDMap []IDMap `json:"uidmap,omitempty"`
	GIDMap []IDMap `json:"gidmap,omitempty"`

	Flags map[string]interface{} `json:"flags,omitempty"`

	// volatileStore is true if the container is from the volatile json file
	volatileStore bool `json:"-"`
}

type IDMap struct {
	ContainerID int `json:"container_id"`
	HostID      int `json:"host_id"`
	Size        int `json:"size"`
}

type HardwareAddr net.HardwareAddr

type ContainerConfig struct {
	// Spec is OCI runtime spec used to create the container. This is passed
	// in when the container is created, but it is not the final spec used
	// to run the container - it will be modified by Libpod to add things we
	// manage (e.g. bind mounts for /etc/resolv.conf, named volumes, a
	// network namespace prepared by the network backend) in the
	// generateSpec() function.
	Spec *spec.Spec `json:"spec"`

	// ID is a hex-encoded 256-bit pseudorandom integer used as a unique
	// identifier for the container. IDs are globally unique in Libpod -
	// once an ID is in use, no other container or pod will be created with
	// the same one until the holder of the ID has been removed.
	// ID is generated by Libpod, and cannot be chosen or influenced by the
	// user (except when restoring a checkpointed container).
	// ID is guaranteed to be 64 characters long.
	ID string `json:"id"`

	// Name is a human-readable name for the container. All containers must
	// have a non-empty name. Name may be provided when the container is
	// created; if no name is chosen, a name will be auto-generated.
	Name string `json:"name"`

	// Pod is the full ID of the pod the container belongs to. If the
	// container does not belong to a pod, this will be empty.
	// If this is not empty, a pod with this ID is guaranteed to exist in
	// the state for the duration of this container's existence.
	Pod string `json:"pod,omitempty"`

	// Namespace is the libpod Namespace the container is in.
	// Namespaces are used to divide containers in the state.
	Namespace string `json:"namespace,omitempty"`

	// LockID is the ID of this container's lock. Each container, pod, and
	// volume is assigned a unique Lock (from one of several backends) by
	// the libpod Runtime. This lock will belong only to this container for
	// the duration of the container's lifetime.
	LockID uint32 `json:"lockID"`

	// CreateCommand is the full command plus arguments that were used to
	// create the container. It is shown in the output of Inspect, and may
	// be used to recreate an identical container for automatic updates or
	// portable systemd unit files.
	CreateCommand []string `json:"CreateCommand,omitempty"`

	// RawImageName is the raw and unprocessed name of the image when creating
	// the container (as specified by the user).  May or may not be set.  One
	// use case to store this data are auto-updates where we need the _exact_
	// name and not some normalized instance of it.
	RawImageName string `json:"RawImageName,omitempty"`

	// IDMappings are UID/GID mappings used by the container's user
	// namespace. They are used by the OCI runtime when creating the
	// container, and by c/storage to ensure that the container's files have
	// the appropriate owner.
	IDMappings IDMappingOptions `json:"idMappingsOptions,omitempty"`

	// Dependencies are the IDs of dependency containers.
	// These containers must be started before this container is started.
	Dependencies []string

	// rewrite is an internal bool to indicate that the config was modified after
	// a read from the db, e.g. to migrate config fields after an upgrade.
	// This field should never be written to the db, the json tag ensures this.
	rewrite bool `json:"-"`

	// embedded sub-configs
	ContainerRootFSConfig
	ContainerSecurityConfig
	ContainerNameSpaceConfig
	ContainerNetworkConfig
	ContainerImageConfig
	ContainerMiscConfig
}
type ContainerMiscConfig struct {
	// Whether to keep container STDIN open
	Stdin bool `json:"stdin,omitempty"`
	// Labels is a set of key-value pairs providing additional information
	// about a container
	Labels map[string]string `json:"labels,omitempty"`
	// StopSignal is the signal that will be used to stop the container
	StopSignal uint `json:"stopSignal,omitempty"`
	// StopTimeout is the signal that will be used to stop the container
	StopTimeout uint `json:"stopTimeout,omitempty"`
	// Timeout is maximum time a container will run before getting the kill signal
	Timeout uint `json:"timeout,omitempty"`
	// Time container was created
	CreatedTime time.Time `json:"createdTime"`
	// CgroupManager is the cgroup manager used to create this container.
	// If empty, the runtime default will be used.
	CgroupManager string `json:"cgroupManager,omitempty"`
	// NoCgroups indicates that the container will not create Cgroups. It is
	// incompatible with CgroupParent.  Deprecated in favor of CgroupsMode.
	NoCgroups bool `json:"noCgroups,omitempty"`
	// CgroupsMode indicates how the container will create cgroups
	// (disabled, no-conmon, enabled).  It supersedes NoCgroups.
	CgroupsMode string `json:"cgroupsMode,omitempty"`
	// Cgroup parent of the container.
	CgroupParent string `json:"cgroupParent"`
	// GroupEntry specifies arbitrary data to append to a file.
	GroupEntry string `json:"group_entry,omitempty"`
	// KubeExitCodePropagation of the service container.
	KubeExitCodePropagation KubeExitCodePropagation `json:"kubeExitCodePropagation"`
	// LogPath log location
	LogPath string `json:"logPath"`
	// LogTag is the tag used for logging
	LogTag string `json:"logTag"`
	// LogSize is the tag used for logging
	LogSize int64 `json:"logSize"`
	// LogDriver driver for logs
	LogDriver string `json:"logDriver"`
	// File containing the conmon PID
	ConmonPidFile string `json:"conmonPidFile,omitempty"`
	// RestartPolicy indicates what action the container will take upon
	// exiting naturally.
	// Allowed options are "no" (take no action), "on-failure" (restart on
	// non-zero exit code, up to a maximum of RestartRetries times),
	// and "always" (always restart the container on any exit code).
	// The empty string is treated as the default ("no")
	RestartPolicy string `json:"restart_policy,omitempty"`
	// RestartRetries indicates the number of attempts that will be made to
	// restart the container. Used only if RestartPolicy is set to
	// "on-failure".
	RestartRetries uint `json:"restart_retries,omitempty"`
	// PostConfigureNetNS needed when a user namespace is created by an OCI runtime
	// if the network namespace is created before the user namespace it will be
	// owned by the wrong user namespace.
	PostConfigureNetNS bool `json:"postConfigureNetNS"`
	// OCIRuntime used to create the container
	OCIRuntime string `json:"runtime,omitempty"`
	// IsInfra is a bool indicating whether this container is an infra container used for
	// sharing kernel namespaces in a pod
	IsInfra bool `json:"pause"`
	// IsService is a bool indicating whether this container is a service container used for
	// tracking the life cycle of K8s service.
	IsService bool `json:"isService"`
	// SdNotifyMode tells libpod what to do with a NOTIFY_SOCKET if passed
	SdNotifyMode string `json:"sdnotifyMode,omitempty"`
	// SdNotifySocket stores NOTIFY_SOCKET in use by the container
	SdNotifySocket string `json:"sdnotifySocket,omitempty"`
	// Systemd tells libpod to set up the container in systemd mode, a value of nil denotes false
	Systemd *bool `json:"systemd,omitempty"`
	// HealthCheckConfig has the health check command and related timings
	HealthCheckConfig *Schema2HealthConfig `json:"healthcheck"`
	// HealthCheckOnFailureAction defines an action to take once the container turns unhealthy.
	HealthCheckOnFailureAction HealthCheckOnFailureAction `json:"healthcheck_on_failure_action"`
	// StartupHealthCheckConfig is the configuration of the startup
	// healthcheck for the container. This will run before the regular HC
	// runs, and when it passes the regular HC will be activated.
	StartupHealthCheckConfig *StartupHealthCheck `json:"startupHealthCheck,omitempty"`
	// PreserveFDs is a number of additional file descriptors (in addition
	// to 0, 1, 2) that will be passed to the executed process. The total FDs
	// passed will be 3 + PreserveFDs.
	PreserveFDs uint `json:"preserveFds,omitempty"`
	// Timezone is the timezone inside the container.
	// Local means it has the same timezone as the host machine
	Timezone string `json:"timezone,omitempty"`
	// Umask is the umask inside the container.
	Umask string `json:"umask,omitempty"`
	// PidFile is the file that saves the pid of the container process
	PidFile string `json:"pid_file,omitempty"`
	// CDIDevices contains devices that use the CDI
	CDIDevices []string `json:"cdiDevices,omitempty"`
	// DeviceHostSrc contains the original source on the host
	DeviceHostSrc []spec.LinuxDevice `json:"device_host_src,omitempty"`
	// EnvSecrets are secrets that are set as environment variables
	EnvSecrets map[string]*Secret `json:"secret_env,omitempty"`
	// InitContainerType specifies if the container is an initcontainer
	// and if so, what type: always or once are possible non-nil entries
	InitContainerType string `json:"init_container_type,omitempty"`
	// PasswdEntry specifies arbitrary data to append to a file.
	PasswdEntry string `json:"passwd_entry,omitempty"`
	// MountAllDevices is an option to indicate whether a privileged container
	// will mount all the host's devices
	MountAllDevices bool `json:"mountAllDevices"`
	// ReadWriteTmpfs indicates whether all tmpfs should be mounted readonly when in ReadOnly mode
	ReadWriteTmpfs bool `json:"readWriteTmpfs"`
}

type StartupHealthCheck struct {
	Schema2HealthConfig
	// Successes are the number of successes required to mark the startup HC
	// as passed.
	// If set to 0, a single success will mark the HC as passed.
	Successes int `json:",omitempty"`
}

type HealthCheckOnFailureAction int

type Schema2HealthConfig struct {
	// Test is the test to perform to check that the container is healthy.
	// An empty slice means to inherit the default.
	// The options are:
	// {} : inherit healthcheck
	// {"NONE"} : disable healthcheck
	// {"CMD", args...} : exec arguments directly
	// {"CMD-SHELL", command} : run command with system's default shell
	Test []string `json:",omitempty"`

	// Zero means to inherit. Durations are expressed as integer nanoseconds.
	StartPeriod time.Duration `json:",omitempty"` // StartPeriod is the time to wait after starting before running the first check.
	Interval    time.Duration `json:",omitempty"` // Interval is the time to wait between checks.
	Timeout     time.Duration `json:",omitempty"` // Timeout is the time to wait before considering the check to have hung.

	// Retries is the number of consecutive failures needed to consider a container as unhealthy.
	// Zero means inherit.
	Retries int `json:",omitempty"`
}

type KubeExitCodePropagation int

type ContainerImageConfig struct {
	// UserVolumes contains user-added volume mounts in the container.
	// These will not be added to the container's spec, as it is assumed
	// they are already present in the spec given to Libpod. Instead, it is
	// used when committing containers to generate the VOLUMES field of the
	// image that is created, and for triggering some OCI hooks which do not
	// fire unless user-added volume mounts are present.
	UserVolumes []string `json:"userVolumes,omitempty"`
	// Entrypoint is the container's entrypoint.
	// It is not used in spec generation, but will be used when the
	// container is committed to populate the entrypoint of the new image.
	Entrypoint []string `json:"entrypoint,omitempty"`
	// Command is the container's command.
	// It is not used in spec generation, but will be used when the
	// container is committed to populate the command of the new image.
	Command []string `json:"command,omitempty"`
}
type ContainerNetworkConfig struct {
	// CreateNetNS indicates that libpod should create and configure a new
	// network namespace for the container.
	// This cannot be set if NetNsCtr is also set.
	CreateNetNS bool `json:"createNetNS"`
	// StaticIP is a static IP to request for the container.
	// This cannot be set unless CreateNetNS is set.
	// If not set, the container will be dynamically assigned an IP by CNI.
	// Deprecated: Do no use this anymore, this is only for DB backwards compat.
	StaticIP net.IP `json:"staticIP,omitempty"`
	// StaticMAC is a static MAC to request for the container.
	// This cannot be set unless CreateNetNS is set.
	// If not set, the container will be dynamically assigned a MAC by CNI.
	// Deprecated: Do no use this anymore, this is only for DB backwards compat.
	StaticMAC HardwareAddr `json:"staticMAC,omitempty"`
	// PortMappings are the ports forwarded to the container's network
	// namespace
	// These are not used unless CreateNetNS is true
	PortMappings []PortMapping `json:"newPortMappings,omitempty"`
	// OldPortMappings are the ports forwarded to the container's network
	// namespace. As of podman 4.0 this field is deprecated, use PortMappings
	// instead. The db will convert the old ports to the new structure for you.
	// These are not used unless CreateNetNS is true
	OldPortMappings []OCICNIPortMapping `json:"portMappings,omitempty"`
	// ExposedPorts are the ports which are exposed but not forwarded
	// into the container.
	// The map key is the port and the string slice contains the protocols,
	// e.g. tcp and udp
	// These are only set when exposed ports are given but not published.
	ExposedPorts map[uint16][]string `json:"exposedPorts,omitempty"`
	// UseImageResolvConf indicates that resolv.conf should not be
	// bind-mounted inside the container.
	// Conflicts with DNSServer, DNSSearch, DNSOption.
	UseImageResolvConf bool
	// DNS servers to use in container resolv.conf
	// Will override servers in host resolv if set
	DNSServer []net.IP `json:"dnsServer,omitempty"`
	// DNS Search domains to use in container resolv.conf
	// Will override search domains in host resolv if set
	DNSSearch []string `json:"dnsSearch,omitempty"`
	// DNS options to be set in container resolv.conf
	// With override options in host resolv if set
	DNSOption []string `json:"dnsOption,omitempty"`
	// UseImageHosts indicates that /etc/hosts should not be
	// bind-mounted inside the container.
	// Conflicts with HostAdd.
	UseImageHosts bool
	// Hosts to add in container
	// Will be appended to host's host file
	HostAdd []string `json:"hostsAdd,omitempty"`
	// Network names with the network specific options.
	// Please note that these can be altered at runtime. The actual list is
	// stored in the DB and should be retrieved from there via c.networks()
	// this value is only used for container create.
	// Added in podman 4.0, previously NetworksDeprecated was used. Make
	// sure to not change the json tags.
	Networks map[string]PerNetworkOptions `json:"newNetworks,omitempty"`
	// Network names to add container to. Empty to use default network.
	// Please note that these can be altered at runtime. The actual list is
	// stored in the DB and should be retrieved from there; this is only the
	// set of networks the container was *created* with.
	// Deprecated: Do no use this anymore, this is only for DB backwards compat.
	// Also note that we need to keep the old json tag to decode from DB correctly
	NetworksDeprecated []string `json:"networks,omitempty"`
	// Network mode specified for the default network.
	NetMode NetworkMode `json:"networkMode,omitempty"`
	// NetworkOptions are additional options for each network
	NetworkOptions map[string][]string `json:"network_options,omitempty"`
}
type OCICNIPortMapping struct {
	// HostPort is the port number on the host.
	HostPort int32 `json:"hostPort"`
	// ContainerPort is the port number inside the sandbox.
	ContainerPort int32 `json:"containerPort"`
	// Protocol is the protocol of the port mapping.
	Protocol string `json:"protocol"`
	// HostIP is the host ip to use.
	HostIP string `json:"hostIP"`
}

type NetworkMode string

type PortMapping struct {
	// HostIP is the IP that we will bind to on the host.
	// If unset, assumed to be 0.0.0.0 (all interfaces).
	HostIP string `json:"host_ip"`
	// ContainerPort is the port number that will be exposed from the
	// container.
	// Mandatory.
	ContainerPort uint16 `json:"container_port"`
	// HostPort is the port number that will be forwarded from the host into
	// the container.
	// If omitted, a random port on the host (guaranteed to be over 1024)
	// will be assigned.
	HostPort uint16 `json:"host_port"`
	// Range is the number of ports that will be forwarded, starting at
	// HostPort and ContainerPort and counting up.
	// This is 1-indexed, so 1 is assumed to be a single port (only the
	// Hostport:Containerport mapping will be added), 2 is two ports (both
	// Hostport:Containerport and Hostport+1:Containerport+1), etc.
	// If unset, assumed to be 1 (a single port).
	// Both hostport + range and containerport + range must be less than
	// 65536.
	Range uint16 `json:"range"`
	// Protocol is the protocol forward.
	// Must be either "tcp", "udp", and "sctp", or some combination of these
	// separated by commas.
	// If unset, assumed to be TCP.
	Protocol string `json:"protocol"`
}
type StoreContainer struct {
	ID       string `json:"id"`
	Names    []string
	Image    string
	Layer    string
	Metadata string `json:"metadata"`
	Created  time.Time
	Flags    struct {
		MountLabel   string
		ProcessLabel string
	}
}

type ContainerSecurityConfig struct {
	// Privileged is whether the container is privileged. Privileged
	// containers have lessened security and increased access to the system.
	// Note that this does NOT directly correspond to Podman's --privileged
	// flag - most of the work of that flag is done in creating the OCI spec
	// given to Libpod. This only enables a small subset of the overall
	// operation, mostly around mounting the container image with reduced
	// security.
	Privileged bool `json:"privileged"`
	// ProcessLabel is the SELinux process label for the container.
	ProcessLabel string `json:"ProcessLabel,omitempty"`
	// MountLabel is the SELinux mount label for the container's root
	// filesystem. Only used if the container was created from an image.
	// If not explicitly set, an unused random MLS label will be assigned by
	// containers/storage (but only if SELinux is enabled).
	MountLabel string `json:"MountLabel,omitempty"`
	// LabelOpts are options passed in by the user to set up SELinux labels.
	// These are used by the containers/storage library.
	LabelOpts []string `json:"labelopts,omitempty"`
	// User and group to use in the container. Can be specified as only user
	// (in which case we will attempt to look up the user in the container
	// to determine the appropriate group) or user and group separated by a
	// colon.
	// Can be specified by name or UID/GID.
	// If unset, this will default to UID and GID 0 (root).
	User string `json:"user,omitempty"`
	// Groups are additional groups to add the container's user to. These
	// are resolved within the container using the container's /etc/passwd.
	Groups []string `json:"groups,omitempty"`
	// HostUsers are a list of host user accounts to add to /etc/passwd
	HostUsers []string `json:"HostUsers,omitempty"`
	// AddCurrentUserPasswdEntry indicates that Libpod should ensure that
	// the container's /etc/passwd contains an entry for the user running
	// Libpod - mostly used in rootless containers where the user running
	// Libpod wants to retain their UID inside the container.
	AddCurrentUserPasswdEntry bool `json:"addCurrentUserPasswdEntry,omitempty"`
	// LabelNested, allow labeling separation from within a container
	LabelNested bool `json:"label_nested"`
}

type ContainerNameSpaceConfig struct {
	// IDs of container to share namespaces with
	// NetNsCtr conflicts with the CreateNetNS bool
	// These containers are considered dependencies of the given container
	// They must be started before the given container is started
	IPCNsCtr    string `json:"ipcNsCtr,omitempty"`
	MountNsCtr  string `json:"mountNsCtr,omitempty"`
	NetNsCtr    string `json:"netNsCtr,omitempty"`
	PIDNsCtr    string `json:"pidNsCtr,omitempty"`
	UserNsCtr   string `json:"userNsCtr,omitempty"`
	UTSNsCtr    string `json:"utsNsCtr,omitempty"`
	CgroupNsCtr string `json:"cgroupNsCtr,omitempty"`
}

type IDMapping struct {
	// UIDMap and GIDMap are used for setting up a layer's root filesystem
	// for use inside of a user namespace where ID mapping is being used.
	// If HostUIDMapping/HostGIDMapping is true, no mapping of the
	// respective type will be used.  Otherwise, if UIDMap and/or GIDMap
	// contain at least one mapping, one or both will be used.  By default,
	// if neither of those conditions apply, if the layer has a parent
	// layer, the parent layer's mapping will be used, and if it does not
	// have a parent layer, the mapping which was passed to the Store
	// object when it was initialized will be used.
	HostUIDMapping bool
	HostGIDMapping bool
	UIDMap         []idtools.IDMap
	GIDMap         []idtools.IDMap
	AutoUserNs     bool
	AutoUserNsOpts AutoUserNsOptions
}

type IDMappingOptions = IDMapping
type AutoUserNsOptions struct {
	// Size defines the size for the user namespace.  If it is set to a
	// value bigger than 0, the user namespace will have exactly this size.
	// If it is not set, some heuristics will be used to find its size.
	Size uint32
	// InitialSize defines the minimum size for the user namespace.
	// The created user namespace will have at least this size.
	InitialSize uint32
	// PasswdFile to use if the container uses a volume.
	PasswdFile string
	// GroupFile to use if the container uses a volume.
	GroupFile string
	// AdditionalUIDMappings specified additional UID mappings to include in
	// the generated user namespace.
	AdditionalUIDMappings []idtools.IDMap
	// AdditionalGIDMappings specified additional GID mappings to include in
	// the generated user namespace.
	AdditionalGIDMappings []idtools.IDMap
}

type ContainerRootFSConfig struct {
	// RootfsImageID is the ID of the image used to create the container.
	// If the container was created from a Rootfs, this will be empty.
	// If non-empty, Podman will create a root filesystem for the container
	// based on an image with this ID.
	// This conflicts with Rootfs.
	RootfsImageID string `json:"rootfsImageID,omitempty"`
	// RootfsImageName is the (normalized) name of the image used to create
	// the container. If the container was created from a Rootfs, this will
	// be empty.
	RootfsImageName string `json:"rootfsImageName,omitempty"`
	// Rootfs is a directory to use as the container's root filesystem.
	// If RootfsImageID is set, this will be empty.
	// If this is set, Podman will not create a root filesystem for the
	// container based on an image, and will instead use the given directory
	// as the container's root.
	// Conflicts with RootfsImageID.
	Rootfs string `json:"rootfs,omitempty"`
	// RootfsOverlay tells if rootfs has to be mounted as an overlay
	RootfsOverlay bool `json:"rootfs_overlay,omitempty"`
	// RootfsMapping specifies if there are mappings to apply to the rootfs.
	RootfsMapping *string `json:"rootfs_mapping,omitempty"`
	// ShmDir is the path to be mounted on /dev/shm in container.
	// If not set manually at creation time, Libpod will create a tmpfs
	// with the size specified in ShmSize and populate this with the path of
	// said tmpfs.
	ShmDir string `json:"ShmDir,omitempty"`
	// NoShmShare indicates whether /dev/shm can be shared with other containers
	NoShmShare bool `json:"NOShmShare,omitempty"`
	// NoShm indicates whether a tmpfs should be created and mounted on  /dev/shm
	NoShm bool `json:"NoShm,omitempty"`
	// ShmSize is the size of the container's SHM. Only used if ShmDir was
	// not set manually at time of creation.
	ShmSize int64 `json:"shmSize"`
	// ShmSizeSystemd is the size of systemd-specific tmpfs mounts
	ShmSizeSystemd int64 `json:"shmSizeSystemd"`
	// Static directory for container content that will persist across
	// reboot.
	// StaticDir is a persistent directory for Libpod files that will
	// survive system reboot. It is not part of the container's rootfs and
	// is not mounted into the container. It will be removed when the
	// container is removed.
	// Usually used to store container log files, files that will be bind
	// mounted into the container (e.g. the resolv.conf we made for the
	// container), and other per-container content.
	StaticDir string `json:"staticDir"`
	// Mounts contains all additional mounts into the container rootfs.
	// It is presently only used for the container's SHM directory.
	// These must be unmounted before the container's rootfs is unmounted.
	Mounts []string `json:"mounts,omitempty"`
	// NamedVolumes lists the Libpod named volumes to mount into the
	// container. Each named volume is guaranteed to exist so long as this
	// container exists.
	NamedVolumes []*ContainerNamedVolume `json:"namedVolumes,omitempty"`
	// OverlayVolumes lists the overlay volumes to mount into the container.
	OverlayVolumes []*ContainerOverlayVolume `json:"overlayVolumes,omitempty"`
	// ImageVolumes lists the image volumes to mount into the container.
	// Please note that this is named ctrImageVolumes in JSON to
	// distinguish between these and the old `imageVolumes` field in Podman
	// pre-1.8, which was used in very old Podman versions to determine how
	// image volumes were handled in Libpod (support for these eventually
	// moved out of Libpod into pkg/specgen).
	// Please DO NOT re-use the `imageVolumes` name in container JSON again.
	ImageVolumes []*ContainerImageVolume `json:"ctrImageVolumes,omitempty"`
	// CreateWorkingDir indicates that Libpod should create the container's
	// working directory if it does not exist. Some OCI runtimes do this by
	// default, but others do not.
	CreateWorkingDir bool `json:"createWorkingDir,omitempty"`
	// Secrets lists secrets to mount into the container
	Secrets []*ContainerSecret `json:"secrets,omitempty"`
	// SecretPath is the secrets location in storage
	SecretsPath string `json:"secretsPath"`
	// StorageOpts to be used when creating rootfs
	StorageOpts map[string]string `json:"storageOpts"`
	// Volatile specifies whether the container storage can be optimized
	// at the cost of not syncing all the dirty files in memory.
	Volatile bool `json:"volatile,omitempty"`
	// Passwd allows to user to override podman's passwd/group file setup
	Passwd *bool `json:"passwd,omitempty"`
	// ChrootDirs is an additional set of directories that need to be
	// treated as root directories. Standard bind mounts will be mounted
	// into paths relative to these directories.
	ChrootDirs []string `json:"chroot_directories,omitempty"`
}

type Secret struct {
	// Name is the name of the secret
	Name string `json:"name"`
	// ID is the unique secret ID
	ID string `json:"id"`
	// Labels are labels on the secret
	Labels map[string]string `json:"labels,omitempty"`
	// Metadata stores other metadata on the secret
	Metadata map[string]string `json:"metadata,omitempty"`
	// CreatedAt is when the secret was created
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is when the secret was updated
	UpdatedAt time.Time `json:"updatedAt"`
	// Driver is the driver used to store secret data
	Driver string `json:"driver"`
	// DriverOptions are extra options used to run this driver
	DriverOptions map[string]string `json:"driverOptions"`
}

type ContainerSecret struct {
	// Secret is the secret
	*Secret
	// UID is the UID of the secret file
	UID uint32
	// GID is the GID of the secret file
	GID uint32
	// Mode is the mode of the secret file
	Mode uint32
	// Secret target inside container
	Target string
}

type ContainerImageVolume struct {
	// Source is the source of the image volume.  The image can be referred
	// to by name and by ID.
	Source string `json:"source"`
	// Dest is the absolute path of the mount in the container.
	Dest string `json:"dest"`
	// ReadWrite sets the volume writable.
	ReadWrite bool `json:"rw"`
}

type ContainerOverlayVolume struct {
	// Destination is the absolute path where the mount will be placed in the container.
	Dest string `json:"dest"`
	// Source specifies the source path of the mount.
	Source string `json:"source,omitempty"`
	// Options holds overlay volume options.
	Options []string `json:"options,omitempty"`
}

type ContainerNamedVolume struct {
	// Name is the name of the volume to mount in.
	// Must resolve to a valid volume present in this Podman.
	Name string `json:"volumeName"`
	// Dest is the mount's destination
	Dest string `json:"dest"`
	// Options are fstab style mount options
	Options []string `json:"options,omitempty"`
	// IsAnonymous sets the named volume as anonymous even if it has a name
	// This is used for emptyDir volumes from a kube yaml
	IsAnonymous bool `json:"setAnonymous,omitempty"`
	// SubPath determines which part of the Source will be mounted in the container
	SubPath string
}
type PerNetworkOptions struct {
	// StaticIPs for this container. Optional.
	// swagger:type []string
	StaticIPs []net.IP `json:"static_ips,omitempty"`
	// Aliases contains a list of names which the dns server should resolve
	// to this container. Should only be set when DNSEnabled is true on the Network.
	// If aliases are set but there is no dns support for this network the
	// network interface implementation should ignore this and NOT error.
	// Optional.
	Aliases []string `json:"aliases,omitempty"`
	// StaticMac for this container. Optional.
	// swagger:strfmt string
	StaticMAC HardwareAddr `json:"static_mac,omitempty"`
	// InterfaceName for this container. Required in the backend.
	// Optional in the frontend. Will be filled with ethX (where X is a integer) when empty.
	InterfaceName string `json:"interface_name"`
}

type ContainerState struct {
	// The current state of the running container
	State ContainerStatus `json:"state"`
	// The path to the JSON OCI runtime spec for this container
	ConfigPath string `json:"configPath,omitempty"`
	// RunDir is a per-boot directory for container content
	RunDir string `json:"runDir,omitempty"`
	// Mounted indicates whether the container's storage has been mounted
	// for use
	Mounted bool `json:"mounted,omitempty"`
	// Mountpoint contains the path to the container's mounted storage as given
	// by containers/storage.
	Mountpoint string `json:"mountPoint,omitempty"`
	// StartedTime is the time the container was started
	StartedTime time.Time `json:"startedTime,omitempty"`
	// FinishedTime is the time the container finished executing
	FinishedTime time.Time `json:"finishedTime,omitempty"`
	// ExitCode is the exit code returned when the container stopped
	ExitCode int32 `json:"exitCode,omitempty"`
	// Exited is whether the container has exited
	Exited bool `json:"exited,omitempty"`
	// Error holds the last known error message during start, stop, or remove
	Error string `json:"error,omitempty"`
	// OOMKilled indicates that the container was killed as it ran out of
	// memory
	OOMKilled bool `json:"oomKilled,omitempty"`
	// Checkpointed indicates that the container was stopped by a checkpoint
	// operation.
	Checkpointed bool `json:"checkpointed,omitempty"`
	// PID is the PID of a running container
	PID int `json:"pid,omitempty"`
	// ConmonPID is the PID of the container's conmon
	ConmonPID int `json:"conmonPid,omitempty"`
	// ExecSessions contains all exec sessions that are associated with this
	// container.
	// LegacyExecSessions are legacy exec sessions from older versions of
	// Podman.
	// These are DEPRECATED and will be removed in a future release.
	// NetNS is the path or name of the NetNS
	NetNS string `json:"netns,omitempty"`
	// NetworkStatusOld contains the configuration results for all networks
	// the pod is attached to. Only populated if we created a network
	// namespace for the container, and the network namespace is currently
	// active.
	// These are DEPRECATED and will be removed in a future release.
	// This field is only used for backwarts compatibility.
	// NetworkStatus contains the network Status for all networks
	// the container is attached to. Only populated if we created a network
	// namespace for the container, and the network namespace is currently
	// active.
	// To read this field use container.getNetworkStatus() instead, this will
	// take care of migrating the old DEPRECATED network status to the new format.
	NetworkStatus map[string]StatusBlock `json:"networkStatus,omitempty"`
	// BindMounts contains files that will be bind-mounted into the
	// container when it is mounted.
	// These include /etc/hosts and /etc/resolv.conf
	// This maps the path the file will be mounted to in the container to
	// the path of the file on disk outside the container
	BindMounts map[string]string `json:"bindMounts,omitempty"`
	// StoppedByUser indicates whether the container was stopped by an
	// explicit call to the Stop() API.
	StoppedByUser bool `json:"stoppedByUser,omitempty"`
	// RestartPolicyMatch indicates whether the conditions for restart
	// policy have been met.
	RestartPolicyMatch bool `json:"restartPolicyMatch,omitempty"`
	// RestartCount is how many times the container was restarted by its
	// restart policy. This is NOT incremented by normal container restarts
	// (only by restart policy).
	RestartCount uint `json:"restartCount,omitempty"`
	// StartupHCPassed indicates that the startup healthcheck has
	// succeeded and the main healthcheck can begin.
	StartupHCPassed bool `json:"startupHCPassed,omitempty"`
	// StartupHCSuccessCount indicates the number of successes of the
	// startup healthcheck. A startup HC can require more than one success
	// to be marked as passed.
	StartupHCSuccessCount int `json:"startupHCSuccessCount,omitempty"`
	// StartupHCFailureCount indicates the number of failures of the startup
	// healthcheck. The container will be restarted if this exceed a set
	// number in the startup HC config.
	StartupHCFailureCount int `json:"startupHCFailureCount,omitempty"`

	// ExtensionStageHooks holds hooks which will be executed by libpod
	// and not delegated to the OCI runtime.
	ExtensionStageHooks map[string][]spec.Hook `json:"extensionStageHooks,omitempty"`

	// NetInterfaceDescriptions describe the relationship between a CNI
	// network and an interface names

	// Service indicates that container is the service container of a
	// service. A service consists of one or more pods.  The service
	// container is started before all pods and is stopped when the last
	// pod stops. The service container allows for tracking and managing
	// the entire life cycle of service which may be started via
	// `podman-play-kube`.

	// Following checkpoint/restore related information is displayed
	// if the container has been checkpointed or restored.
	CheckpointedTime time.Time `json:"checkpointedTime,omitempty"`
	RestoredTime     time.Time `json:"restoredTime,omitempty"`
	CheckpointLog    string    `json:"checkpointLog,omitempty"`
	CheckpointPath   string    `json:"checkpointPath,omitempty"`
	RestoreLog       string    `json:"restoreLog,omitempty"`
	Restored         bool      `json:"restored,omitempty"`
}

type RuntimeContainerMetadata struct {
	// The provided name and the ID of the image that was used to
	// instantiate the container.
	ImageName string `json:"image-name"` // Applicable to both PodSandboxes and Containers
	ImageID   string `json:"image-id"`   // Applicable to both PodSandboxes and Containers
	// The container's name, which for an infrastructure container is usually PodName + "-infra".
	ContainerName string `json:"name"`                 // Applicable to both PodSandboxes and Containers, mandatory
	CreatedAt     int64  `json:"created-at"`           // Applicable to both PodSandboxes and Containers
	MountLabel    string `json:"mountlabel,omitempty"` // Applicable to both PodSandboxes and Containers
}

type RestoreOptions struct {
	All             bool
	IgnoreRootFS    bool
	IgnoreVolumes   bool
	IgnoreStaticIP  bool
	IgnoreStaticMAC bool
	Import          string
	CheckpointImage bool
	Keep            bool
	Latest          bool
	Name            string
	TCPEstablished  bool
	ImportPrevious  string
	PublishPorts    []string
	Pod             string
	PrintStats      bool
	FileLocks       bool
}
