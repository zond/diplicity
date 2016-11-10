package diptest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/gorilla/mux"
	"github.com/jmoiron/jsonq"
	"github.com/kr/pretty"
	"github.com/zond/diplicity/auth"
	"github.com/zond/diplicity/routes"
	"google.golang.org/appengine/aetest"
)

type aetestTransport struct {
	instance aetest.Instance
}

func (a *aetestTransport) Request(method string, url string, body io.Reader) (*http.Request, error) {
	return a.instance.NewRequest(method, url, body)
}

type responseWriter struct {
	status int
	body   *bytes.Buffer
	header http.Header
}

func (r *responseWriter) Header() http.Header {
	return r.header
}

func (r *responseWriter) WriteHeader(i int) {
	r.status = i
}

func (r *responseWriter) Write(b []byte) (int, error) {
	return r.body.Write(b)
}

func (a *aetestTransport) Execute(req *http.Request) (int, http.Header, io.Reader, error) {
	rw := &responseWriter{
		body:   &bytes.Buffer{},
		header: http.Header{},
		status: 200,
	}
	router.ServeHTTP(rw, req)
	return rw.status, rw.header, rw.body, nil
}

type realTransport struct {
	host   string
	scheme string
	client *http.Client
}

func (r *realTransport) Request(method string, u string, body io.Reader) (*http.Request, error) {
	parsedURL, err := url.Parse(u)
	if err != nil {
		return nil, err
	}
	parsedURL.Host = r.host
	parsedURL.Scheme = r.scheme
	return http.NewRequest(method, parsedURL.String(), body)
}

func (r *realTransport) Execute(req *http.Request) (int, http.Header, io.Reader, error) {
	resp, err := r.client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	return resp.StatusCode, resp.Header, resp.Body, nil
}

type Transport interface {
	Request(method string, url string, body io.Reader) (*http.Request, error)
	Execute(req *http.Request) (status int, header http.Header, body io.Reader, err error)
}

var (
	counter   uint64
	startTime = time.Now().UnixNano()
	router    = mux.NewRouter()
)

func init() {
	routes.Setup(router)
	if os.Getenv("TRANSPORT") == "inprocess" {
		// This used to work, but then it stopped working.
		// Don't know why :/
		instance, err := aetest.NewInstance(&aetest.Options{
			StronglyConsistentDatastore: true,
		})
		if err != nil {
			panic(fmt.Errorf("trying to create aetest instance: %v", err))
		}
		T = &aetestTransport{
			instance: instance,
		}
	} else {
		T = &realTransport{
			host:   "localhost:8080",
			scheme: "http",
			client: &http.Client{},
		}
		auth.TestMode = true
	}
}

var (
	T Transport
)

func String(s string) string {
	c := atomic.AddUint64(&counter, 1)
	return fmt.Sprintf("%s-%d-%d", s, startTime, c)
}

func NewEnv() *Env {
	return (&Env{})
}

type Env struct {
	uid string
}

func (e *Env) GetUID() string {
	return e.uid
}

func (e *Env) SetUID(uid string) *Env {
	e.uid = uid
	return e
}

type Req struct {
	env         *Env
	route       string
	routeParams []string
	queryParams url.Values
	url         *url.URL
	method      string
	body        []byte
}

func (e *Env) GetRoute(route string) *Req {
	return &Req{
		env:    e,
		route:  route,
		method: "GET",
	}
}

func (r *Req) RouteParams(routeParams ...string) *Req {
	r.routeParams = routeParams
	return r
}

func (r *Req) QueryParams(queryParams url.Values) *Req {
	r.queryParams = queryParams
	return r
}

func (r *Req) Body(i interface{}) *Req {
	b, err := json.Marshal(i)
	if err != nil {
		panic(fmt.Errorf("trying to encode %v: %v", spew.Sdump(i), err))
	}
	r.body = b
	return r
}

type Result struct {
	Env       *Env
	URL       *url.URL
	Body      interface{}
	BodyBytes []byte
	Status    int
}

func (r *Result) GetValue(path ...string) interface{} {
	found, err := jsonq.NewQuery(r.Body).Interface(path...)
	if err != nil {
		panic(fmt.Errorf("looking for %+v in %v: %v", path, pp(r.Body), err))
	}
	return found
}

func (r *Result) AssertEq(val interface{}, path ...string) *Result {
	if found, err := jsonq.NewQuery(r.Body).Interface(path...); err != nil {
		panic(fmt.Errorf("looking for %+v in %v: %v", path, pp(r.Body), err))
	} else if diff := pretty.Diff(found, val); len(diff) > 0 {
		panic(fmt.Errorf("got %+v = %v, want %v; diff %v", path, found, val, spew.Sdump(diff)))
	}
	return r
}

func (r *Result) AssertBoolEq(val bool, path ...string) *Result {
	if found, err := jsonq.NewQuery(r.Body).Bool(path...); err != nil {
		panic(fmt.Errorf("looking for %+v in %v: %v", path, pp(r.Body), err))
	} else if found != val {
		panic(fmt.Errorf("got %+v = %v, want %v", path, found, val))
	}
	return r
}

func (r *Result) AssertEmpty(path ...string) *Result {
	if val, err := jsonq.NewQuery(r.Body).ArrayOfObjects(path...); err != nil {
		panic(fmt.Errorf("looking for %+v in %v: %v", path, pp(r.Body), err))
	} else if len(val) > 0 {
		panic(fmt.Errorf("got %+v = %+v, want empty", path, val))
	}
	return r
}

func (r *Result) AssertLen(l int, path ...string) *Result {
	if val, err := jsonq.NewQuery(r.Body).ArrayOfObjects(path...); err != nil {
		panic(fmt.Errorf("looking for %+v in %v: %v", path, pp(r.Body), err))
	} else if len(val) != l {
		panic(fmt.Errorf("got %+v = %+v, want length %v", path, val, l))
	}
	return r
}

func (r *Result) AssertNil(path ...string) *Result {
	if val, err := jsonq.NewQuery(r.Body).String(path...); err == nil {
		panic(fmt.Errorf("wanted nil at %+v in %v, got %q", path, pp(r.Body), val))
	} else if !strings.Contains(err.Error(), "Nil value found") {
		panic(fmt.Errorf("wanted nil at %+v: %v", err))
	}
	return r
}

func pp(i interface{}) string {
	b, err := json.MarshalIndent(i, "  ", "  ")
	if err != nil {
		panic(fmt.Errorf("trying to marshal %+v: %v", i, err))
	}
	return string(b)
}

func (r *Result) AssertNotFind(path []string, subPath []string, subMatch interface{}) *Result {
	ary, err := jsonq.NewQuery(r.Body).ArrayOfObjects(path...)
	if err != nil {
		panic(err)
	}
	for _, obj := range ary {
		found, err := jsonq.NewQuery(obj).Interface(subPath...)
		if err == nil && found == subMatch {
			panic(fmt.Errorf("found %+v with %+v = %q in %v", path, subPath, subMatch, pp(r.Body)))
		}
	}
	return r
}

func (r *Result) AssertNotRel(rel string, path ...string) *Result {
	r.AssertNotFind(path, []string{"Rel"}, rel)
	return r
}

func (r *Result) AssertRel(rel string, path ...string) *Result {
	r.Find(path, []string{"Rel"}, rel)
	return r
}

func (r *Result) Find(path []string, subPath []string, subMatch interface{}) *Result {
	ary, err := jsonq.NewQuery(r.Body).ArrayOfObjects(path...)
	if err != nil {
		panic(err)
	}
	for _, obj := range ary {
		found, err := jsonq.NewQuery(obj).Interface(subPath...)
		if err == nil && fmt.Sprint(found) == fmt.Sprint(subMatch) {
			cpy := *r
			cpy.Body = obj
			return &cpy
		}
	}
	panic(fmt.Errorf("Found no %+v with %+v = %v in %v", path, subPath, subMatch, pp(r.Body)))
}

func (r *Result) Follow(rel string, path ...string) *Req {
	ary, err := jsonq.NewQuery(r.Body).ArrayOfObjects(path...)
	if err != nil {
		panic(fmt.Errorf("looking for %+v in %v: %v", path, pp(r.Body), err))
	}
	for _, obj := range ary {
		if rel == obj["Rel"] {
			u, err := url.Parse(obj["URL"].(string))
			if err != nil {
				panic(fmt.Errorf("trying to parse %q: %v", obj["URL"].(string), err))
			}
			return &Req{
				env:    r.Env,
				url:    u,
				method: obj["Method"].(string),
			}
		}
	}
	panic(fmt.Errorf("found no Rel %q in %v, %v", rel, r.URL, pp(r.Body)))
}

func (r *Req) Success() *Result {
	res := r.do()
	bodyDesc := ""
	if r.body != nil {
		bodyDesc = fmt.Sprintf(" with %q", string(r.body))
	}
	if res.Status < 200 || res.Status > 299 {
		panic(fmt.Errorf("%qing %q%v: %v\n%s", r.method, r.url.String(), bodyDesc, res.Status, res.BodyBytes))
	}
	return res
}

func (r *Req) Failure() *Result {
	res := r.do()
	if res.Status > 199 && res.Status < 300 {
		panic(fmt.Errorf("fetching %q: %v", res.URL.String(), res.Status))
	}
	return res
}

func (r *Req) do() *Result {
	if r.url == nil {
		u, err := router.Get(r.route).URL(r.routeParams...)
		if err != nil {
			panic(fmt.Errorf("creating URL for %q and %+v: %v", r.route, r.routeParams, err))
		}
		r.url = u
	}
	queryParams := r.url.Query()
	if r.queryParams != nil {
		for k, v := range r.queryParams {
			queryParams[k] = append(queryParams[k], v...)
		}
	}
	if r.env.uid != "" {
		queryParams.Set("fake-id", r.env.uid)
	}
	r.url.RawQuery = queryParams.Encode()
	var bodyReader io.Reader
	if r.method == "POST" || r.method == "PUT" {
		bodyReader = bytes.NewBuffer(r.body)
	}

	req, err := T.Request(r.method, r.url.String(), bodyReader)
	if err != nil {
		panic(fmt.Errorf("creating GET %q: %v", r.url, err))
	}
	req.Header.Set("Accept", "application/json; charset=utf-8")
	if r.body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	status, _, responseReader, err := T.Execute(req)
	if err != nil {
		panic(fmt.Errorf("executing %+v: %v", req, err))
	}
	responseBytes, err := ioutil.ReadAll(responseReader)
	if err != nil {
		panic(fmt.Errorf("reading body from %+v: %v", req, err))
	}
	var result interface{}
	if status > 199 && status < 300 {
		if len(responseBytes) > 0 {
			if err := json.Unmarshal(responseBytes, &result); err != nil {
				panic(fmt.Errorf("unmarshaling %q: %v", string(responseBytes), err))
			}
		}
	}
	return &Result{
		Env:       r.env,
		URL:       r.url,
		BodyBytes: responseBytes,
		Body:      result,
		Status:    status,
	}
}
