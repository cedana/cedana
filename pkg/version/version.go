package version

var Version = "dev"

func PutVersion(version string) {
	Version = version
}

func GetVersion() string {
	return Version
}
