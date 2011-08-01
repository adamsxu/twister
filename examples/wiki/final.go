// This is the Go web application example (http://golang.org/doc/codelab/wiki/) 
// converted to use Twister instead of the Go http package.
package main

import (
	"github.com/garyburd/twister/server"
	"github.com/garyburd/twister/web"
	"io/ioutil"
	"log"
	"os"
	"template"
)

type page struct {
	Title string
	Body  []byte
}

func (p *page) save() os.Error {
	filename := p.Title + ".txt"
	return ioutil.WriteFile(filename, p.Body, 0600)
}

func loadPage(title string) (*page, os.Error) {
	filename := title + ".txt"
	body, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return &page{Title: title, Body: body}, nil
}

func viewHandler(req *web.Request) {
	title := req.URLParam["title"]
	p, err := loadPage(title)
	if err != nil {
		req.Redirect("/edit/"+title, false)
		return
	}
	renderTemplate(req, "view", p)
}

func editHandler(req *web.Request) {
	title := req.Param.Get("title")
	p, err := loadPage(title)
	if err != nil {
		p = &page{Title: req.Param.Get("title")}
	}
	renderTemplate(req, "edit", p)
}

func saveHandler(req *web.Request) {
	body := req.Param.Get("body")
	title := req.Param.Get("title")
	p := &page{Title: title, Body: []byte(body)}
	err := p.save()
	if err != nil {
		req.Error(web.StatusInternalServerError, err)
		return
	}
	req.Redirect("/view/"+title, false)
}

var templates = make(map[string]*template.Template)

func init() {
	for _, tmpl := range []string{"edit", "view"} {
		templates[tmpl] = template.MustParseFile(tmpl+".html", nil)
	}
}

func renderTemplate(req *web.Request, tmpl string, p *page) {
	err := templates[tmpl].Execute(
		req.Respond(web.StatusOK),
		map[string]interface{}{
			"page": p,
			"xsrf": req.Param.Get("xsrf"),
		})
	if err != nil {
		log.Println("error rendering", tmpl, err)
	}
}

func main() {
	const titleParam = "<title:[a-zA-Z0-9]+>"
	h := web.FormHandler(10000, true, // limit size of form to 10k, enable xsrf
		web.NewRouter().
			Register("/view/"+titleParam, "GET", viewHandler).
			Register("/edit/"+titleParam, "GET", editHandler, "POST", saveHandler))
	server.Run(":8080", h)
}
