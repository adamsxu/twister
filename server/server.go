// Copyright 2010 Gary Burd
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

// The server package implements the HTTP protocol for a web server.
package server

import (
	"bufio"
	"bytes"
	"github.com/garyburd/twister/web"
	"http"
	"io"
	"log"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrBadRequestLine = os.NewError("could not parse request line")
	ErrLineTooLong    = os.NewError("request line or header line too long")
	ErrBadHeaderLine  = os.NewError("could not parse header line")
	ErrHeaderTooLong  = os.NewError("header value too long")
	ErrHeadersTooLong = os.NewError("too many headers")
)

type conn struct {
	serverName         string
	secure             bool
	netConn            net.Conn
	br                 *bufio.Reader
	bw                 *bufio.Writer
	chunked            bool
	closeAfterResponse bool
	hijacked           bool
	req                *web.Request
	requestAvail       int
	requestErr         os.Error
	respondCalled      bool
	responseAvail      int
	responseErr        os.Error
	write100Continue   bool
}

func skipBytes(p []byte, f func(byte) bool) int {
	i := 0
	for ; i < len(p); i++ {
		if !f(byte(p[i])) {
			break
		}
	}
	return i
}

func trimWSLeft(p []byte) []byte {
	return p[skipBytes(p, web.IsSpaceByte):]
}

func trimWSRight(p []byte) []byte {
	var i int
	for i = len(p); i > 0; i-- {
		if !web.IsSpaceByte(p[i-1]) {
			break
		}
	}
	return p[0:i]
}

var requestLineRegexp = regexp.MustCompile("^([_A-Za-z0-9]+) ([^ ]+) HTTP/([0-9]+)\\.([0-9]+)[\r\n ]+$")

func readRequestLine(b *bufio.Reader) (method string, url string, version int, err os.Error) {

	p, err := b.ReadSlice('\n')
	if err != nil {
		if err == bufio.ErrBufferFull {
			err = ErrLineTooLong
		}
		return
	}

	m := requestLineRegexp.FindSubmatch(p)
	if m == nil {
		err = ErrBadRequestLine
		return
	}

	method = string(m[1])

	major, err := strconv.Atoi(string(m[3]))
	if err != nil {
		return
	}

	minor, err := strconv.Atoi(string(m[4]))
	if err != nil {
		return
	}

	version = web.ProtocolVersion(major, minor)

	url = string(m[2])

	return
}

func readHeader(b *bufio.Reader) (header web.StringsMap, err os.Error) {

	const (
		// Max size for header line
		maxLineSize = 4096
		// Max size for header value
		maxValueSize = 4096
		// Maximum number of headers 
		maxHeaderCount = 256
	)

	header = make(web.StringsMap)
	lastKey := ""
	headerCount := 0

	for {
		p, err := b.ReadSlice('\n')
		if err != nil {
			if err == bufio.ErrBufferFull {
				err = ErrLineTooLong
			} else if err == os.EOF {
				err = io.ErrUnexpectedEOF
			}
			return nil, err
		}

		// remove line terminator
		if len(p) >= 2 && p[len(p)-2] == '\r' {
			// \r\n
			p = p[0 : len(p)-2]
		} else {
			// \n
			p = p[0 : len(p)-1]
		}

		// End of headers?
		if len(p) == 0 {
			break
		}

		// Don't allow huge header lines.
		if len(p) > maxLineSize {
			return nil, ErrLineTooLong
		}

		if web.IsSpaceByte(p[0]) {

			if lastKey == "" {
				return nil, ErrBadHeaderLine
			}

			p = trimWSLeft(trimWSRight(p))

			if len(p) > 0 {
				values := header[lastKey]
				value := values[len(values)-1]
				value = value + " " + string(p)
				if len(value) > maxValueSize {
					return nil, ErrHeaderTooLong
				}
				values[len(values)-1] = value
			}

		} else {

			// New header
			headerCount = headerCount + 1
			if headerCount > maxHeaderCount {
				return nil, ErrHeadersTooLong
			}

			// Key
			i := skipBytes(p, web.IsTokenByte)
			if i < 1 {
				return nil, ErrBadHeaderLine
			}
			key := web.HeaderNameBytes(p[0:i])
			p = p[i:]
			lastKey = key

			p = trimWSLeft(p)

			// Colon
			if p[0] != ':' {
				return nil, ErrBadHeaderLine
			}
			p = p[1:]

			// Value 
			p = trimWSLeft(p)
			value := string(trimWSRight(p))
			header.Append(key, value)
		}
	}
	return header, nil
}

func (c *conn) prepare() (err os.Error) {

	method, rawURL, version, err := readRequestLine(c.br)
	if err != nil {
		return err
	}

	header, err := readHeader(c.br)
	if err != nil {
		return err
	}

	url, err := http.ParseURL(rawURL)
	if err != nil {
		return err
	}

	if url.Host == "" {
		url.Host = header.GetDef(web.HeaderHost, "")
		if url.Host == "" {
			url.Host = c.serverName
		}
	}

	if c.secure {
		url.Scheme = "https"
	} else {
		url.Scheme = "http"
	}

	req, err := web.NewRequest(c.netConn.RemoteAddr().String(), method, url, version, header)
	if err != nil {
		return
	}
	c.req = req

	c.requestAvail = req.ContentLength
	if c.requestAvail < 0 {
		c.requestAvail = 0
	}

	if s, found := req.Header.Get(web.HeaderExpect); found {
		c.write100Continue = strings.ToLower(s) == "100-continue"
	}

	connection := strings.ToLower(req.Header.GetDef(web.HeaderConnection, ""))
	if version >= web.ProtocolVersion(1, 1) {
		c.closeAfterResponse = connection == "close"
	} else if version == web.ProtocolVersion(1, 0) && req.ContentLength >= 0 {
		c.closeAfterResponse = connection != "keep-alive"
	} else {
		c.closeAfterResponse = true
	}

	req.Responder = c
	req.Body = requestReader{c}
	return nil
}

type requestReader struct {
	*conn
}

func (c requestReader) Read(p []byte) (int, os.Error) {
	if c.requestErr != nil {
		return 0, c.requestErr
	}
	if c.write100Continue {
		c.write100Continue = false
		io.WriteString(c.netConn, "HTTP/1.1 100 Continue\r\n\r\n")
	}
	if c.requestAvail <= 0 {
		c.requestErr = os.EOF
		return 0, c.requestErr
	}
	if len(p) > c.requestAvail {
		p = p[0:c.requestAvail]
	}
	var n int
	n, c.requestErr = c.br.Read(p)
	c.requestAvail -= n
	return n, c.requestErr
}

func (c *conn) Respond(status int, header web.StringsMap) (body web.ResponseBody) {
	if c.hijacked {
		log.Stderr("twister: Respond called on hijacked connection")
		return nil
	}
	if c.respondCalled {
		log.Stderr("twister: multiple calls to Respond")
		return nil
	}
	c.respondCalled = true
	c.requestErr = web.ErrInvalidState

	if _, found := header.Get(web.HeaderTransferEncoding); found {
		log.Stderr("twister: transfer encoding not allowed")
		header[web.HeaderTransferEncoding] = nil, false
	}

	if c.requestAvail > 0 {
		c.closeAfterResponse = true
	}

	c.chunked = true
	c.responseAvail = 0

	if status == web.StatusNotModified {
		header[web.HeaderContentType] = nil, false
		header[web.HeaderContentLength] = nil, false
		c.chunked = false
	} else if s, found := header.Get(web.HeaderContentLength); found {
		c.responseAvail, _ = strconv.Atoi(s)
		c.chunked = false
	} else if c.req.ProtocolVersion < web.ProtocolVersion(1, 1) {
		c.closeAfterResponse = true
	}

	if c.closeAfterResponse {
		header.Set(web.HeaderConnection, "close")
		c.chunked = false
	}

	if c.chunked {
		header.Set(web.HeaderTransferEncoding, "chunked")
	}

	proto := "HTTP/1.0"
	if c.req.ProtocolVersion >= web.ProtocolVersion(1, 1) {
		proto = "HTTP/1.1"
	}
	statusString := strconv.Itoa(status)
	text := web.StatusText(status)

	var b bytes.Buffer
	b.WriteString(proto)
	b.WriteString(" ")
	b.WriteString(statusString)
	b.WriteString(" ")
	b.WriteString(text)
	b.WriteString("\r\n")
	for key, values := range header {
		for _, value := range values {
			b.WriteString(key)
			b.WriteString(": ")
			b.WriteString(cleanHeaderValue(value))
			b.WriteString("\r\n")
		}
	}
	b.WriteString("\r\n")

	if c.chunked {
		c.bw = bufio.NewWriter(chunkedWriter{c})
		_, c.responseErr = c.netConn.Write(b.Bytes())
	} else {
		c.bw = bufio.NewWriter(identityWriter{c})
		c.bw.Write(b.Bytes())
	}

	return c.bw
}

// cleanHeaderValue replaces \r and \n with ' ' in header values to prevent
// response splitting attacks.  
func cleanHeaderValue(s string) string {
	dirty := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\r' || c == '\n' {
			dirty = true
			break
		}
	}
	if !dirty {
		return s
	}
	p := []byte(s)
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '\r' || c == '\n' {
			p[i] = ' '
		}
	}
	return string(p)
}

func (c *conn) Hijack() (conn net.Conn, buf []byte, err os.Error) {
	if c.respondCalled {
		return nil, nil, web.ErrInvalidState
	}

	conn = c.netConn
	buf, err = c.br.Peek(c.br.Buffered())
	if err != nil {
		panic("twsited.server: unexpected error peeking at bufio")
	}

	c.hijacked = true
	c.requestErr = web.ErrInvalidState
	c.responseErr = web.ErrInvalidState
	c.req = nil
	c.br = nil
	c.netConn = nil

	return
}

// Finish the HTTP request
func (c *conn) finish() os.Error {
	if !c.respondCalled {
		c.req.Respond(web.StatusOK, web.HeaderContentType, "text/html charset=utf-8")
	}
	if c.responseAvail != 0 {
		c.closeAfterResponse = true
	}
	c.bw.Flush()
	if c.chunked {
		_, c.responseErr = io.WriteString(c.netConn, "0\r\n\r\n")
	}
	if c.responseErr == nil {
		c.responseErr = web.ErrInvalidState
	}
	c.netConn = nil
	c.br = nil
	c.bw = nil
	return nil
}

type identityWriter struct {
	*conn
}

func (c identityWriter) Write(p []byte) (int, os.Error) {
	if c.responseErr != nil {
		return 0, c.responseErr
	}
	var n int
	n, c.responseErr = c.netConn.Write(p)
	c.responseAvail -= n
	return n, c.responseErr
}

type chunkedWriter struct {
	*conn
}

func (c chunkedWriter) Write(p []byte) (int, os.Error) {
	if c.responseErr != nil {
		return 0, c.responseErr
	}
	if len(p) == 0 {
		return 0, nil
	}
	_, c.responseErr = io.WriteString(c.netConn, strconv.Itob(len(p), 16)+"\r\n")
	if c.responseErr != nil {
		return 0, c.responseErr
	}
	var n int
	n, c.responseErr = c.netConn.Write(p)
	if c.responseErr != nil {
		return n, c.responseErr
	}
	_, c.responseErr = io.WriteString(c.netConn, "\r\n")
	return 0, c.responseErr
}

func serveConnection(serverName string, secure bool, handler web.Handler, netConn net.Conn) {
	br := bufio.NewReader(netConn)
	for {
		c := conn{
			serverName: serverName,
			secure:     secure,
			netConn:    netConn,
			br:         br}
		if err := c.prepare(); err != nil {
			if err != os.EOF {
				log.Stderr("twister/sever: prepare failed", err)
			}
			break
		}
		handler.ServeWeb(c.req)
		if c.hijacked {
			return
		}
		if err := c.finish(); err != nil {
			log.Stderr("twister/sever: finish failed", err)
			break
		}
		if c.closeAfterResponse {
			break
		}
	}
	netConn.Close()
}

// Serve accepts incoming HTTP connections on the listener l, creating a new
// goroutine for each. The goroutines read requests and then call handler to
// reply to them.
func Serve(serverName string, secure bool, handler web.Handler, l net.Listener) os.Error {
	for {
		netConn, e := l.Accept()
		if e != nil {
			return e
		}
		go serveConnection(serverName, secure, handler, netConn)
	}
	return nil
}

// ListenAndServe listens on the TCP network address addr and then calls Serve
// with handler to handle requests on incoming connections.  
func ListenAndServe(serverName string, addr string, handler web.Handler) os.Error {
	l, e := net.Listen("tcp", addr)
	if e != nil {
		return e
	}
	defer l.Close()
	return Serve(serverName, false, handler, l)
}
