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

package web

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// ContentTypeHTML is the content type for UTF-8 encoded HTML.
const ContentTypeHTML = "text/html; charset=\"utf-8\""

// TimeLayout is the time layout used for HTTP headers and other values.
const TimeLayout = "Mon, 02 Jan 2006 15:04:05 GMT"

// FormatDeltaSeconds returns current time plus delta formatted per HTTP conventions.
func FormatDeltaSeconds(delta int) string {
	return time.SecondsToUTC(time.Seconds() + int64(delta)).Format(TimeLayout)
}

// FormatDeltaDays returns current time plus delta formatted per HTTP conventions.
func FormatDeltaDays(delta int) string {
	return FormatDeltaSeconds(delta * 60 * 60 * 24)
}

var (
	colonSpaceBytes   = []byte{':', ' '}
	crlfBytes         = []byte{'\r', '\n'}
	dashDashCrlfBytes = []byte{'-', '-', '\r', '\n'}
)

// HTTP status codes from RFC 2606
const (
	StatusContinue                     = 100
	StatusSwitchingProtocols           = 101
	StatusOK                           = 200
	StatusCreated                      = 201
	StatusAccepted                     = 202
	StatusNonAuthoritativeInformation  = 203
	StatusNoContent                    = 204
	StatusResetContent                 = 205
	StatusPartialContent               = 206
	StatusMultipleChoices              = 300
	StatusMovedPermanently             = 301
	StatusFound                        = 302
	StatusSeeOther                     = 303
	StatusNotModified                  = 304
	StatusUseProxy                     = 305
	StatusTemporaryRedirect            = 307
	StatusBadRequest                   = 400
	StatusUnauthorized                 = 401
	StatusPaymentRequired              = 402
	StatusForbidden                    = 403
	StatusNotFound                     = 404
	StatusMethodNotAllowed             = 405
	StatusNotAcceptable                = 406
	StatusProxyAuthenticationRequired  = 407
	StatusRequestTimeout               = 408
	StatusConflict                     = 409
	StatusGone                         = 410
	StatusLengthRequired               = 411
	StatusPreconditionFailed           = 412
	StatusRequestEntityTooLarge        = 413
	StatusRequestURITooLong            = 414
	StatusUnsupportedMediaType         = 415
	StatusRequestedRangeNotSatisfiable = 416
	StatusExpectationFailed            = 417
	StatusInternalServerError          = 500
	StatusNotImplemented               = 501
	StatusBadGateway                   = 502
	StatusServiceUnavailable           = 503
	StatusGatewayTimeout               = 504
	StatusHTTPVersionNotSupported      = 505
)

var statusText = map[int]string{
	StatusContinue:                     "Continue",
	StatusSwitchingProtocols:           "Switching Protocols",
	StatusOK:                           "OK",
	StatusCreated:                      "Created",
	StatusAccepted:                     "Accepted",
	StatusNonAuthoritativeInformation:  "Non-Authoritative Information",
	StatusNoContent:                    "No Content",
	StatusResetContent:                 "Reset Content",
	StatusPartialContent:               "Partial Content",
	StatusMultipleChoices:              "Multiple Choices",
	StatusMovedPermanently:             "Moved Permanently",
	StatusFound:                        "Found",
	StatusSeeOther:                     "See Other",
	StatusNotModified:                  "Not Modified",
	StatusUseProxy:                     "Use Proxy",
	StatusTemporaryRedirect:            "Temporary Redirect",
	StatusBadRequest:                   "Bad Request",
	StatusUnauthorized:                 "Unauthorized",
	StatusPaymentRequired:              "Payment Required",
	StatusForbidden:                    "Forbidden",
	StatusNotFound:                     "Not Found",
	StatusMethodNotAllowed:             "Method Not Allowed",
	StatusNotAcceptable:                "Not Acceptable",
	StatusProxyAuthenticationRequired:  "Proxy Authentication Required",
	StatusRequestTimeout:               "Request Timeout",
	StatusConflict:                     "Conflict",
	StatusGone:                         "Gone",
	StatusLengthRequired:               "Length Required",
	StatusPreconditionFailed:           "Precondition Failed",
	StatusRequestEntityTooLarge:        "Request Entity Too Large",
	StatusRequestURITooLong:            "Request URI Too Long",
	StatusUnsupportedMediaType:         "Unsupported Media Type",
	StatusRequestedRangeNotSatisfiable: "Requested Range Not Satisfiable",
	StatusExpectationFailed:            "Expectation Failed",
	StatusInternalServerError:          "Internal Server Error",
	StatusNotImplemented:               "Not Implemented",
	StatusBadGateway:                   "Bad Gateway",
	StatusServiceUnavailable:           "Service Unavailable",
	StatusGatewayTimeout:               "Gateway Timeout",
	StatusHTTPVersionNotSupported:      "HTTP Version Not Supported",
}

// StatusText returns a text description of an HTTP status code.
func StatusText(status int) string {
	s, found := statusText[status]
	if !found {
		s = "Status " + strconv.Itoa(status)
	}
	return s
}

// ProtocolVersion combines HTTP major and minor protocol numbers into a single
// integer for easy comparision of protocol versions.
func ProtocolVersion(major int, minor int) int {
	if minor > 999 {
		minor = 999
	}
	return major*1000 + minor
}

// Commonly used protocol versions in format returned by the ProtocolVersion
// function.
const (
	ProtocolVersion10 = 1000 // HTTP/1.0
	ProtocolVersion11 = 1001 // HTTP/1.1
)

// parseCookieValues parses cookies from values and adds them to m. The
// function supports the Netscape draft specification for cookies
// (http://goo.gl/1WSx3). 
func parseCookieValues(values []string, m Values) os.Error {
	for _, s := range values {
		key := ""
		begin := 0
		end := 0
		for i := 0; i < len(s); i++ {
			switch s[i] {
			case ' ', '\t':
				// leading whitespace?
				if begin == end {
					begin = i + 1
					end = begin
				}
			case '=':
				if key == "" {
					key = s[begin:end]
					begin = i + 1
					end = begin
				} else {
					end += 1
				}
			case ';':
				if len(key) > 0 && begin < end {
					value := s[begin:end]
					m.Add(key, value)
				}
				key = ""
				begin = i + 1
				end = begin
			default:
				end = i + 1
			}
		}
		if len(key) > 0 && begin < end {
			m.Add(key, s[begin:end])
		}
	}
	return nil
}

func signature(secret, key, expiration, value string) string {
	hm := hmac.NewSHA1([]byte(secret))
	io.WriteString(hm, key)
	hm.Write([]byte{0})
	io.WriteString(hm, expiration)
	hm.Write([]byte{0})
	io.WriteString(hm, value)
	return hex.EncodeToString(hm.Sum())
}

// SignValue returns a string containing value, an expiration time and a
// signature. The expiration time is computed from the current time and
// maxAgeSeconds.  The signature is an HMAC SHA-1 signature of value, context
// and the expiration time. Use the function VerifyValue to extract the value,
// check the expiration time and verify the signature.
// 
// SignValue can be used to store credentials in a cookie:
//
//  var secret string // Initialized by application
//  const uidCookieMaxAge = 3600 * 24 * 30
//
//  // uidCookieValue returns the Set-Cookie header value containing a 
//  // signed and timestamped user id.
//  func uidCookieValue(uid string) string {
//      s := web.SignValue(secret, "uid", uidCookieMaxAge, uid)
//      return web.NewCookie("uid", s).MaxAge(uidCookieMaxAge).String()
//  }
//
//  // requestUid returns the user id from the request cookie. An error 
//  // is returned if the cookie is missing, the value has expired or the
//  // signature is not valid.
//  func requestUid(req *web.Request) (string, os.Error) {
//      return web.VerifyValue(secret, "uid", req.Cookie.Get("uid"))
//  }
func SignValue(secret, context string, maxAgeSeconds int, value string) string {
	expiration := strconv.Itob64(time.Seconds()+int64(maxAgeSeconds), 16)
	sig := signature(secret, context, expiration, value)
	return sig + "~" + expiration + "~" + value
}

var errVerificationFailure = os.NewError("verification failed")

// VerifyValue extracts a value from a string created by SignValue. An error is
// returned if the expiration time has elapsed or the signature is not correct.
func VerifyValue(secret, context string, signedValue string) (string, os.Error) {
	a := strings.Split(signedValue, "~", 3)
	if len(a) != 3 {
		return "", errVerificationFailure
	}
	expiration, err := strconv.Btoi64(a[1], 16)
	if err != nil || expiration < time.Seconds() {
		return "", errVerificationFailure
	}
	expectedSig := signature(secret, context, a[1], a[2])
	actualSig := a[0]
	if len(actualSig) != len(expectedSig) {
		return "", errVerificationFailure
	}
	// Constant time compare
	var v byte
	for i := 0; i < len(actualSig); i++ {
		v |= actualSig[i] ^ expectedSig[i]
	}
	if subtle.ConstantTimeByteEq(v, 0) != 1 {
		return "", errVerificationFailure
	}
	return a[2], nil
}

// Cookie is a helper for constructing Set-Cookie header values. 
// 
// Cookie supports the ancient Netscape draft specification for cookies
// (http://goo.gl/1WSx3) and the modern HttpOnly attribute
// (http://www.owasp.org/index.php/HttpOnly). Cookie does not attempt to
// support any RFC for cookies because the RFCs are not supported by popular
// browsers.
//
// As a convenience, the NewCookie function returns a cookie with the path
// attribute set to "/" and the httponly attribute set to true. 
//
// The following example shows how to set a cookie header using Cookie:
//
//  func myHandler(req *web.Request) {
//      c := web.NewCookie("my-cookie-name", "my-cookie-value").String()
//      w := req.Respond(web.StatusOK, web.HeaderSetCookie, c)
//      io.WriteString(w, "<html><body>Hello</body></html>")
//  }
type Cookie struct {
	name     string
	value    string
	path     string
	domain   string
	maxAge   int
	secure   bool
	httpOnly bool
}

// NewCookie returns a new cookie with the given name and value, the path
// attribute set to "/" and the httponly attribute set to true.
func NewCookie(name, value string) *Cookie {
	return &Cookie{name: name, value: value, path: "/", httpOnly: true}
}

// Path sets the cookie path attribute. The path must either be "" or start with a
// '/'.  The NewCookie function initializes the path to "/". If the path is "",
// then the path attribute is not included in the header value. 
func (c *Cookie) Path(path string) *Cookie { c.path = path; return c }

// Domain sets the cookie domain attribute. If the host is "", then the domain
// attribute is not included in the header value. 
func (c *Cookie) Domain(domain string) *Cookie { c.domain = domain; return c }

// MaxAge specifies the maximum age for a cookie. The age is converted to an
// absolute expiration time when the header value is rendered. If the maximum
// age is 0, then the expiration time is not included in the header value
// and the browser will handle the cookie as a "session" cookie.
func (c *Cookie) MaxAge(seconds int) *Cookie { c.maxAge = seconds; return c }

// MaxAgeDays sets the maximum age for the cookie in days.
func (c *Cookie) MaxAgeDays(days int) *Cookie { return c.MaxAge(days * 60 * 60 * 24) }

// Delete sets the expiration date to a time in the past. 
func (c *Cookie) Delete() *Cookie { return c.MaxAgeDays(-30).HTTPOnly(false) }

// Secure sets the secure attribute. 
func (c *Cookie) Secure(secure bool) *Cookie { c.secure = secure; return c }

// HTTPOnly sets the httponly attribute. The NewCookie function
// initializes the httponly attribute to true.
func (c *Cookie) HTTPOnly(httpOnly bool) *Cookie {
	c.httpOnly = httpOnly
	return c
}

// String renders the Set-Cookie header value as a string.
func (c *Cookie) String() string {
	var buf bytes.Buffer

	buf.WriteString(c.name)
	buf.WriteByte('=')
	buf.WriteString(c.value)

	if c.path != "" {
		buf.WriteString("; path=")
		buf.WriteString(c.path)
	}

	if c.domain != "" {
		buf.WriteString("; domain=")
		buf.WriteString(c.domain)
	}

	if c.maxAge != 0 {
		buf.WriteString("; expires=")
		buf.WriteString(FormatDeltaSeconds(c.maxAge))
	}

	if c.secure {
		buf.WriteString("; secure")
	}

	if c.httpOnly {
		buf.WriteString("; HttpOnly")
	}

	return buf.String()
}

// HTMLEscapeString returns s with special HTML characters escaped. 
func HTMLEscapeString(s string) string {
	escape := false
	for i := 0; i < len(s); i++ {
		if c := s[i]; c == '"' || c == '\'' || c == '/' || c == '&' || c == '<' || c == '>' {
			escape = true
			break
		}
	}
	if !escape {
		return s
	}
	var b bytes.Buffer
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"':
			b.WriteString("&quot;")
		case '\'':
			// &apos; is not defined in the HTML standard
			b.WriteString("&#x27;")
		case '/':
			// forward slash is included as it helps end an HTML entity
			b.WriteString("&#x2F;")
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}


// CheckXSRF implements cross-site request forgery protection. Here's how it works:
// 
// CheckXSRF sets a cookie with name cookieName to a random token.
//
// The application ensures that POSTed forms include a parameter with name
// paramName and value equal to the token.
//
// POSTed forms are considered valid if the cookieName cookie is set and is
// equal to the paramName request parameter. A third party site cannot generate
// a request where the cookie and request parameter are equal because the third
// party site cannot access the cookie value.
//
// CheckXSRF returns an error if the request is not valid. It is the applications's 
// responsiblity to respond to the request with an appropriate error.
//
// Before returning, CheckXSRF ensures that the paramName request parameter is
// set to the token. The application should use the value of the paramName
// parameter when generating hidden fields in POSTed forms.
//
// CheckXSRF also validates PUT and DELETE requests. 
//
// The X-XSRFToken can be used to specifiy the token in addition to the
// paramName request parameter.
//
// See http://en.wikipedia.org/wiki/Cross-site_request_forgery for information
// on cross-site request forgery.
func CheckXSRF(req *Request, cookieName string, paramName string) os.Error {

	const tokenLen = 8
	expectedToken := req.Cookie.Get(cookieName)

	// Create new XSRF token?
	if len(expectedToken) != tokenLen {
		p := make([]byte, tokenLen/2)
		_, err := rand.Reader.Read(p)
		if err != nil {
			panic("twister: rand read failed")
		}
		expectedToken = hex.EncodeToString(p)
		c := NewCookie(cookieName, expectedToken).String()
		FilterRespond(req, func(status int, header Header) (int, Header) {
			header.Add(HeaderSetCookie, c)
			return status, header
		})
	}

	actualToken := req.Param.Get(paramName)
	if actualToken == "" {
		actualToken = req.Header.Get(HeaderXXSRFToken)
		req.Param.Set(paramName, expectedToken)
	}
	if expectedToken != actualToken {
		req.Param.Set(paramName, expectedToken)
		if req.Method == "POST" ||
			req.Method == "PUT" ||
			req.Method == "DELETE" {
			err := os.NewError("twister: bad xsrf token")
			if actualToken == "" {
				err = os.NewError("twister: missing xsrf token")
			}
			return err
		}
	}
	return nil
}
