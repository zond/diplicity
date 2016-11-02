package diptest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/davecgh/go-spew/spew"
	"github.com/gorilla/mux"
	"github.com/jmoiron/jsonq"
	"github.com/zond/diplicity/auth"
	"github.com/zond/diplicity/routes"
	"google.golang.org/appengine/aetest"
)

var (
	instance aetest.Instance
	router   = mux.NewRouter()
	counter  uint64
)

func String(s string) string {
	c := atomic.AddUint64(&counter, 1)
	return fmt.Sprintf("%s-%d", s, c)
}

func init() {
	inst, err := aetest.NewInstance(&aetest.Options{StronglyConsistentDatastore: true})
	if err != nil {
		panic(fmt.Errorf("when starting test instance: %v", err))
	}
	instance = inst
	routes.Setup(router)
	auth.TestMode = true
}

func NewEnv() *Env {
	return (&Env{})
}

type Env struct {
	uid string
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

func (e *Env) GetRoute(route string) *Get {
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
	Env      *Env
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
	return r.AssertSliceStringEq(rel, []string{"Rel"}, path...)
}

func (r *Result) AssertSliceStringEq(val string, inSlicePath []string, path ...string) *Result {
	ary, err := jsonq.NewQuery(r.Body).ArrayOfObjects(path...)
	if err != nil {
		panic(fmt.Errorf("looking for %+v in %+v: %v", path, r.Body, err))
	}
	found := false
	for _, obj := range ary {
		if foundString, err := jsonq.NewQuery(obj).String(inSlicePath...); err == nil && foundString == val {
			found = true
			break
		}
	}
	if !found {
		panic(fmt.Errorf("found no %+v = %q in %v", inSlicePath, val, spew.Sdump(r.Body)))
	}
	return r
}

func (r *Result) FollowPOST(body interface{}, rel string, path ...string) *Result {
	ary, err := jsonq.NewQuery(r.Body).ArrayOfObjects(path...)
	if err != nil {
		panic(fmt.Errorf("looking for %+v in %+v: %v", path, r.Body, err))
	}
	for _, obj := range ary {
		if rel == obj["Rel"] {
			u, err := url.Parse(obj["URL"].(string))
			if err != nil {
				panic(fmt.Errorf("trying to parse %q: %v", obj["URL"].(string), err))
			}
			return r.Env.POSTURL(body, u)
		}
	}
	panic(fmt.Errorf("found no Rel %q in %+v", rel, ary))
}

func (r *Result) FollowGET(rel string, path ...string) *Result {
	ary, err := jsonq.NewQuery(r.Body).ArrayOfObjects(path...)
	if err != nil {
		panic(fmt.Errorf("looking for %+v in %+v: %v", path, r.Body, err))
	}
	for _, obj := range ary {
		if rel == obj["Rel"] {
			u, err := url.Parse(obj["URL"].(string))
			if err != nil {
				panic(fmt.Errorf("trying to parse %q: %v", obj["URL"].(string), err))
			}
			return r.Env.GetURL(u)
		}
	}
	panic(fmt.Errorf("found no Rel %q in %+v", rel, ary))
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
	u, err := router.Get(g.route).URL(g.routeParams...)
	if err != nil {
		panic(fmt.Errorf("creating URL for %q and %+v: %v", g.route, g.routeParams, err))
	}
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
	return g.env.GetURL(u)
}

func (e *Env) POSTURL(body interface{}, u *url.URL) *Result {
	b := &bytes.Buffer{}
	if err := json.NewEncoder(b).Encode(body); err != nil {
		panic(fmt.Errorf("encoding %+v to JSON: %v", body, err))
	}
	req, err := instance.NewRequest("POST", u.String(), b)
	if err != nil {
		panic(fmt.Errorf("creating GET %q: %v", u, err))
	}
	req.Header.Set("Accept", "application/json; charset=utf-8")
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rw := NewResponseWriter()
	router.ServeHTTP(rw, req)
	responseBytes := rw.Body.Bytes()
	var result interface{}
	if err := json.Unmarshal(responseBytes, &result); err != nil {
		panic(fmt.Errorf("unmarshaling %q: %v", string(responseBytes), err))
	}
	return &Result{
		Env:      e,
		URL:      u,
		Body:     result,
		Response: rw,
	}
}

func (e *Env) GetURL(u *url.URL) *Result {
	req, err := instance.NewRequest("GET", u.String(), nil)
	if err != nil {
		panic(fmt.Errorf("creating GET %q: %v", u, err))
	}
	req.Header.Set("Accept", "application/json; charset=utf-8")
	rw := NewResponseWriter()
	router.ServeHTTP(rw, req)
	body := rw.Body.Bytes()
	var result interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		panic(fmt.Errorf("unmarshaling %q: %v", string(body), err))
	}
	return &Result{
		Env:      e,
		URL:      u,
		Body:     result,
		Response: rw,
	}
}
