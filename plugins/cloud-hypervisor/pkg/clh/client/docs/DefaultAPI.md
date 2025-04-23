# \DefaultAPI

All URIs are relative to *http://localhost/api/v1*

Method | HTTP request | Description
------------- | ------------- | -------------
[**BootVM**](DefaultAPI.md#BootVM) | **Put** /vm.boot | Boot the previously created VM instance.
[**CreateVM**](DefaultAPI.md#CreateVM) | **Put** /vm.create | Create the cloud-hypervisor Virtual Machine (VM) instance. The instance is not booted, only created.
[**DeleteVM**](DefaultAPI.md#DeleteVM) | **Put** /vm.delete | Delete the cloud-hypervisor Virtual Machine (VM) instance.
[**PauseVM**](DefaultAPI.md#PauseVM) | **Put** /vm.pause | Pause a previously booted VM instance.
[**PowerButtonVM**](DefaultAPI.md#PowerButtonVM) | **Put** /vm.power-button | Trigger a power button in the VM
[**RebootVM**](DefaultAPI.md#RebootVM) | **Put** /vm.reboot | Reboot the VM instance.
[**ResumeVM**](DefaultAPI.md#ResumeVM) | **Put** /vm.resume | Resume a previously paused VM instance.
[**ShutdownVM**](DefaultAPI.md#ShutdownVM) | **Put** /vm.shutdown | Shut the VM instance down.
[**ShutdownVMM**](DefaultAPI.md#ShutdownVMM) | **Put** /vmm.shutdown | Shuts the cloud-hypervisor VMM.
[**VmAddDevicePut**](DefaultAPI.md#VmAddDevicePut) | **Put** /vm.add-device | Add a new device to the VM
[**VmAddDiskPut**](DefaultAPI.md#VmAddDiskPut) | **Put** /vm.add-disk | Add a new disk to the VM
[**VmAddFsPut**](DefaultAPI.md#VmAddFsPut) | **Put** /vm.add-fs | Add a new virtio-fs device to the VM
[**VmAddNetPut**](DefaultAPI.md#VmAddNetPut) | **Put** /vm.add-net | Add a new network device to the VM
[**VmAddPmemPut**](DefaultAPI.md#VmAddPmemPut) | **Put** /vm.add-pmem | Add a new pmem device to the VM
[**VmAddUserDevicePut**](DefaultAPI.md#VmAddUserDevicePut) | **Put** /vm.add-user-device | Add a new userspace device to the VM
[**VmAddVdpaPut**](DefaultAPI.md#VmAddVdpaPut) | **Put** /vm.add-vdpa | Add a new vDPA device to the VM
[**VmAddVsockPut**](DefaultAPI.md#VmAddVsockPut) | **Put** /vm.add-vsock | Add a new vsock device to the VM
[**VmCoredumpPut**](DefaultAPI.md#VmCoredumpPut) | **Put** /vm.coredump | Takes a VM coredump.
[**VmCountersGet**](DefaultAPI.md#VmCountersGet) | **Get** /vm.counters | Get counters from the VM
[**VmInfoGet**](DefaultAPI.md#VmInfoGet) | **Get** /vm.info | Returns general information about the cloud-hypervisor Virtual Machine (VM) instance.
[**VmReceiveMigrationPut**](DefaultAPI.md#VmReceiveMigrationPut) | **Put** /vm.receive-migration | Receive a VM migration from URL
[**VmRemoveDevicePut**](DefaultAPI.md#VmRemoveDevicePut) | **Put** /vm.remove-device | Remove a device from the VM
[**VmResizePut**](DefaultAPI.md#VmResizePut) | **Put** /vm.resize | Resize the VM
[**VmResizeZonePut**](DefaultAPI.md#VmResizeZonePut) | **Put** /vm.resize-zone | Resize a memory zone
[**VmRestorePut**](DefaultAPI.md#VmRestorePut) | **Put** /vm.restore | Restore a VM from a snapshot.
[**VmSendMigrationPut**](DefaultAPI.md#VmSendMigrationPut) | **Put** /vm.send-migration | Send a VM migration to URL
[**VmSnapshotPut**](DefaultAPI.md#VmSnapshotPut) | **Put** /vm.snapshot | Returns a VM snapshot.
[**VmmNmiPut**](DefaultAPI.md#VmmNmiPut) | **Put** /vmm.nmi | Inject an NMI.
[**VmmPingGet**](DefaultAPI.md#VmmPingGet) | **Get** /vmm.ping | Ping the VMM to check for API server availability



## BootVM

> BootVM(ctx).Execute()

Boot the previously created VM instance.

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	r, err := apiClient.DefaultAPI.BootVM(context.Background()).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.BootVM``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
}
```

### Path Parameters

This endpoint does not need any parameter.

### Other Parameters

Other parameters are passed through a pointer to a apiBootVMRequest struct via the builder pattern


### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## CreateVM

> CreateVM(ctx).VmConfig(vmConfig).Execute()

Create the cloud-hypervisor Virtual Machine (VM) instance. The instance is not booted, only created.

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {
	vmConfig := *openapiclient.NewVmConfig(*openapiclient.NewPayloadConfig()) // VmConfig | The VM configuration

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	r, err := apiClient.DefaultAPI.CreateVM(context.Background()).VmConfig(vmConfig).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.CreateVM``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
}
```

### Path Parameters



### Other Parameters

Other parameters are passed through a pointer to a apiCreateVMRequest struct via the builder pattern


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **vmConfig** | [**VmConfig**](VmConfig.md) | The VM configuration | 

### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## DeleteVM

> DeleteVM(ctx).Execute()

Delete the cloud-hypervisor Virtual Machine (VM) instance.

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	r, err := apiClient.DefaultAPI.DeleteVM(context.Background()).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.DeleteVM``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
}
```

### Path Parameters

This endpoint does not need any parameter.

### Other Parameters

Other parameters are passed through a pointer to a apiDeleteVMRequest struct via the builder pattern


### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## PauseVM

> PauseVM(ctx).Execute()

Pause a previously booted VM instance.

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	r, err := apiClient.DefaultAPI.PauseVM(context.Background()).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.PauseVM``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
}
```

### Path Parameters

This endpoint does not need any parameter.

### Other Parameters

Other parameters are passed through a pointer to a apiPauseVMRequest struct via the builder pattern


### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## PowerButtonVM

> PowerButtonVM(ctx).Execute()

Trigger a power button in the VM

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	r, err := apiClient.DefaultAPI.PowerButtonVM(context.Background()).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.PowerButtonVM``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
}
```

### Path Parameters

This endpoint does not need any parameter.

### Other Parameters

Other parameters are passed through a pointer to a apiPowerButtonVMRequest struct via the builder pattern


### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## RebootVM

> RebootVM(ctx).Execute()

Reboot the VM instance.

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	r, err := apiClient.DefaultAPI.RebootVM(context.Background()).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.RebootVM``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
}
```

### Path Parameters

This endpoint does not need any parameter.

### Other Parameters

Other parameters are passed through a pointer to a apiRebootVMRequest struct via the builder pattern


### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## ResumeVM

> ResumeVM(ctx).Execute()

Resume a previously paused VM instance.

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	r, err := apiClient.DefaultAPI.ResumeVM(context.Background()).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.ResumeVM``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
}
```

### Path Parameters

This endpoint does not need any parameter.

### Other Parameters

Other parameters are passed through a pointer to a apiResumeVMRequest struct via the builder pattern


### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## ShutdownVM

> ShutdownVM(ctx).Execute()

Shut the VM instance down.

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	r, err := apiClient.DefaultAPI.ShutdownVM(context.Background()).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.ShutdownVM``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
}
```

### Path Parameters

This endpoint does not need any parameter.

### Other Parameters

Other parameters are passed through a pointer to a apiShutdownVMRequest struct via the builder pattern


### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## ShutdownVMM

> ShutdownVMM(ctx).Execute()

Shuts the cloud-hypervisor VMM.

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	r, err := apiClient.DefaultAPI.ShutdownVMM(context.Background()).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.ShutdownVMM``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
}
```

### Path Parameters

This endpoint does not need any parameter.

### Other Parameters

Other parameters are passed through a pointer to a apiShutdownVMMRequest struct via the builder pattern


### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## VmAddDevicePut

> PciDeviceInfo VmAddDevicePut(ctx).DeviceConfig(deviceConfig).Execute()

Add a new device to the VM

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {
	deviceConfig := *openapiclient.NewDeviceConfig("Path_example") // DeviceConfig | The path of the new device

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	resp, r, err := apiClient.DefaultAPI.VmAddDevicePut(context.Background()).DeviceConfig(deviceConfig).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.VmAddDevicePut``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
	// response from `VmAddDevicePut`: PciDeviceInfo
	fmt.Fprintf(os.Stdout, "Response from `DefaultAPI.VmAddDevicePut`: %v\n", resp)
}
```

### Path Parameters



### Other Parameters

Other parameters are passed through a pointer to a apiVmAddDevicePutRequest struct via the builder pattern


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **deviceConfig** | [**DeviceConfig**](DeviceConfig.md) | The path of the new device | 

### Return type

[**PciDeviceInfo**](PciDeviceInfo.md)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## VmAddDiskPut

> PciDeviceInfo VmAddDiskPut(ctx).DiskConfig(diskConfig).Execute()

Add a new disk to the VM

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {
	diskConfig := *openapiclient.NewDiskConfig("Path_example") // DiskConfig | The details of the new disk

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	resp, r, err := apiClient.DefaultAPI.VmAddDiskPut(context.Background()).DiskConfig(diskConfig).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.VmAddDiskPut``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
	// response from `VmAddDiskPut`: PciDeviceInfo
	fmt.Fprintf(os.Stdout, "Response from `DefaultAPI.VmAddDiskPut`: %v\n", resp)
}
```

### Path Parameters



### Other Parameters

Other parameters are passed through a pointer to a apiVmAddDiskPutRequest struct via the builder pattern


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **diskConfig** | [**DiskConfig**](DiskConfig.md) | The details of the new disk | 

### Return type

[**PciDeviceInfo**](PciDeviceInfo.md)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## VmAddFsPut

> PciDeviceInfo VmAddFsPut(ctx).FsConfig(fsConfig).Execute()

Add a new virtio-fs device to the VM

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {
	fsConfig := *openapiclient.NewFsConfig("Tag_example", "Socket_example", int32(123), int32(123)) // FsConfig | The details of the new virtio-fs

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	resp, r, err := apiClient.DefaultAPI.VmAddFsPut(context.Background()).FsConfig(fsConfig).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.VmAddFsPut``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
	// response from `VmAddFsPut`: PciDeviceInfo
	fmt.Fprintf(os.Stdout, "Response from `DefaultAPI.VmAddFsPut`: %v\n", resp)
}
```

### Path Parameters



### Other Parameters

Other parameters are passed through a pointer to a apiVmAddFsPutRequest struct via the builder pattern


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **fsConfig** | [**FsConfig**](FsConfig.md) | The details of the new virtio-fs | 

### Return type

[**PciDeviceInfo**](PciDeviceInfo.md)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## VmAddNetPut

> PciDeviceInfo VmAddNetPut(ctx).NetConfig(netConfig).Execute()

Add a new network device to the VM

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {
	netConfig := *openapiclient.NewNetConfig() // NetConfig | The details of the new network device

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	resp, r, err := apiClient.DefaultAPI.VmAddNetPut(context.Background()).NetConfig(netConfig).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.VmAddNetPut``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
	// response from `VmAddNetPut`: PciDeviceInfo
	fmt.Fprintf(os.Stdout, "Response from `DefaultAPI.VmAddNetPut`: %v\n", resp)
}
```

### Path Parameters



### Other Parameters

Other parameters are passed through a pointer to a apiVmAddNetPutRequest struct via the builder pattern


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **netConfig** | [**NetConfig**](NetConfig.md) | The details of the new network device | 

### Return type

[**PciDeviceInfo**](PciDeviceInfo.md)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## VmAddPmemPut

> PciDeviceInfo VmAddPmemPut(ctx).PmemConfig(pmemConfig).Execute()

Add a new pmem device to the VM

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {
	pmemConfig := *openapiclient.NewPmemConfig("File_example") // PmemConfig | The details of the new pmem device

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	resp, r, err := apiClient.DefaultAPI.VmAddPmemPut(context.Background()).PmemConfig(pmemConfig).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.VmAddPmemPut``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
	// response from `VmAddPmemPut`: PciDeviceInfo
	fmt.Fprintf(os.Stdout, "Response from `DefaultAPI.VmAddPmemPut`: %v\n", resp)
}
```

### Path Parameters



### Other Parameters

Other parameters are passed through a pointer to a apiVmAddPmemPutRequest struct via the builder pattern


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **pmemConfig** | [**PmemConfig**](PmemConfig.md) | The details of the new pmem device | 

### Return type

[**PciDeviceInfo**](PciDeviceInfo.md)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## VmAddUserDevicePut

> PciDeviceInfo VmAddUserDevicePut(ctx).VmAddUserDevice(vmAddUserDevice).Execute()

Add a new userspace device to the VM

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {
	vmAddUserDevice := *openapiclient.NewVmAddUserDevice("Socket_example") // VmAddUserDevice | The path of the new device

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	resp, r, err := apiClient.DefaultAPI.VmAddUserDevicePut(context.Background()).VmAddUserDevice(vmAddUserDevice).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.VmAddUserDevicePut``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
	// response from `VmAddUserDevicePut`: PciDeviceInfo
	fmt.Fprintf(os.Stdout, "Response from `DefaultAPI.VmAddUserDevicePut`: %v\n", resp)
}
```

### Path Parameters



### Other Parameters

Other parameters are passed through a pointer to a apiVmAddUserDevicePutRequest struct via the builder pattern


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **vmAddUserDevice** | [**VmAddUserDevice**](VmAddUserDevice.md) | The path of the new device | 

### Return type

[**PciDeviceInfo**](PciDeviceInfo.md)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## VmAddVdpaPut

> PciDeviceInfo VmAddVdpaPut(ctx).VdpaConfig(vdpaConfig).Execute()

Add a new vDPA device to the VM

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {
	vdpaConfig := *openapiclient.NewVdpaConfig("Path_example", int32(123)) // VdpaConfig | The details of the new vDPA device

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	resp, r, err := apiClient.DefaultAPI.VmAddVdpaPut(context.Background()).VdpaConfig(vdpaConfig).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.VmAddVdpaPut``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
	// response from `VmAddVdpaPut`: PciDeviceInfo
	fmt.Fprintf(os.Stdout, "Response from `DefaultAPI.VmAddVdpaPut`: %v\n", resp)
}
```

### Path Parameters



### Other Parameters

Other parameters are passed through a pointer to a apiVmAddVdpaPutRequest struct via the builder pattern


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **vdpaConfig** | [**VdpaConfig**](VdpaConfig.md) | The details of the new vDPA device | 

### Return type

[**PciDeviceInfo**](PciDeviceInfo.md)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## VmAddVsockPut

> PciDeviceInfo VmAddVsockPut(ctx).VsockConfig(vsockConfig).Execute()

Add a new vsock device to the VM

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {
	vsockConfig := *openapiclient.NewVsockConfig(int64(123), "Socket_example") // VsockConfig | The details of the new vsock device

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	resp, r, err := apiClient.DefaultAPI.VmAddVsockPut(context.Background()).VsockConfig(vsockConfig).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.VmAddVsockPut``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
	// response from `VmAddVsockPut`: PciDeviceInfo
	fmt.Fprintf(os.Stdout, "Response from `DefaultAPI.VmAddVsockPut`: %v\n", resp)
}
```

### Path Parameters



### Other Parameters

Other parameters are passed through a pointer to a apiVmAddVsockPutRequest struct via the builder pattern


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **vsockConfig** | [**VsockConfig**](VsockConfig.md) | The details of the new vsock device | 

### Return type

[**PciDeviceInfo**](PciDeviceInfo.md)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## VmCoredumpPut

> VmCoredumpPut(ctx).VmCoredumpData(vmCoredumpData).Execute()

Takes a VM coredump.

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {
	vmCoredumpData := *openapiclient.NewVmCoredumpData() // VmCoredumpData | The coredump configuration

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	r, err := apiClient.DefaultAPI.VmCoredumpPut(context.Background()).VmCoredumpData(vmCoredumpData).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.VmCoredumpPut``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
}
```

### Path Parameters



### Other Parameters

Other parameters are passed through a pointer to a apiVmCoredumpPutRequest struct via the builder pattern


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **vmCoredumpData** | [**VmCoredumpData**](VmCoredumpData.md) | The coredump configuration | 

### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## VmCountersGet

> map[string]map[string]int64 VmCountersGet(ctx).Execute()

Get counters from the VM

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	resp, r, err := apiClient.DefaultAPI.VmCountersGet(context.Background()).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.VmCountersGet``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
	// response from `VmCountersGet`: map[string]map[string]int64
	fmt.Fprintf(os.Stdout, "Response from `DefaultAPI.VmCountersGet`: %v\n", resp)
}
```

### Path Parameters

This endpoint does not need any parameter.

### Other Parameters

Other parameters are passed through a pointer to a apiVmCountersGetRequest struct via the builder pattern


### Return type

[**map[string]map[string]int64**](map.md)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## VmInfoGet

> VmInfo VmInfoGet(ctx).Execute()

Returns general information about the cloud-hypervisor Virtual Machine (VM) instance.

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	resp, r, err := apiClient.DefaultAPI.VmInfoGet(context.Background()).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.VmInfoGet``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
	// response from `VmInfoGet`: VmInfo
	fmt.Fprintf(os.Stdout, "Response from `DefaultAPI.VmInfoGet`: %v\n", resp)
}
```

### Path Parameters

This endpoint does not need any parameter.

### Other Parameters

Other parameters are passed through a pointer to a apiVmInfoGetRequest struct via the builder pattern


### Return type

[**VmInfo**](VmInfo.md)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## VmReceiveMigrationPut

> VmReceiveMigrationPut(ctx).ReceiveMigrationData(receiveMigrationData).Execute()

Receive a VM migration from URL

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {
	receiveMigrationData := *openapiclient.NewReceiveMigrationData("ReceiverUrl_example") // ReceiveMigrationData | The URL for the reception of migration state

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	r, err := apiClient.DefaultAPI.VmReceiveMigrationPut(context.Background()).ReceiveMigrationData(receiveMigrationData).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.VmReceiveMigrationPut``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
}
```

### Path Parameters



### Other Parameters

Other parameters are passed through a pointer to a apiVmReceiveMigrationPutRequest struct via the builder pattern


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **receiveMigrationData** | [**ReceiveMigrationData**](ReceiveMigrationData.md) | The URL for the reception of migration state | 

### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## VmRemoveDevicePut

> VmRemoveDevicePut(ctx).VmRemoveDevice(vmRemoveDevice).Execute()

Remove a device from the VM

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {
	vmRemoveDevice := *openapiclient.NewVmRemoveDevice() // VmRemoveDevice | The identifier of the device

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	r, err := apiClient.DefaultAPI.VmRemoveDevicePut(context.Background()).VmRemoveDevice(vmRemoveDevice).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.VmRemoveDevicePut``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
}
```

### Path Parameters



### Other Parameters

Other parameters are passed through a pointer to a apiVmRemoveDevicePutRequest struct via the builder pattern


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **vmRemoveDevice** | [**VmRemoveDevice**](VmRemoveDevice.md) | The identifier of the device | 

### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## VmResizePut

> VmResizePut(ctx).VmResize(vmResize).Execute()

Resize the VM

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {
	vmResize := *openapiclient.NewVmResize() // VmResize | The target size for the VM

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	r, err := apiClient.DefaultAPI.VmResizePut(context.Background()).VmResize(vmResize).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.VmResizePut``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
}
```

### Path Parameters



### Other Parameters

Other parameters are passed through a pointer to a apiVmResizePutRequest struct via the builder pattern


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **vmResize** | [**VmResize**](VmResize.md) | The target size for the VM | 

### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## VmResizeZonePut

> VmResizeZonePut(ctx).VmResizeZone(vmResizeZone).Execute()

Resize a memory zone

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {
	vmResizeZone := *openapiclient.NewVmResizeZone() // VmResizeZone | The target size for the memory zone

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	r, err := apiClient.DefaultAPI.VmResizeZonePut(context.Background()).VmResizeZone(vmResizeZone).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.VmResizeZonePut``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
}
```

### Path Parameters



### Other Parameters

Other parameters are passed through a pointer to a apiVmResizeZonePutRequest struct via the builder pattern


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **vmResizeZone** | [**VmResizeZone**](VmResizeZone.md) | The target size for the memory zone | 

### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## VmRestorePut

> VmRestorePut(ctx).RestoreConfig(restoreConfig).Execute()

Restore a VM from a snapshot.

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {
	restoreConfig := *openapiclient.NewRestoreConfig("SourceUrl_example") // RestoreConfig | The restore configuration

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	r, err := apiClient.DefaultAPI.VmRestorePut(context.Background()).RestoreConfig(restoreConfig).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.VmRestorePut``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
}
```

### Path Parameters



### Other Parameters

Other parameters are passed through a pointer to a apiVmRestorePutRequest struct via the builder pattern


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **restoreConfig** | [**RestoreConfig**](RestoreConfig.md) | The restore configuration | 

### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## VmSendMigrationPut

> VmSendMigrationPut(ctx).SendMigrationData(sendMigrationData).Execute()

Send a VM migration to URL

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {
	sendMigrationData := *openapiclient.NewSendMigrationData("DestinationUrl_example") // SendMigrationData | The URL for sending the migration state

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	r, err := apiClient.DefaultAPI.VmSendMigrationPut(context.Background()).SendMigrationData(sendMigrationData).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.VmSendMigrationPut``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
}
```

### Path Parameters



### Other Parameters

Other parameters are passed through a pointer to a apiVmSendMigrationPutRequest struct via the builder pattern


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **sendMigrationData** | [**SendMigrationData**](SendMigrationData.md) | The URL for sending the migration state | 

### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## VmSnapshotPut

> VmSnapshotPut(ctx).VmSnapshotConfig(vmSnapshotConfig).Execute()

Returns a VM snapshot.

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {
	vmSnapshotConfig := *openapiclient.NewVmSnapshotConfig() // VmSnapshotConfig | The snapshot configuration

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	r, err := apiClient.DefaultAPI.VmSnapshotPut(context.Background()).VmSnapshotConfig(vmSnapshotConfig).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.VmSnapshotPut``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
}
```

### Path Parameters



### Other Parameters

Other parameters are passed through a pointer to a apiVmSnapshotPutRequest struct via the builder pattern


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **vmSnapshotConfig** | [**VmSnapshotConfig**](VmSnapshotConfig.md) | The snapshot configuration | 

### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## VmmNmiPut

> VmmNmiPut(ctx).Execute()

Inject an NMI.

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	r, err := apiClient.DefaultAPI.VmmNmiPut(context.Background()).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.VmmNmiPut``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
}
```

### Path Parameters

This endpoint does not need any parameter.

### Other Parameters

Other parameters are passed through a pointer to a apiVmmNmiPutRequest struct via the builder pattern


### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)


## VmmPingGet

> VmmPingResponse VmmPingGet(ctx).Execute()

Ping the VMM to check for API server availability

### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	resp, r, err := apiClient.DefaultAPI.VmmPingGet(context.Background()).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `DefaultAPI.VmmPingGet``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
	// response from `VmmPingGet`: VmmPingResponse
	fmt.Fprintf(os.Stdout, "Response from `DefaultAPI.VmmPingGet`: %v\n", resp)
}
```

### Path Parameters

This endpoint does not need any parameter.

### Other Parameters

Other parameters are passed through a pointer to a apiVmmPingGetRequest struct via the builder pattern


### Return type

[**VmmPingResponse**](VmmPingResponse.md)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)

