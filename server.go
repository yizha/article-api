package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/rs/xid"
)

const (
	CtxKeyReqId     CtxKey = "req-id"
	CtxKeyReqLogger CtxKey = "req-app-logger"
)

type CtxKey string

func WithRequestId(ctx context.Context, reqId string) context.Context {
	return context.WithValue(ctx, CtxKeyReqId, reqId)
}

func RequestIdFromReq(req *http.Request) string {
	return req.Context().Value(CtxKeyReqId).(string)
}

func WithCtxLogger(ctx context.Context, jl *JsonLogger, reqId string) context.Context {
	return context.WithValue(ctx, CtxKeyReqLogger, jl.CloneWithFields(LogFields{
		"req-id": reqId,
	}))
}

func CtxLoggerFromReq(req *http.Request) *JsonLogger {
	return req.Context().Value(CtxKeyReqLogger).(*JsonLogger)
}

type ResponseWriter struct {
	rw          http.ResponseWriter
	status      int
	bytesWrote  uint64
	requestTime time.Time
}

func (w *ResponseWriter) Header() http.Header {
	return w.rw.Header()
}

func (w *ResponseWriter) Write(bytes []byte) (int, error) {
	n, err := w.rw.Write(bytes)
	w.bytesWrote += uint64(n)
	return n, err
}

func (w *ResponseWriter) WriteHeader(status int) {
	w.rw.WriteHeader(status)
	w.status = status
}

type HttpResponseData struct {
	RespWriter http.ResponseWriter
	Request    *http.Request
	Status     int
	Header     http.Header
	Body       io.Reader
	Ignore     bool
}

func (data *HttpResponseData) Write() error {
	if data.Ignore {
		return nil
	}
	// set headers
	header := data.RespWriter.Header()
	for k, vals := range data.Header {
		header.Del(k)
		for _, v := range vals {
			header.Add(k, v)
		}
	}
	// set request id
	header.Add("X-Request-Id", RequestIdFromReq(data.Request))
	// write header with status code
	data.RespWriter.WriteHeader(data.Status)
	// write body
	_, err := io.Copy(data.RespWriter, data.Body)
	return err
}

func CreateHeader(kv ...string) http.Header {
	m := make(map[string][]string)
	cnt := len(kv) / 2
	for i := 0; i <= cnt; i = i + 2 {
		k := kv[i]
		if k == "" {
			continue
		}
		m[k] = []string{kv[i+1]}
	}
	return http.Header(m)
}

type EndpointMethodHandler func(http.ResponseWriter, *http.Request, *AppRuntime) *HttpResponseData
type EndpointHandler map[string]EndpointMethodHandler
type Endpoint struct {
	app      *AppRuntime
	handlers EndpointHandler
}

func wrapRequestAndResponse(w http.ResponseWriter, r *http.Request, app *AppRuntime) (*ResponseWriter, *http.Request) {
	rw := &ResponseWriter{
		rw:          w,
		status:      http.StatusNotFound,
		bytesWrote:  uint64(0),
		requestTime: time.Now().UTC(),
	}
	reqId := xid.New().String()
	ctx := r.Context()
	ctx = WithRequestId(ctx, reqId)
	ctx = WithCtxLogger(ctx, app.logger, reqId)
	r = r.WithContext(ctx)

	return rw, r
}

func logRequest(w *ResponseWriter, r *http.Request, logger *JsonLogger) {
	processDuration := time.Now().UTC().Sub(w.requestTime)
	referer := r.Referer()
	if referer == "" {
		referer = "-"
	}
	userAgent := r.UserAgent()
	if userAgent == "" {
		userAgent = "-"
	}
	clientIp := r.Header.Get("X-Forwarded-For")
	if clientIp == "" {
		if r.RemoteAddr != "" {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err == nil {
				userIP := net.ParseIP(ip)
				if userIP != nil {
					clientIp = userIP.String()
				}
			}
		}
	}
	if clientIp == "" {
		clientIp = "-"
	}
	requestId := r.Context().Value(CtxKeyReqId)
	logger.LogMap(LogFields{
		"_log_type":      "http-access",
		"client_ip":      clientIp,
		"req_id":         requestId,
		"req_ts":         w.requestTime.Format("2006-01-02T15:04:05.000Z"),
		"req_method":     r.Method,
		"req_uri":        r.RequestURI,
		"req_protocol":   r.Proto,
		"resp_status":    w.status,
		"resp_body_size": w.bytesWrote,
		"process_time":   processDuration.Nanoseconds(),
		"referer":        referer,
		"user_agent":     userAgent,
	})
}

func (e *Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rw, rr := wrapRequestAndResponse(w, r, e.app)
	var d *HttpResponseData
	if h, ok := e.handlers[r.Method]; !ok {
		allow := make([]string, 0, len(e.handlers))
		for m, _ := range e.handlers {
			allow = append(allow, m)
		}
		allowHeader := strings.Join(allow, ", ")
		d = &HttpResponseData{
			RespWriter: rw,
			Request:    r,
			Status:     http.StatusMethodNotAllowed,
			Header:     CreateHeader("Content-Type", "text/plain; charset=utf-8", "Allow", allowHeader),
			Body:       strings.NewReader(fmt.Sprintf("Method %v not allowed for resource %v", r.Method, r.URL.Path)),
		}
	} else {
		d = h(rw, rr, e.app)
	}
	if d != nil {
		if err := d.Write(); err != nil {
			e.app.logger.LogFields("error", err.Error())
		}
	}
	logRequest(rw, rr, e.app.logger)
}

func registerHandlers(app *AppRuntime) {
	// set up
	handlers := make(map[string]*Endpoint)
	handlers["/keepalive"] = &Endpoint{app, Keepalive()}

	// register handlers
	for path, ep := range handlers {
		http.Handle(path, ep)
	}
}

func StartAPIServer(app *AppRuntime) error {
	conf := app.conf
	logger := app.logger

	// create server

	// register routes and handlers
	registerHandlers(app)

	// start server
	address := fmt.Sprintf("%v:%v", conf.ServerIP, conf.ServerPort)
	logger.Printf("starting api server on %v ...", address)
	return http.ListenAndServe(address, nil)
}
