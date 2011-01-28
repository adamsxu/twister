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

package websocket

import (
	"github.com/garyburd/twister/web"
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"io"
	"net"
	"os"
	"strings"
)

type Conn struct {
	conn net.Conn
	br   *bufio.Reader
	bw   *bufio.Writer
}

func (conn *Conn) Close() os.Error {
	return conn.conn.Close()
}

func (conn *Conn) Receive() ([]byte, os.Error) {
	// Support text framing for now. Revisit after browsers support framing
	// described in later specs.
	c, err := conn.br.ReadByte()
	if err != nil {
		return nil, err
	}
	if c != 0 {
		return nil, os.NewError("twister.websocket: unexpected framing.")
	}
	p, err := conn.br.ReadSlice(0xff)
	if err != nil {
		return nil, err
	}
	return p[:len(p)-1], nil
}

func (conn *Conn) Send(p []byte) os.Error {
	// Support text framing for now. Revisit after browsers support framing
	// described in later specs.
	conn.bw.WriteByte(0)
	conn.bw.Write(p)
	conn.bw.WriteByte(0xff)
	return conn.bw.Flush()
}

// webSocketKey returns the key bytes from the specified websocket key header.
func webSocketKey(req *web.Request, name string) (key []byte, err os.Error) {
	s, found := req.Header.Get(name)
	if !found {
		return key, os.NewError("twister.websocket: missing key")
	}
	var n uint32 // number formed from decimal digits in key
	var d uint32 // number of spaces in key
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b == ' ' {
			d += 1
		} else if '0' <= b && b <= '9' {
			n = n*10 + uint32(b) - '0'
		}
	}
	if d == 0 || n%d != 0 {
		return nil, os.NewError("twister.websocket: bad key")
	}
	key = make([]byte, 4)
	binary.BigEndian.PutUint32(key, n/d)
	return key, nil
}

// Upgrade upgrades the HTTP connection to the WebSocket protocol. The 
// caller is responsible for closing the returned connection.
func Upgrade(req *web.Request) (conn *Conn, err os.Error) {

	netConn, buf, err := req.Responder.Hijack()
	if err != nil {
		panic("twister.websocket: hijack failed")
		return nil, err
	}

	defer func() {
		if netConn != nil {
			netConn.Close()
		}
	}()

	var r io.Reader
	if len(buf) > 0 {
		r = io.MultiReader(bytes.NewBuffer(buf), netConn)
	} else {
		r = netConn
	}
	br := bufio.NewReader(r)
	bw := bufio.NewWriter(netConn)

	if req.Method != "GET" {
		return nil, os.NewError("twister.websocket: bad request method")
	}

	origin, found := req.Header.Get(web.HeaderOrigin)
	if !found {
		return nil, os.NewError("twister.websocket: origin missing")
	}

	connection := strings.ToLower(req.Header.GetDef(web.HeaderConnection, ""))
	if connection != "upgrade" {
		return nil, os.NewError("twister.websocket: connection header missing or wrong value")
	}

	upgrade := strings.ToLower(req.Header.GetDef(web.HeaderUpgrade, ""))
	if upgrade != "websocket" {
		return nil, os.NewError("twister.websocket: upgrade header missing or wrong value")
	}

	key1, err := webSocketKey(req, web.HeaderSecWebSocketKey1)
	if err != nil {
		return nil, err
	}

	key2, err := webSocketKey(req, web.HeaderSecWebSocketKey2)
	if err != nil {
		return nil, err
	}

	key3 := make([]byte, 8)
	if _, err := io.ReadFull(br, key3); err != nil {
		return nil, err
	}

	h := md5.New()
	h.Write(key1)
	h.Write(key2)
	h.Write(key3)
	response := h.Sum()

	// TODO: handle tls
	location := "ws://" + req.URL.Host + req.URL.RawPath
	protocol := req.Header.GetDef(web.HeaderSecWebSocketProtocol, "")

	bw.WriteString("HTTP/1.1 101 WebSocket Protocol Handshake")
	bw.WriteString("\r\nUpgrade: WebSocket")
	bw.WriteString("\r\nConnection: Upgrade")
	bw.WriteString("\r\nSec-WebSocket-Location: ")
	bw.WriteString(location)
	bw.WriteString("\r\nSec-WebSocket-Origin: ")
	bw.WriteString(origin)
	if len(protocol) > 0 {
		bw.WriteString("\r\nSec-WebSocket-Protocol: ")
		bw.WriteString(protocol)
	}
	bw.WriteString("\r\n\r\n")
	bw.Write(response)

	if err := bw.Flush(); err != nil {
		return nil, err
	}

	conn = &Conn{netConn, br, bw}
	netConn = nil
	return conn, nil
}
