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

package websocket

import (
	"bufio"
	"bytes"
	"github.com/garyburd/twister/web"
	"io/ioutil"
	"testing"
)

func testHandler(req *web.Request) {
	c, err := Upgrade(req, 8, 1024, nil)
	if err != nil {
		return
	}
	defer c.Close()
	for {
		var a []byte
		for {
			m, hasMore, err := c.ReadMessage()
			if err != nil {
				return
			}
			a = append(a, m...)
			if !hasMore {
				break
			}
		}
		err := c.WriteMessage(a)
		if err != nil {
			return
		}
	}
}

var webSocketTests = []struct {
	in     string
	header web.Header
	fail   bool
}{
	{in: "", fail: true},
	{
		header: web.NewHeader(
			"Connection", "Upgrade",
			"Origin", "http://localhost:8080",
			"Host", "localhost:8080",
			"Upgrade", "WebSocket",
			"Sec-Websocket-Key2", "z 4 d0 3 0a>mU 7N 1@991HP I {2",
			"Sec-Websocket-Key1", "284<qQA84i92708  /"),
		in: "P\u05e4>mX\x18k",
	},
	{
		header: web.NewHeader(
			"Connection", "Upgrade",
			"Origin", "http://localhost:8080",
			"Host", "localhost:8080",
			"Upgrade", "WebSocket",
			"Sec-Websocket-Key2", "z 4 d0 3 0a>mU 7N 1@991HP I {2",
			"Sec-Websocket-Key1", "284<qQA84i92708  /"),
		in: "P\u05e4>mX\x18k\x00Hello\xff",
	},
	{
		header: web.NewHeader(
			"Connection", "Upgrade",
			"Origin", "http://localhost:8080",
			"Host", "localhost:8080",
			"Upgrade", "WebSocket",
			"Sec-Websocket-Key2", "z 4 d0 3 0a>mU 7N 1@991HP I {2",
			"Sec-Websocket-Key1", "284<qQA84i92708  /"),
		in: "P\u05e4>mX\x18k\x00Now is the time for a very long message.\xff\x00short\xff",
	},
}

func TestWebSocket(t *testing.T) {
	for _, tt := range webSocketTests {
		var test bytes.Buffer
		tt.header.WriteHttpHeader(&test)

		status, _, out := web.RunHandler("http://example.com/", "GET", tt.header, []byte(tt.in), web.HandlerFunc(testHandler))

		fail := status >= 400
		if fail != tt.fail {
			t.Errorf("%q, fail=%v, want %v; status %d", test.String(), fail, tt.fail, status)
			continue
		}

		if tt.fail {
			continue
		}

		br := bufio.NewReader(bytes.NewBuffer(out))
		br.ReadSlice('\n') // TODO: check correctness of status line
		header := make(web.Header)
		err := header.ParseHttpHeader(br)
		if err != nil {
			t.Errorf("%q, out=%q, header parse error %v", test.String(), string(out), err)
			continue
		}
		out, err = ioutil.ReadAll(br)
		if len(out) < 16 {
			t.Errorf("%q, expect 16 byte response, got %d", test.String(), len(out))
			continue
		}
		// TODO: check correctness of response.
		in := tt.in[8:] // remove key3
		out = out[16:]  // remove response

		// We expect the input to equal the output because the handler echoes
		// the messages.
		if string(out) != in {
			t.Errorf("%q, got %q", in, string(out))
		}
	}
}
