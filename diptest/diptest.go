package diptest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/mux"
	"github.com/jmoiron/jsonq"
	"github.com/zond/diplicity/auth"
	"github.com/zond/diplicity/routes"
)

var Router = mux.NewRouter()

func init() {
	auth.TestMode = true
	routes.Setup(Router)
}

func NewEnv() *Env {
	return (&Env{
		host: "localhost:8080",
	})
}

type Env struct {
	host string
	uid  string
}

func (e *Env) UID(uid string) *Env {
	e.uid = uid
	return e
}

type Get struct {
	env         *Env
	route       string
	routeParams []string
	queryParams url.Values
}

func (e *Env) Get(route string) *Get {
	return &Get{
		env:   e,
		route: route,
	}
}

func (g *Get) RouteParams(routeParams ...string) *Get {
	g.routeParams = routeParams
	return g
}

func (g *Get) QueryParams(queryParams url.Values) *Get {
	g.queryParams = queryParams
	return g
}

type Result struct {
	Get      *Get
	URL      *url.URL
	Body     interface{}
	Response *ResponseWriter
}

func (r *Result) AssertOK() *Result {
	if r.Response.StatusCode != 200 {
		panic(fmt.Errorf("fetching %q: %v", r.URL, r.Response.StatusCode))
	}
	return r
}

func (r *Result) AssertStringEq(val string, path ...string) *Result {
	if found, err := jsonq.NewQuery(r.Body).String(path...); err != nil {
		panic(fmt.Errorf("looking for %+v in %+v: %v", path, r.Body, err))
	} else if found != val {
		panic(fmt.Errorf("got %+v = %q, want %q", path, found, val))
	}
	return r
}

func (r *Result) AssertNil(path ...string) *Result {
	if val, err := jsonq.NewQuery(r.Body).String(path...); err == nil {
		panic(fmt.Errorf("wanted nil at %+v, got %q", path, val))
	} else if !strings.Contains(err.Error(), "Nil value found") {
		panic(fmt.Errorf("wanted nil at %+v: %v", err))
	}
	return r
}

func (r *Result) AssertRel(rel string, path ...string) *Result {
	ary, err := jsonq.NewQuery(r.Body).ArrayOfObjects(path...)
	if err != nil {
		panic(fmt.Errorf("looking for %+v in %+v: %v", path, r.Body, err))
	}
	found := false
	for _, obj := range ary {
		if rel == obj["Rel"] {
			found = true
			break
		}
	}
	if !found {
		panic(fmt.Errorf("found no Rel %q in %+v", rel, ary))
	}
	return r
}

type ResponseWriter struct {
	Body       *bytes.Buffer
	HTTPHeader http.Header
	StatusCode int
}

func NewResponseWriter() *ResponseWriter {
	return &ResponseWriter{
		Body:       &bytes.Buffer{},
		HTTPHeader: http.Header{},
		StatusCode: 200,
	}
}

func (rw *ResponseWriter) Header() http.Header {
	return rw.HTTPHeader
}

func (rw *ResponseWriter) WriteHeader(i int) {
	rw.StatusCode = i
}

func (rw *ResponseWriter) Write(b []byte) (int, error) {
	return rw.Body.Write(b)
}

func (g *Get) Do() *Result {
	u, err := Router.Get(g.route).URL(g.routeParams...)
	if err != nil {
		panic(fmt.Errorf("creating URL for %q and %+v: %v", g.route, g.routeParams, err))
	}
	u.Host = g.env.host
	u.Scheme = "http"
	var queryParams url.Values
	if g.queryParams == nil {
		queryParams = url.Values{}
	} else {
		queryParams = g.queryParams
	}
	if g.env.uid != "" {
		queryParams.Set("fake-id", g.env.uid)
	}
	u.RawQuery = queryParams.Encode()
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		panic(fmt.Errorf("creating GET %q: %v", u, err))
	}
	req.Header.Set("Accept", "application/json; charset=utf-8")
	rw := NewResponseWriter()
	Router.ServeHTTP(rw, req)
	body := rw.Body.Bytes()
	var result interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		panic(fmt.Errorf("unmarshaling %q: %v", string(body), err))
	}
	return &Result{
		Get:      g,
		URL:      u,
		Body:     result,
		Response: rw,
	}
}
