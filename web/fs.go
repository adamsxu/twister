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

package web

import (
	"io"
	"mime"
	"os"
	"path"
	"strconv"
	"strings"
)

type ServeFileOptions struct {
	// Map file extension to mime type.
	MimeType map[string]string

	// Response headers. 
	Header Header
}

var defaultServeFileOptions ServeFileOptions

// ServeFile responds to the request with the contents of the named file.
//
// If the "v" request parameter is set, then ServeFile sets the expires header
// and the cache control maximum age parameter to ten years in the future.
func ServeFile(req *Request, fname string, options *ServeFileOptions) {
	if options == nil {
		options = &defaultServeFileOptions
	}

	f, err := os.Open(fname)
	if err != nil {
		req.Error(StatusNotFound, err)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || !info.IsRegular() {
		req.Error(StatusNotFound, err)
		return
	}

	status := StatusOK

	header := Header{}
	if options.Header != nil {
		for k, v := range options.Header {
			header[k] = v
		}
	}

	etag := strconv.Itob64(info.Mtime_ns, 36)
	header.Set(HeaderETag, QuoteHeaderValue(etag))

	for _, qetag := range req.Header.GetList(HeaderIfNoneMatch) {
		if etag == UnquoteHeaderValue(qetag) {
			status = StatusNotModified
			break
		}
	}

	if status == StatusNotModified {
		// Clear entity headers.
		for k, _ := range header {
			if strings.HasPrefix(k, "Content-") {
				header[k] = nil, false
			}
		}
	} else {
		// Set entity headers
		header.Set(HeaderContentLength, strconv.Itoa64(info.Size))
		if _, found := header[HeaderContentType]; !found {
			ext := path.Ext(fname)
			contentType := ""
			if options.MimeType != nil {
				contentType = options.MimeType[ext]
			}
			if contentType == "" {
				contentType = mime.TypeByExtension(ext)
			}
			if contentType != "" {
				header.Set(HeaderContentType, contentType)
			}
		}
	}

	if v := req.Param.Get("v"); v != "" {

		parts := header.GetList(HeaderCacheControl)
		i := 0
		for _, part := range parts {
			if strings.HasPrefix(part, "max-age=") {
				continue
			}
			parts[i] = part
			i += 1
		}
		if i != len(parts) {
			parts = parts[:i]
		}

		const maxAge = 60 * 60 * 24 * 365 * 10
		header.Set(HeaderExpires, FormatDeltaSeconds(maxAge))
		header.Set(HeaderCacheControl, strings.Join(append(parts, "max-age="+strconv.Itoa(maxAge)), ", "))
	}

	w := req.Responder.Respond(status, header)
	if req.Method != "HEAD" && status != StatusNotModified {
		io.Copy(w, f)
	}
}

// DirectoryHandler returns a request handler that serves static files from
// root using using the URL parameter "path". The "path" parameter is typically
// set using a Router pattern match:
//
//  r.Register("/static/<path:.*>", "GET", DirectoryHandler(root))
//
// Directory handler does not serve directory listings.
func DirectoryHandler(root string, options *ServeFileOptions) Handler {
	if !path.IsAbs(root) {
		wd, err := os.Getwd()
		if err != nil {
			panic("twister: DirectoryHandler could not find cwd")
		}
		root = path.Join(wd, root)
	}
	root = path.Clean(root) + "/"
	return &directoryHandler{root, options}
}

// directoryHandler serves static files from a directory.
type directoryHandler struct {
	root    string
	options *ServeFileOptions
}

func (dh *directoryHandler) ServeWeb(req *Request) {

	fname := req.URLParam["path"]
	if fname == "" {
		panic("twister: DirectoryHandler expects path URLParam")
	}

	fname = path.Clean(dh.root + fname)
	if !strings.HasPrefix(fname, dh.root) {
		req.Error(StatusNotFound, os.NewError("twister: DirectoryHandler access outside of root"))
		return
	}

	ServeFile(req, fname, dh.options)
}

// FileHandler returns a request handler that serves a static file specified by
// fname. 
func FileHandler(fname string, options *ServeFileOptions) Handler {
	return &fileHandler{fname, options}
}

// fileHandler servers static files.
type fileHandler struct {
	fname   string
	options *ServeFileOptions
}

func (fh *fileHandler) ServeWeb(req *Request) {
	ServeFile(req, fh.fname, fh.options)
}
