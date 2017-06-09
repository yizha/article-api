package main

import (
	"net/http"
	"strings"
)

func handleKeepalive(w http.ResponseWriter, r *http.Request, app *AppRuntime) *HttpResponseData {
	//CtxLoggerFromReq(r).Print("logging from /keepalive handler.")
	return &HttpResponseData{
		Status: http.StatusOK,
		Header: CreateHeader("Content-Type", "text/plain; charset=utf-8"),
		Body:   strings.NewReader("WoW"),
	}
}

func Keepalive() EndpointHandler {
	m := EndpointHandler(make(map[string]EndpointMethodHandler))
	m[http.MethodGet] = handleKeepalive
	return m
}
