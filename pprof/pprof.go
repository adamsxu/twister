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

// Package pprof implements Twister request handlers for the pprof tool.
// 
// The application should wrap the ServeWeb function in this package with
// appropriate access control and register the resulting handler for a path
// with the suffix "/pprof/.*". An example of registering the handler with
// a web.Router is:
//
//  r.Register("/debug/pprof/<:.*>", "*", pprof.ServeWeb)
//
// The handler is not compatible with the XSRF protection in
// web.WebFormHandler. See twister/example/demo/main.go for an example of how
// structure an application with both pprof an XSRF protection.
//
// Use the gopprof tool to look at a heap profile:
//
//	gopprof http://localhost:8080/debug/pprof/heap
//
// Or to look at a 30-second CPU profile:
//
//	gopprof http://localhost:6060/debug/pprof/profile
package pprof

import (
	"bytes"
	"fmt"
	"github.com/garyburd/twister/web"
	"io"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"
)

func respondText(req *web.Request) io.Writer {
	return req.Respond(web.StatusOK,
		web.HeaderContentType, "text/plain; charset=utf-8")
}

type lazyResponder struct {
	req *web.Request
	w   io.Writer
}

func (r lazyResponder) Write(p []byte) (int, os.Error) {
	if r.w == nil {
		r.w = r.req.Respond(web.StatusOK,
			web.HeaderContentType, "application/octet-stream")
	}
	return r.w.Write(p)
}

func serveProfile(req *web.Request) {
	sec, _ := strconv.Atoi64(req.Param.Get("seconds"))
	if sec == 0 {
		sec = 30
	}
	if err := pprof.StartCPUProfile(&lazyResponder{req, nil}); err != nil {
		req.Error(web.StatusInternalServerError, err)
		return
	}
	time.Sleep(sec * 1e9)
	pprof.StopCPUProfile()
}

func serveSymbol(req *web.Request) {
	var p []byte
	if req.Method == "POST" {
		var err os.Error
		p, err = req.BodyBytes(-1)
		if err != nil {
			req.Error(web.StatusInternalServerError, err)
			return
		}
	} else {
		p = []byte(req.URL.RawQuery)
	}

	w := respondText(req)
	io.WriteString(w, "num_symbols: 1\n")
	for len(p) > 0 {
		var a []byte
		if i := bytes.IndexByte(p, '+'); i >= 0 {
			a = p[:i]
			p = p[i+1:]
		} else {
			a = p
			p = nil
		}
		if pc, _ := strconv.Btoui64(string(a), 0); pc != 0 {
			if f := runtime.FuncForPC(uintptr(pc)); f != nil {
				fmt.Fprintf(w, "%#x %s\n", pc, f.Name())
			}
		}
	}
}

// ServeWeb serves profile data for the pprof tool.
func ServeWeb(req *web.Request) {
	switch {
	case strings.HasSuffix(req.URL.Path, "/pprof/cmdline"):
		io.WriteString(respondText(req), strings.Join(os.Args, "\x00"))
	case strings.HasSuffix(req.URL.Path, "/pprof/profile"):
		serveProfile(req)
	case strings.HasSuffix(req.URL.Path, "/pprof/heap"):
		pprof.WriteHeapProfile(respondText(req))
	case strings.HasSuffix(req.URL.Path, "/pprof/symbol"):
		serveSymbol(req)
	default:
		req.Error(web.StatusNotFound, nil)
	}
}
