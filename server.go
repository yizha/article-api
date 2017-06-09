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

type CtxKey string

const (
	CtxKeyReqId     CtxKey = "req-id"
	CtxKeyReqLogger        = "req-app-logger"

	HttpHeaderRequestId string = "X-Request-Id"
)

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
	w           http.ResponseWriter
	status      int
	bytesWrote  uint64
	requestTime time.Time
}

func (w *ResponseWriter) Header() http.Header {
	return w.w.Header()
}

func (w *ResponseWriter) Write(bytes []byte) (int, error) {
	n, err := w.w.Write(bytes)
	w.bytesWrote += uint64(n)
	return n, err
}

func (w *ResponseWriter) WriteHeader(status int) {
	w.w.WriteHeader(status)
	w.status = status
}

type HttpResponseData struct {
	Status int
	Header http.Header
	Body   io.Reader
}

func (data *HttpResponseData) Write(w http.ResponseWriter) error {
	// set headers
	header := w.Header()
	for k, vals := range data.Header {
		header.Del(k)
		for _, v := range vals {
			header.Add(k, v)
		}
	}
	// write header with status code
	w.WriteHeader(data.Status)
	// write body
	_, err := io.Copy(w, data.Body)
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
	reqId := xid.New().String()
	w.Header().Set(HttpHeaderRequestId, reqId)
	ww := &ResponseWriter{
		w:           w,
		status:      http.StatusNotFound,
		bytesWrote:  uint64(0),
		requestTime: time.Now().UTC(),
	}

	ctx := r.Context()
	ctx = WithRequestId(ctx, reqId)
	ctx = WithCtxLogger(ctx, app.logger, reqId)
	wr := r.WithContext(ctx)

	return ww, wr
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
	ww, wr := wrapRequestAndResponse(w, r, e.app)
	var d *HttpResponseData
	if h, ok := e.handlers[r.Method]; !ok {
		allow := make([]string, 0, len(e.handlers))
		for m, _ := range e.handlers {
			allow = append(allow, m)
		}
		allowHeader := strings.Join(allow, ", ")
		d = &HttpResponseData{
			Status: http.StatusMethodNotAllowed,
			Header: CreateHeader("Content-Type", "text/plain; charset=utf-8", "Allow", allowHeader),
			Body:   strings.NewReader(fmt.Sprintf("Method %v not allowed for resource %v", r.Method, r.URL.Path)),
		}
	} else {
		d = h(ww, wr, e.app)
	}
	if err := d.Write(ww); err != nil {
		e.app.logger.LogFields("error", err.Error())
	}
	logRequest(ww, wr, e.app.logger)
}

func registerHandlers(app *AppRuntime) {
	http.Handle("/keepalive", &Endpoint{app, Keepalive()})
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
