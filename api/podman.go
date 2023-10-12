package api

import (
	"fmt"

	bolt "go.etcd.io/bbolt"
)

const (
	idRegistryName    = "id-registry"
	nameRegistryName  = "name-registry"
	ctrName           = "ctr"
	allCtrsName       = "all-ctrs"
	podName           = "pod"
	allPodsName       = "allPods"
	volName           = "vol"
	allVolsName       = "allVolumes"
	execName          = "exec"
	aliasesName       = "aliases"
	runtimeConfigName = "runtime-config"
	volumeCtrsName    = "volume-ctrs"

	exitCodeName          = "exit-code"
	exitCodeTimeStampName = "exit-code-time-stamp"

	configName         = "config"
	stateName          = "state"
	dependenciesName   = "dependencies"
	volCtrDependencies = "vol-dependencies"
	netNSName          = "netns"
	containersName     = "containers"
	podIDName          = "pod-id"
	networksName       = "networks"

	staticDirName   = "static-dir"
	tmpDirName      = "tmp-dir"
	runRootName     = "run-root"
	graphRootName   = "graph-root"
	graphDriverName = "graph-driver-name"
	osName          = "os"
	volPathName     = "volume-path"
)

var (
	idRegistryBkt      = []byte(idRegistryName)
	nameRegistryBkt    = []byte(nameRegistryName)
	ctrBkt             = []byte(ctrName)
	allCtrsBkt         = []byte(allCtrsName)
	podBkt             = []byte(podName)
	allPodsBkt         = []byte(allPodsName)
	volBkt             = []byte(volName)
	allVolsBkt         = []byte(allVolsName)
	execBkt            = []byte(execName)
	aliasesBkt         = []byte(aliasesName)
	runtimeConfigBkt   = []byte(runtimeConfigName)
	dependenciesBkt    = []byte(dependenciesName)
	volDependenciesBkt = []byte(volCtrDependencies)
	networksBkt        = []byte(networksName)
	volCtrsBkt         = []byte(volumeCtrsName)

	exitCodeBkt          = []byte(exitCodeName)
	exitCodeTimeStampBkt = []byte(exitCodeTimeStampName)

	configKey     = []byte(configName)
	stateKey      = []byte(stateName)
	netNSKey      = []byte(netNSName)
	containersBkt = []byte(containersName)
	podIDKey      = []byte(podIDName)

	staticDirKey   = []byte(staticDirName)
	tmpDirKey      = []byte(tmpDirName)
	runRootKey     = []byte(runRootName)
	graphRootKey   = []byte(graphRootName)
	graphDriverKey = []byte(graphDriverName)
	osKey          = []byte(osName)
	volPathKey     = []byte(volPathName)
)

func getIDBucket(tx *bolt.Tx) (*bolt.Bucket, error) {
	bkt := tx.Bucket(idRegistryBkt)
	if bkt == nil {
		return nil, fmt.Errorf("id registry bucket not found in DB")
	}
	return bkt, nil
}

func getNamesBucket(tx *bolt.Tx) (*bolt.Bucket, error) {
	bkt := tx.Bucket(nameRegistryBkt)
	if bkt == nil {
		return nil, fmt.Errorf("name registry bucket not found in DB")
	}
	return bkt, nil
}

func getCtrBucket(tx *bolt.Tx) (*bolt.Bucket, error) {
	bkt := tx.Bucket(ctrBkt)
	if bkt == nil {
		return nil, fmt.Errorf("containers bucket not found in DB")
	}
	return bkt, nil
}

func getAllCtrsBucket(tx *bolt.Tx) (*bolt.Bucket, error) {
	bkt := tx.Bucket(allCtrsBkt)
	if bkt == nil {
		return nil, fmt.Errorf("all containers bucket not found in DB")
	}
	return bkt, nil
}

func getVolBucket(tx *bolt.Tx) (*bolt.Bucket, error) {
	bkt := tx.Bucket(volBkt)
	if bkt == nil {
		return nil, fmt.Errorf("volumes bucket not found in DB")
	}
	return bkt, nil
}
