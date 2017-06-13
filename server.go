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
	CtxKeyLogger CtxKey = "logger"
	CtxKeyUser          = "user"
	CtxKeyId            = "id"
	CtxKeyVer           = "ver"
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
		if err := d.Write(ww); err != nil {
			CtxLoggerFromReq(wr).Perror(err)
		}
		logRequest(ww, wr)
	})
}

func authHandler(h EndpointHandler) EndpointHandler {
	return func(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
		msg := ""
		if token := r.Header.Get(HeaderAuthToken); len(token) > 0 {
			var user User
			if err := app.Conf.SCookie.Decode(TokenCookieName, token, &user); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), CtxKeyUser, &user))
				return h(app, w, r)
			} else {
				msg = fmt.Sprintf(`You are not authorized to access this resource! Reason: %v`, err)
			}
		} else {
			msg = `You are not authorized to access this resource!`
		}
		return CreateForbiddenRespData(msg)
	}
}

func registerHandlers(app *AppRuntime) *http.ServeMux {

	mux := http.NewServeMux()

	// keepalive
	mux.Handle("/keepalive", handler(app, http.MethodGet, Keepalive))

	// login
	mux.Handle("/login", handler(app, http.MethodGet, Login))

	// article endpoints
	mux.Handle("/article/create", handler(app, http.MethodGet, authHandler(ArticleCreate(app))))
	mux.Handle("/article/edit", handler(app, http.MethodGet, authHandler(ArticleEdit(app))))
	mux.Handle("/article/save", handler(app, http.MethodPost, authHandler(ArticleSave(app))))
	mux.Handle("/article/submit", handler(app, http.MethodPost, authHandler(ArticleSubmit(app))))
	mux.Handle("/article/discard", handler(app, http.MethodGet, authHandler(ArticleDiscard(app))))
	mux.Handle("/article/publish", handler(app, http.MethodGet, authHandler(ArticlePublish(app))))
	mux.Handle("/article/unpublish", handler(app, http.MethodGet, authHandler(ArticleUnpublish(app))))

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
