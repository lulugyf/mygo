// Copyright 2014 ZeroStack, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// config
//
// config package implements an interface to parse toml config files for
// various modules. It supports configuration from files, flags and default
// values specified in source code.
//
// Lot of the heavy lifting is done by the library. The package user only has
// to specify the config struct in code. The library handles declaring the
// flags based on the json tags of the struct. The test examnple shows a
// simple wrapper that can be called by user to checks for flags that can
// override the defaults in the source code and the values specified in a
// config file.
//
// If not anything the library is a good source of reference for golang
// flags package and reflection usage :)
//
// TODO: Integrate environment variables into the scheme for completeness.

package util

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// flagChecker is a type used to store the loaded and flags vars so flags.Visit
// can be called with a member function of flagChecker.
type flagChecker struct {
	loaded interface{}
	flags  interface{}
}

// CheckFlagOverride checks if any flags are set which override the
// value of the loaded flags from file.
func CheckFlagOverride(loaded, flags interface{}) error {
	if loaded == nil {
		return fmt.Errorf("expect non-nil input to checkFlags")
	}
	if flags == nil {
		return nil
	}
	if reflect.TypeOf(loaded).Kind() != reflect.Ptr ||
		reflect.TypeOf(flags).Kind() != reflect.Ptr {
		return fmt.Errorf("expect pointer input for loaded and flags")
	}
	checker := flagChecker{loaded: loaded, flags: flags}
	flag.Visit(checker.checkFlagOverrideFunc)
	return nil
}

// WriteConfig writes the config to the destination filepath.
func WriteConfig(c interface{}, path string) error {
	tempfile, err := ioutil.TempFile("", "config")
	if err != nil {
		return fmt.Errorf("error creating temp file :: %v", err)
	}
	defer func() {
		_ = os.Remove(tempfile.Name())
	}()

	enc := toml.NewEncoder(tempfile)
	if enc == nil {
		return fmt.Errorf("could not create encoder :: %v", err)
	}

	err = enc.Encode(c)
	if err != nil {
		return fmt.Errorf("error in encoder :: %v", err)
	}

	tempfile.Close()

	if err = os.Rename(tempfile.Name(), path); err != nil {
		return fmt.Errorf("error renaming %s to %s :: %v",
			tempfile.Name(), path, err)
	}

	return nil
}

// checkFlagOverrideFunc checks if the input param flag matches a field name in
// the loaded interface. If there is a match then the value from the flags
// member is used to override the loaded interface value.
func (f *flagChecker) checkFlagOverrideFunc(val *flag.Flag) {
	typ := reflect.TypeOf(f.loaded)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	for fi := 0; fi < typ.NumField(); fi++ {
		field := typ.Field(fi)
		tagName := field.Tag.Get("toml")
		if strings.EqualFold(tagName, val.Name) {
			loadedFieldVal := reflect.ValueOf(f.loaded).Elem().Field(fi)
			flagsFieldVal := reflect.ValueOf(f.flags).Elem().Field(fi)
			fmt.Printf("overriding value with flag for %v value %v\n", tagName,
				flagsFieldVal)
			CopyField(loadedFieldVal, flagsFieldVal)
		}
	}
}

// RegisterFlags parses the input interface using reflection and registers
// all simple fields as flags based on their toml tag. It skips over complex
// fields such as slices, maps, etc.
// It also returns a copy of the struct with members bound as flags.
func RegisterFlags(v interface{}) (interface{}, error) {
	if v == nil {
		return nil, fmt.Errorf("expect non-nil input to registerFlags")
	}
	if reflect.TypeOf(v).Kind() != reflect.Ptr {
		return nil, fmt.Errorf("expect pointer input to registerFlags")
	}

	typ := reflect.TypeOf(v)
	val := reflect.ValueOf(v)
	// Just check before transforming pointer types to their elements even though
	// we made sure pointer type is checked above.
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
		val = reflect.Indirect(val)
	}

	// We only support pointers to "struct" as inputs.
	if typ.Kind() != reflect.Struct {
		return nil, fmt.Errorf("input is not a pointer to a struct")
	}

	// Allocate a new struct that we can bind the flag vars so flag values
	// can be looked up after flag.Parse()
	flags := reflect.New(typ)
	flagsVal := flags.Elem()

	for fi := 0; fi < typ.NumField(); fi++ {
		field := typ.Field(fi)
		fName := field.Name
		tagName := field.Tag.Get("toml")

		// No tag to declare the flag
		if tagName == "" {
			continue
		}

		// Create a new var of the appropriate type
		flagsValField := flagsVal.Field(fi)
		CopyField(flagsValField, val.Field(fi))

		fTyp := field.Type
		fKind := fTyp.Kind()
		// Get the value for the field already in the input which will be the
		// default value for the flag.
		fVal := val.FieldByName(fName)
		// If field is a pointer then adjust the type, kind and value to point to
		// the actual object.
		if fKind == reflect.Ptr {
			fTyp = fTyp.Elem()
			fKind = fTyp.Kind()
			fVal = reflect.Indirect(val.Field(fi))
		}
		if fKind == reflect.Struct {
			_, err := RegisterFlags(val.Field(fi).Interface())
			if err != nil {
				fmt.Printf("error registering flag for struct: %s : %v\n", fName, err)
			}
			continue
		}
		// If the field is not a pointer, get the address so we can bind it to the
		// flags.Var().
		bindField := flagsValField
		if field.Type.Kind() != reflect.Ptr {
			bindField = flagsValField.Addr()
		}
		// Skip complex types like slices and structs since we do not know how to
		// declare flags for them.
		// TODO: do we want to register flags for nested types?
		if fKind == reflect.Slice || fKind == reflect.Map {
			fmt.Printf("warning: flags not supported for nested data types for %v\n",
				fName)
			continue
		}
		// Declare a flag of that type, Default usage to name of flag.
		// TODO: get usage string from tag?
		err := bindVar(fTyp, bindField, tagName, fVal, tagName)
		if err != nil {
			fmt.Printf("error: could not declare var of type %v\n", fKind)
			return nil, err
		}
	}
	return flags.Interface(), nil
}

// CopyField copies src Value into dst Value handling the different data types.
func CopyField(dst, src reflect.Value) {
	switch src.Kind() {
	case reflect.Ptr:
		srcValue := src.Elem()
		// Check if the src pointer is nil.
		if !srcValue.IsValid() {
			return
		}
		// Allocate a new object and set the pointer to it.
		dst.Set(reflect.New(srcValue.Type()))
		dstValue := dst.Elem()
		// Copy the actual contents from the pointer recursively.
		CopyField(dstValue, srcValue)

	// Interface which is a pre-allocated pointer so just copy the values.
	case reflect.Interface:
		srcValue := src.Elem()
		dstValue := reflect.New(srcValue.Type()).Elem()
		CopyField(dstValue, srcValue)
		dst.Set(dstValue)

	case reflect.Struct:
		for i := 0; i < src.NumField(); i++ {
			CopyField(dst.Field(i), src.Field(i))
		}

	// If it is a slice we create a new slice and copy each field.
	case reflect.Slice:
		dst.Set(reflect.MakeSlice(src.Type(), src.Len(), src.Cap()))
		for i := 0; i < src.Len(); i++ {
			CopyField(dst.Index(i), src.Index(i))
		}

	// If it is a map we create a new map and copy each key-value.
	case reflect.Map:
		dst.Set(reflect.MakeMap(src.Type()))
		for _, key := range src.MapKeys() {
			srcValue := src.MapIndex(key)
			dstValue := reflect.New(srcValue.Type()).Elem()
			CopyField(dstValue, srcValue)
			dst.SetMapIndex(key, dstValue)
		}

	default:
		dst.Set(src)
	}
}

// bindVar declares a flag variable based on the type.
// t - the type of flag
// v - the variable that is bound to the flag
// name - the name of the flag
// def - the default value of the flag
// usage - the usage string for the flag
func bindVar(t reflect.Type, v reflect.Value, name string, def reflect.Value,
	usage string) error {

	switch t.Kind() {
	case reflect.Bool:
		flag.BoolVar(v.Interface().(*bool), name, def.Interface().(bool), name)
	case reflect.Int:
		flag.IntVar(v.Interface().(*int), name, def.Interface().(int), name)
	case reflect.Int32:
		flag.CommandLine.Var(
			newInt32Value(def.Interface().(int32), v.Interface().(*int32)),
			name, name)
	case reflect.Int64:
		infInt64, ok := v.Interface().(*int64)
		if ok {
			flag.Int64Var(infInt64, name, def.Interface().(int64), name)
			return nil
		}
		infDur, ok := v.Interface().(*time.Duration)
		if ok {
			flag.DurationVar(infDur, name, (def.Interface().(time.Duration)), name)
			return nil
		}
	case reflect.Uint:
		flag.UintVar(v.Interface().(*uint), name, def.Interface().(uint), name)
	// flag package doesn't define Uint32Var!
	case reflect.Uint32:
		flag.CommandLine.Var(
			newUint32Value(def.Interface().(uint32), v.Interface().(*uint32)),
			name, name)
	case reflect.Uint64:
		flag.Uint64Var(v.Interface().(*uint64), name, (def.Interface().(uint64)),
			name)
	case reflect.String:
		flag.StringVar(v.Interface().(*string), name,
			(def.Interface().(string)), name)
	case reflect.Float64:
		flag.Float64Var(v.Interface().(*float64), name,
			(def.Interface().(float64)), name)
	default:
		return fmt.Errorf("unsupported flag type %v", t.Kind())
	}
	return nil
}

// These are some required interface functions implemented to that
// flag.Int32Var will work since default flag package only has Int and Int64.
type int32Value int32

// newInt32Value returns a new object with the value set to val param.
func newInt32Value(val int32, p *int32) *int32Value {
	*p = val
	return (*int32Value)(p)
}

// Set the value of Int32 based on the string.
func (i *int32Value) Set(s string) error {
	v, err := strconv.ParseInt(s, 0, 32)
	*i = int32Value(v)
	return err
}

// Get returns the int32 value of the type.
func (i *int32Value) Get() interface{} { return int32(*i) }

// String returns the string representation.
func (i *int32Value) String() string { return fmt.Sprintf("%v", *i) }

// These are some required interface functions implemented so that
// flag.Uint32Var will work since default flag package only has Uint and Uint64.
type uint32Value uint32

// newUint32Value returns a new object with the value set to val param.
func newUint32Value(val uint32, p *uint32) *uint32Value {
	*p = val
	return (*uint32Value)(p)
}

// Set the value of Uint32 based on the string.
func (i *uint32Value) Set(s string) error {
	v, err := strconv.ParseUint(s, 0, 32)
	*i = uint32Value(v)
	return err
}

// Get returns the uint32 value of the type.
func (i *uint32Value) Get() interface{} { return uint32(*i) }

// String returns the string representation.
func (i *uint32Value) String() string { return fmt.Sprintf("%v", *i) }
