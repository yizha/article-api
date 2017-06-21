package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/xid"
)

type CtxKey string

const (
	CtxKeyLogger  CtxKey = "logger"
	CtxKeyCmsUser        = "cms-user"
	CtxKeyId             = "id"
	CtxKeyVer            = "ver"
)

func WithCtxStringValue(ctx context.Context, key CtxKey, val string) context.Context {
	return context.WithValue(ctx, key, val)
}

func StringFromReq(req *http.Request, key CtxKey) string {
	v := req.Context().Value(key)
	if v == nil {
		return ""
	} else if s, ok := v.(string); ok {
		return s
	} else {
		fmt.Fprintf(os.Stdout, "value (%T, %v) under key %v is not string!", v, v, key)
		return ""
	}
}

func WithCtxLogger(ctx context.Context, jl *JsonLogger, reqId string) context.Context {
	return context.WithValue(ctx, CtxKeyLogger, jl.CloneWithFields(LogFields{
		"req_id": reqId,
	}))
}

func CtxLoggerFromReq(req *http.Request) *JsonLogger {
	return req.Context().Value(CtxKeyLogger).(*JsonLogger)
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
	Data   interface{}
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

func IPFromRequestRemoteAddr(addr string) string {
	if len(addr) > 0 {
		ip, _, err := net.SplitHostPort(addr)
		if err == nil {
			userIP := net.ParseIP(ip)
			if userIP != nil {
				return userIP.String()
			}
		}
	}
	return ""
}

func logRequest(w *ResponseWriter, r *http.Request) {
	processDuration := time.Now().UTC().Sub(w.requestTime)
	/*
		referer := r.Referer()
		if referer == "" {
			referer = "-"
		}
	//*/
	userAgent := r.UserAgent()
	if userAgent == "" {
		userAgent = "-"
	}
	clientIp := r.Header.Get("X-Forwarded-For")
	if clientIp == "" {
		clientIp = IPFromRequestRemoteAddr(r.RemoteAddr)
	}
	if clientIp == "" {
		clientIp = "-"
	}
	CtxLoggerFromReq(r).LogMap(LogFields{
		"log_group":        "http-access",
		"req_remote_ip":    clientIp,
		"req_time":         w.requestTime.Format("2006-01-02T15:04:05.000Z"),
		"req_method":       r.Method,
		"req_uri":          r.RequestURI,
		"req_protocol":     r.Proto,
		"req_process_time": processDuration.Nanoseconds(),
		"resp_status":      w.status,
		"resp_body_size":   w.bytesWrote,
		//		"req_referer":      referer,
		"req_user_agent": userAgent,
	})
}

type EndpointHandler func(*AppRuntime, http.ResponseWriter, *http.Request) *HttpResponseData

func handler(app *AppRuntime, method string, h EndpointHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww, wr := wrapRequestAndResponse(w, r, app)
		var d *HttpResponseData
		if method != r.Method {
			d = &HttpResponseData{
				Status: http.StatusMethodNotAllowed,
				Header: CreateHeader(HeaderContentType, ContentTypeValueText, "Allow", method),
				Body:   strings.NewReader(fmt.Sprintf("Method %v not allowed for resource %v", r.Method, r.URL.Path)),
			}
		} else {
			d = h(app, ww, wr)
		}
		d.Header.Set("Access-Control-Allow-Origin", "http://localhost:8000")
		if err := d.Write(ww); err != nil {
			CtxLoggerFromReq(wr).Perror(err)
		}
		logRequest(ww, wr)
	})
}

func registerHandlers(app *AppRuntime) *http.ServeMux {

	mux := http.NewServeMux()

	// keepalive
	mux.Handle("/keepalive", handler(app, http.MethodGet, Keepalive))

	// login
	mux.Handle("/api/login", handler(app, http.MethodGet, Login()))
	mux.Handle("/api/login/create", handler(app, http.MethodGet, LoginCreate()))
	mux.Handle("/api/login/update", handler(app, http.MethodGet, LoginUpdate()))
	mux.Handle("/api/login/delete", handler(app, http.MethodGet, LoginDelete()))

	// article update endpoints
	mux.Handle("/api/article/create", handler(app, http.MethodGet, ArticleCreate()))
	mux.Handle("/api/article/edit", handler(app, http.MethodGet, ArticleEdit()))
	mux.Handle("/api/article/save", handler(app, http.MethodPost, ArticleSave()))
	mux.Handle("/api/article/submit-self", handler(app, http.MethodPost, ArticleSubmitSelf()))
	mux.Handle("/api/article/discard-self", handler(app, http.MethodGet, ArticleDiscardSelf()))
	mux.Handle("/api/article/submit-other", handler(app, http.MethodGet, ArticleSubmitOther()))
	mux.Handle("/api/article/discard-other", handler(app, http.MethodGet, ArticleDiscardOther()))
	mux.Handle("/api/article/publish", handler(app, http.MethodGet, ArticlePublish()))
	mux.Handle("/api/article/unpublish", handler(app, http.MethodGet, ArticleUnpublish()))

	// article get endpoints
	mux.Handle("/api/article", handler(app, http.MethodGet, ArticleGet()))

	return mux
}

func StartAPIServer(app *AppRuntime) {
	conf := app.Conf
	logger := app.Logger

	addr := fmt.Sprintf("%v:%v", conf.ServerIP, conf.ServerPort)
	srv := &http.Server{
		Handler:      registerHandlers(app),
		Addr:         addr,
		ReadTimeout:  app.Conf.ServerReadTimeout,
		WriteTimeout: app.Conf.ServerWriteTimeout,
	}

	// start server
	logger.Pinfof("starting api server on %v", addr)
	if err := srv.ListenAndServe(); err != nil {
		panic(err)
	}
}
