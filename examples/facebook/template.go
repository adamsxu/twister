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
	"path"
	"log"
	"mime"
	"template"
	"io"
	"github.com/garyburd/twister/web"
)

// itemFormatter formats a Facebook feed item by invoking the appropriate child
// template.
func itemFormatter(w io.Writer, value interface{}, format string) {
	itemType := value.(map[string]interface{})["type"].(string)
	t, ok := itemTemplates[itemType]
	if !ok {
		t = otherItemTemplate
	}
	err := t.t.Execute(value, w)
	if err != nil {
		log.Println("Error executing item formatter", itemType, err)
	}
}

type Template struct {
	t        *template.Template
	mimeType string
}

func parseTemplate(filename string) *Template {
	return &Template{
		t:        template.MustParseFile(path.Join("template", filename), formatterMap),
		mimeType: mime.TypeByExtension(path.Ext(filename))}
}

func (t *Template) respond(req *web.Request, status int, value interface{}, kvs ...string) {
	header := web.NewStringsMap(kvs...)
	header.Set(web.HeaderContentType, t.mimeType)
	err := t.t.Execute(value, req.Responder.Respond(status, header))
	if err != nil {
		log.Println("Error executing template", err)
	}
}

var (
	formatterMap = template.FormatterMap{
		"":     template.HTMLFormatter,
		"item": template.HTMLFormatter, // temp value to break init cycle
	}
	homeTemplate      = parseTemplate("home.html")
	loggedOutTemplate = parseTemplate("loggedout.html")
	otherItemTemplate = parseTemplate("item/other.html")
	itemTemplates     = map[string]*Template{
		"link": parseTemplate("item/link.html"),
		"status": parseTemplate("item/status.html"),
	}
)

func init() {
	formatterMap["item"] = itemFormatter
}