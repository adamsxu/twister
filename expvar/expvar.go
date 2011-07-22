// Copyright 2011 Gary Burd
//
// Licensed under the Apache License, Version 2.0 (the "License"): you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations
// under the License.

// Package expvar implements a Twister handler for registering and publishing
// objects as JSON over HTTP. This is useful for publishing counters and other
// operational data for monitoring tools.
// 
// The application should wrap the ServeWeb function in this package with
// appropriate access control and register the resulting handler with a web
// server.
//
// This package differs from the standard library's expvar package in two ways:
// This package does not have a dependency on the standard library's HTTP
// server. This package uses the json.Marshaler interface for extensibility
// instead of defining its own interface.
package expvar

import (
	"github.com/garyburd/twister/web"
	"json"
	"log"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"
)

var (
	mutex sync.Mutex
	vars  = map[string]interface{}{}
)

// Publish adds v to the root level JSON object published by this package. The
// value v is any type that can be encoded by calling json.Marshal. If a name
// is already registered, then the function will panic. This function is
// typically called from a package's init() function. 
func Publish(name string, v interface{}) {
	mutex.Lock()
	defer mutex.Unlock()
	if _, found := vars[name]; found {
		log.Panicln("Reuse of published var name:", name)
	}
	vars[name] = v
}

// The MarshalJSONFunc type is an adapter to allow the use of ordinary
// functions as JSON marshallers. The function is called each time the object
// is marshaled.
type MarshalJSONFunc func() ([]byte, os.Error)

func (f MarshalJSONFunc) MarshalJSON() ([]byte, os.Error) {
	return f()
}

// Func wraps a func() interface{} with JSON marshalling of the returned
// value. The function is called each time the object is marshaled.
type Func func() interface{}

func (f Func) MarshalJSON() ([]byte, os.Error) {
	return json.Marshal(f())
}

// Map is a synchronized wrapper around a string-to-value map.
type Map struct {
	mu sync.Mutex
	m  map[string]interface{}
}

// NewMap allocates a new Map and publishes it using name.
func NewMap(name string) *Map {
	m := &Map{}
	m.Init()
	Publish(name, m)
	return m
}

func (m *Map) MarshalJSON() ([]byte, os.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return json.Marshal(m.m)
}

func (m *Map) Init() *Map {
	m.m = map[string]interface{}{}
	return m
}

func (m *Map) Get(key string) interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.m[key]
}

func (m *Map) Set(key string, v interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.m[key] = v
}

// AddInt adds delta to the *Int value stored for the given map key.
func (m *Map) AddInt(key string, delta int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.m[key]
	if !ok {
		v = &Int{}
		m.m[key] = v
	}

	if iv, ok := v.(*Int); ok {
		iv.Add(delta)
	}
}

// Int is a synchronized wrapper around a 64-bit integer. The Int type is useful
// implementing counters that can be updated by by concurrent goroutines.
type Int struct {
	i  int64
	mu sync.Mutex
}

func (i *Int) MarshalJSON() ([]byte, os.Error) {
	return []byte(strconv.Itoa64(i.i)), nil
}

// Add atomically adds delta to the counter.
func (i *Int) Add(delta int64) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.i += delta
}

// Set atomically sets the counter to value.
func (i *Int) Set(value int64) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.i = value
}

// NewInt allocates a new counter and publishes it using name.
func NewInt(name string) *Int {
	v := &Int{}
	Publish(name, v)
	return v
}

func ServeWeb(req *web.Request) {
	b, err := json.MarshalIndent(vars, "", " ")
	if err != nil {
		req.Error(web.StatusInternalServerError, err)
		return
	}
	req.Respond(web.StatusOK, web.HeaderContentType, "application/json; charset=utf-8").Write(b)
}

func init() {
	start := time.Seconds()
	Publish("runtime", map[string]interface{}{
		"cgocalls":   Func(func() interface{} { return runtime.Cgocalls() }),
		"goroutines": Func(func() interface{} { return runtime.Goroutines() }),
		"version":    runtime.Version(),
		"memstats":   &runtime.MemStats,
	})
	Publish("uptimeSeconds", Func(func() interface{} { return time.Seconds() - start }))
	Publish("cmdline", &os.Args)
}
