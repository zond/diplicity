package api

import (
	"net/http"

	"github.com/zond/goaeoas"
)

type GoaeoasRequest struct {
	req    *http.Request
	vars   map[string]string
	values map[string]interface{}
}

func (r *GoaeoasRequest) Req() *http.Request {
	return r.req
}

func (r *GoaeoasRequest) Vars() map[string]string {
	return r.vars
}

func (r *GoaeoasRequest) Values() map[string]interface{} {
	return r.values
}

func (r *GoaeoasRequest) DecorateLinks(links goaeoas.LinkDecorator) {
}

func (r *GoaeoasRequest) Media() string {
	return ""
}

func (r *GoaeoasRequest) NewLink(link goaeoas.Link) goaeoas.Link {
	return goaeoas.Link{}
}
