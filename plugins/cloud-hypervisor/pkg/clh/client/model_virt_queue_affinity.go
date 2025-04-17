/*
Cloud Hypervisor API

Local HTTP based API for managing and inspecting a cloud-hypervisor virtual machine.

API version: 0.3.0
*/

// Code generated by OpenAPI Generator (https://openapi-generator.tech); DO NOT EDIT.

package openapi

import (
	"encoding/json"
	"bytes"
	"fmt"
)

// checks if the VirtQueueAffinity type satisfies the MappedNullable interface at compile time
var _ MappedNullable = &VirtQueueAffinity{}

// VirtQueueAffinity struct for VirtQueueAffinity
type VirtQueueAffinity struct {
	QueueIndex int32 `json:"queue_index"`
	HostCpus []int32 `json:"host_cpus"`
}

type _VirtQueueAffinity VirtQueueAffinity

// NewVirtQueueAffinity instantiates a new VirtQueueAffinity object
// This constructor will assign default values to properties that have it defined,
// and makes sure properties required by API are set, but the set of arguments
// will change when the set of required properties is changed
func NewVirtQueueAffinity(queueIndex int32, hostCpus []int32) *VirtQueueAffinity {
	this := VirtQueueAffinity{}
	this.QueueIndex = queueIndex
	this.HostCpus = hostCpus
	return &this
}

// NewVirtQueueAffinityWithDefaults instantiates a new VirtQueueAffinity object
// This constructor will only assign default values to properties that have it defined,
// but it doesn't guarantee that properties required by API are set
func NewVirtQueueAffinityWithDefaults() *VirtQueueAffinity {
	this := VirtQueueAffinity{}
	return &this
}

// GetQueueIndex returns the QueueIndex field value
func (o *VirtQueueAffinity) GetQueueIndex() int32 {
	if o == nil {
		var ret int32
		return ret
	}

	return o.QueueIndex
}

// GetQueueIndexOk returns a tuple with the QueueIndex field value
// and a boolean to check if the value has been set.
func (o *VirtQueueAffinity) GetQueueIndexOk() (*int32, bool) {
	if o == nil {
		return nil, false
	}
	return &o.QueueIndex, true
}

// SetQueueIndex sets field value
func (o *VirtQueueAffinity) SetQueueIndex(v int32) {
	o.QueueIndex = v
}

// GetHostCpus returns the HostCpus field value
func (o *VirtQueueAffinity) GetHostCpus() []int32 {
	if o == nil {
		var ret []int32
		return ret
	}

	return o.HostCpus
}

// GetHostCpusOk returns a tuple with the HostCpus field value
// and a boolean to check if the value has been set.
func (o *VirtQueueAffinity) GetHostCpusOk() ([]int32, bool) {
	if o == nil {
		return nil, false
	}
	return o.HostCpus, true
}

// SetHostCpus sets field value
func (o *VirtQueueAffinity) SetHostCpus(v []int32) {
	o.HostCpus = v
}

func (o VirtQueueAffinity) MarshalJSON() ([]byte, error) {
	toSerialize,err := o.ToMap()
	if err != nil {
		return []byte{}, err
	}
	return json.Marshal(toSerialize)
}

func (o VirtQueueAffinity) ToMap() (map[string]interface{}, error) {
	toSerialize := map[string]interface{}{}
	toSerialize["queue_index"] = o.QueueIndex
	toSerialize["host_cpus"] = o.HostCpus
	return toSerialize, nil
}

func (o *VirtQueueAffinity) UnmarshalJSON(data []byte) (err error) {
	// This validates that all required properties are included in the JSON object
	// by unmarshalling the object into a generic map with string keys and checking
	// that every required field exists as a key in the generic map.
	requiredProperties := []string{
		"queue_index",
		"host_cpus",
	}

	allProperties := make(map[string]interface{})

	err = json.Unmarshal(data, &allProperties)

	if err != nil {
		return err;
	}

	for _, requiredProperty := range(requiredProperties) {
		if _, exists := allProperties[requiredProperty]; !exists {
			return fmt.Errorf("no value given for required property %v", requiredProperty)
		}
	}

	varVirtQueueAffinity := _VirtQueueAffinity{}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&varVirtQueueAffinity)

	if err != nil {
		return err
	}

	*o = VirtQueueAffinity(varVirtQueueAffinity)

	return err
}

type NullableVirtQueueAffinity struct {
	value *VirtQueueAffinity
	isSet bool
}

func (v NullableVirtQueueAffinity) Get() *VirtQueueAffinity {
	return v.value
}

func (v *NullableVirtQueueAffinity) Set(val *VirtQueueAffinity) {
	v.value = val
	v.isSet = true
}

func (v NullableVirtQueueAffinity) IsSet() bool {
	return v.isSet
}

func (v *NullableVirtQueueAffinity) Unset() {
	v.value = nil
	v.isSet = false
}

func NewNullableVirtQueueAffinity(val *VirtQueueAffinity) *NullableVirtQueueAffinity {
	return &NullableVirtQueueAffinity{value: val, isSet: true}
}

func (v NullableVirtQueueAffinity) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.value)
}

func (v *NullableVirtQueueAffinity) UnmarshalJSON(src []byte) error {
	v.isSet = true
	return json.Unmarshal(src, &v.value)
}


