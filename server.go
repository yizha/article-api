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
)

func WithCtxLogger(ctx context.Context, jl *JsonLogger, reqId string) context.Context {
	return context.WithValue(ctx, CtxKeyReqLogger, jl.CloneWithFields(LogFields{
		"req_id": reqId,
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

func wrapRequestAndResponse(w http.ResponseWriter, r *http.Request, app *AppRuntime) (*ResponseWriter, *http.Request) {
	reqId := xid.New().String()
	w.Header().Set(HeaderRequestId, reqId)
	ww := &ResponseWriter{
		w:           w,
		status:      http.StatusNotFound,
		bytesWrote:  uint64(0),
		requestTime: time.Now().UTC(),
	}

	ctx := r.Context()
	ctx = WithCtxLogger(ctx, app.Logger, reqId)
	wr := r.WithContext(ctx)

	return ww, wr
}

func logRequest(w *ResponseWriter, r *http.Request) {
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
	CtxLoggerFromReq(r).LogMap(LogFields{
		"log_group":        "http-access",
		"req_remote_ip":    clientIp,
		"req_ts":           w.requestTime.Format("2006-01-02T15:04:05.000Z"),
		"req_method":       r.Method,
		"req_uri":          r.RequestURI,
		"req_protocol":     r.Proto,
		"req_process_time": processDuration.Nanoseconds(),
		"resp_status":      w.status,
		"resp_body_size":   w.bytesWrote,
		"req_referer":      referer,
		"req_user_agent":   userAgent,
	})
}

type EndpointHandler func(http.ResponseWriter, *http.Request, *AppRuntime) *HttpResponseData

func createHandlerFunc(app *AppRuntime, method string, h EndpointHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww, wr := wrapRequestAndResponse(w, r, app)
		var d *HttpResponseData
		if method != r.Method {
			d = &HttpResponseData{
				Status: http.StatusMethodNotAllowed,
				Header: CreateHeader("Content-Type", "text/plain; charset=utf-8", "Allow", method),
				Body:   strings.NewReader(fmt.Sprintf("Method %v not allowed for resource %v", r.Method, r.URL.Path)),
			}
		} else {
			d = h(ww, wr, app)
		}
		if err := d.Write(ww); err != nil {
			CtxLoggerFromReq(wr).Perror(err)
		}
		logRequest(ww, wr)
	})
}

func registerHandlers(app *AppRuntime) {
	http.Handle("/keepalive", createHandlerFunc(app, http.MethodGet, Keepalive))

	/*
	   new       article  /article/create          GET returns data
	   edit      article  /article/edit?id=x       GET returns data
	   save      changes  /article/save?id=x       PUT with data
	   submit    changes  /article/submit?id=x     PUT with data
	   discard   changes  /article/discard?id=x    GET returns data
	   publish   article  /article/publish?id=x    GET returns data
	   unpublish article  /article/unpublish?id=x  GET returns data
	*/
	http.Handle("/article/create", createHandlerFunc(app, http.MethodGet, ArticleCreate))
}

func StartAPIServer(app *AppRuntime) error {
	conf := app.Conf
	logger := app.Logger

	// register routes and handlers
	registerHandlers(app)

	// start server
	address := fmt.Sprintf("%v:%v", conf.ServerIP, conf.ServerPort)
	logger.Pinfof("starting api server on %v", address)
	return http.ListenAndServe(address, nil)
}
