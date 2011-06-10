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

package main

import (
	"flag"
	"fmt"
	"github.com/garyburd/twister/oauth"
	"github.com/garyburd/twister/server"
	"github.com/garyburd/twister/web"
	"http"
	"io/ioutil"
	"json"
	"log"
	"os"
	"strings"
	"template"
)

var oauthClient = oauth.Client{
	TemporaryCredentialRequestURI: "http://api.twitter.com/oauth/request_token",
	ResourceOwnerAuthorizationURI: "http://api.twitter.com/oauth/authenticate",
	TokenRequestURI:               "http://api.twitter.com/oauth/access_token",
}

// credentialsCookie encodes OAuth credentials to a Set-Cookie header value.
func credentialsCookie(name string, c *oauth.Credentials, maxAgeDays int) string {
	return web.NewCookie(name, http.URLEscape(c.Token)+"/"+http.URLEscape(c.Secret)).
		MaxAgeDays(maxAgeDays).
		String()
}

// credentials returns oauth credentials stored in cookie with name key.
func credentials(req *web.Request, key string) (*oauth.Credentials, os.Error) {
	s := req.Cookie.Get(key)
	if s == "" {
		return nil, os.NewError("main: missing cookie")
	}
	a := strings.Split(s, "/", -1)
	if len(a) != 2 {
		return nil, os.NewError("main: bad credential cookie")
	}
	token, err := http.URLUnescape(a[0])
	if err != nil {
		return nil, os.NewError("main: bad credential cookie")
	}
	secret, err := http.URLUnescape(a[1])
	if err != nil {
		return nil, os.NewError("main: bad credential cookie")
	}
	return &oauth.Credentials{token, secret}, nil
}

// login redirects the user to the Twitter authorization page.
func login(req *web.Request) {
	callback := req.URL.Scheme + "://" + req.URL.Host + "/callback"
	temporaryCredentials, err := oauthClient.RequestTemporaryCredentials(http.DefaultClient, callback)
	if err != nil {
		req.Error(web.StatusInternalServerError, err)
		return
	}
	req.Redirect(oauthClient.AuthorizationURL(temporaryCredentials), false,
		web.HeaderSetCookie, credentialsCookie("tmp", temporaryCredentials, 0))
}

// authCallback handles OAuth callbacks from Twitter.
func authCallback(req *web.Request) {
	temporaryCredentials, err := credentials(req, "tmp")
	if err != nil {
		req.Error(web.StatusNotFound, err)
		return
	}
	s := req.Param.Get("oauth_token")
	if s == "" {
		req.Error(web.StatusNotFound, os.NewError("main: no token"))
		return
	}
	if s != temporaryCredentials.Token {
		req.Error(web.StatusNotFound, os.NewError("main: token mismatch"))
		return
	}
	tokenCredentials, _, err := oauthClient.RequestToken(http.DefaultClient, temporaryCredentials, req.Param.Get("oauth_verifier"))
	if err != nil {
		req.Error(web.StatusNotFound, err)
		return
	}
	req.Redirect("/", false,
		web.HeaderSetCookie, credentialsCookie("tok", tokenCredentials, 30),
		web.HeaderSetCookie, web.NewCookie("tmp", "").Delete().String())
}

// homeLoggedOut handles request to the home page for logged out users.
func homeLoggedOut(req *web.Request) {
	homeLoggedOutTempl.Execute(
		req.Respond(web.StatusOK, web.HeaderContentType, web.ContentTypeHTML),
		req)
}

// home handles requests to the home page.
func home(req *web.Request) {
	token, err := credentials(req, "tok")
	if err != nil {
		homeLoggedOut(req)
		return
	}
	param := make(web.Values)
	url := "http://api.twitter.com/1/statuses/home_timeline.json"
	oauthClient.SignParam(token, "GET", url, param)
	url = url + "?" + param.FormEncodedString()
	resp, _, err := http.Get(url)
	if err != nil {
		req.Error(web.StatusInternalServerError, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		req.Error(web.StatusInternalServerError, os.NewError(fmt.Sprint("Status ", resp.StatusCode)))
		return
	}
	var d interface{}
	err = json.NewDecoder(resp.Body).Decode(&d)
	if err != nil {
		req.Error(web.StatusInternalServerError, err)
		return
	}
	homeTempl.Execute(req.Respond(web.StatusOK, web.HeaderContentType, web.ContentTypeHTML), d)
}

func readSettings() {
	b, err := ioutil.ReadFile("settings.json")
	if err != nil {
		log.Fatal("could not read settings.json", err)
	}
	var m map[string]interface{}
	err = json.Unmarshal(b, &m)
	if err != nil {
		log.Fatal("could not unmarhal settings.json", err)
	}
	oauthClient.Credentials.Token = m["ClientToken"].(string)
	oauthClient.Credentials.Secret = m["ClientSecret"].(string)
}

func main() {
	flag.Parse()
	readSettings()
	h := web.FormHandler(10000, true, web.NewRouter().
		Register("/", "GET", home).
		Register("/login", "GET", login).
		Register("/callback", "GET", authCallback))

	server.Run(":8080", h)
}

var fmap = template.FormatterMap{"": template.HTMLFormatter}
var homeLoggedOutTempl = template.MustParse(homeLoggedOutStr, fmap)

const homeLoggedOutStr = `
<html>
<head>
</head>
<body>
<a href="/login"><img src="http://a0.twimg.com/images/dev/buttons/sign-in-with-twitter-d.png"></a>
</body>
</html>`

var homeTempl = template.MustParse(homeStr, fmap)

const homeStr = `
<html>
<head>
</head>
<body>
{.repeated section @}
<p>{.section user}<b>{screen_name}</b> {.end}{text}
{.end}
</body>`
