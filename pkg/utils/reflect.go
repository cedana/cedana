package utils

import (
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
)

// FunctionName returns the name of the function pointed to by f
func FunctionName(pc uintptr) string {
	fullname := runtime.FuncForPC(pc).Name()
	return fullname
	// components := strings.Split(fullname, "/")
	// return components[len(components)-1]
}

// SimplifyFuncName simplifies the function name to a category and a name.
// Removes the long package prefix. If the function belongs to a plugin, the plugin name is returned.
func SimplifyFuncName(f string) (category string, name string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", f // fallback
	}

	splits := strings.Split(f, ":")
	if len(splits) > 1 {
		category = splits[len(splits)-2]
		f = splits[len(splits)-1]
	} else {
		pluginPattern, err := regexp.Compile(
			fmt.Sprintf(
				`%s/plugins/([a-zA-Z0-9_]+)(/|\.)`,
				info.Main.Path,
			))
		if err != nil {
			return "", f // fallback
		}
		matches := pluginPattern.FindStringSubmatch(f)
		if len(matches) > 1 {
			category = matches[1]
		}
	}

	name = filepath.Base(f)

	trailPattern := regexp.MustCompile(`\.func\d+(\.\d+)?$`)
	name = trailPattern.ReplaceAllString(name, "")

	return
}

func IsCallerSameAsUs(caller string) bool {
	pc, _, _, ok := runtime.Caller(1)
	if !ok {
		return false
	}
	return FunctionName(pc) == caller
}

// GetValue returns the value of the field with the given name.
// If the field does not exist, or is not set, the zero value for the field
// type will be returned. Nested fields can be specified by separating them
// with a period.
func GetValue(i interface{}, field string) interface{} {
	v := reflect.ValueOf(i)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	fields := strings.Split(field, ".")
	for _, field := range fields {
		v = v.FieldByName(field)
		if !v.IsValid() {
			return nil
		}
	}
	return v.Interface()
}

// GetTag returns the value of the tag with the given name for the field with the given name.
// Nested fields can be specified by separating them with a period, and the returned tag will also
// be separated by periods.
// If the field does not exist, or the tag does not exist, an empty string will be returned.
func GetTag(i interface{}, field string, tag string) string {
	t := reflect.TypeOf(i)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return ""
	}
	fields := strings.Split(field, ".")
	vals := make([]string, 0, len(fields))
	for _, field := range fields {
		f, ok := t.FieldByName(field)
		if !ok {
			return ""
		}
		tag := f.Tag.Get(tag)
		if tag != "" {
			vals = append(vals, tag)
		}
		t = f.Type
	}
	return strings.Join(vals, ".")
}

// ListFields returns a list of field names for the given struct.
// If a tag is specified, it will use the tag value instead of the field name.
func ListFields(i interface{}, tag ...string) []string {
	t := reflect.TypeOf(i)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	fields := make([]string, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if len(tag) > 0 {
			fields[i] = field.Tag.Get(tag[0])
		} else {
			fields[i] = field.Name
		}
	}
	return fields
}

// LeavesList returns a list of field names for the given struct
// If a field is a struct, it will recursively call itself to get the fields.
// If a tag is specified, it will use the tag value instead of the field name.
// Nested fields are separated by a period.
func ListLeaves(i interface{}, tag ...string) []string {
	t := reflect.TypeOf(i)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	fields := make([]string, 0)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		var name string
		if len(tag) > 0 {
			name = field.Tag.Get(tag[0])
		} else {
			name = field.Name
		}

		if field.Type.Kind() == reflect.Struct {
			subfields := ListLeaves(reflect.New(field.Type).Interface(), tag...)
			for _, subfield := range subfields {
				fields = append(fields, name+"."+subfield)
			}
		} else {
			fields = append(fields, name)
		}
	}

	return fields
}

// WalkTree recursively walks a tree-structured struct using reflection,
// calling fn for every element in the specified valuesField slice.
// If fn returns false, the walk stops immediately. If true, it continues.
// childrenField specifies which struct field contains the slice of child nodes.
// The field names should match exactly (including capitalization).
func WalkTree[T any](
	node any,
	valuesField string,
	childrenField string,
	fn func(T) bool,
) bool {
	v := reflect.ValueOf(node)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return true // continue
	}

	// Process values field
	values := v.FieldByName(valuesField)
	if values.IsValid() && values.Kind() == reflect.Slice {
		for i := 0; i < values.Len(); i++ {
			if !fn(values.Index(i).Interface().(T)) {
				return false // stop
			}
		}
	}

	// Process children field
	children := v.FieldByName(childrenField)
	if children.IsValid() && children.Kind() == reflect.Slice {
		for i := 0; i < children.Len(); i++ {
			child := children.Index(i)
			if child.Kind() == reflect.Ptr || child.Kind() == reflect.Struct {
				if !WalkTree(child.Interface(), valuesField, childrenField, fn) {
					return false // stop
				}
			}
		}
	}
	return true // continue
}
