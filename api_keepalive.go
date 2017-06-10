package main

import (
	"fmt"
	"net/http"
	"strings"
)

func Keepalive(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	//CtxLoggerFromReq(r).Print("logging from /keepalive handler.")
	if resp, err := app.Elastic.Client.ClusterHealth().Do(app.Elastic.Context); err == nil {
		if resp.Status == "red" {
			return &HttpResponseData{
				Status: http.StatusInternalServerError,
				Header: CreateHeader("Content-Type", "text/plain; charset=utf-8"),
				Body:   strings.NewReader("Elasticsearch server cluster health is RED!"),
			}
		} else {
			return &HttpResponseData{
				Status: http.StatusOK,
				Header: CreateHeader("Content-Type", "text/plain; charset=utf-8"),
				Body:   strings.NewReader("I'm all good!"),
			}
		}
	} else {
		return &HttpResponseData{
			Status: http.StatusInternalServerError,
			Header: CreateHeader("Content-Type", "text/plain; charset=utf-8"),
			Body:   strings.NewReader(fmt.Sprintf("Failed to check elasticsearch server cluster health, error: %v", err)),
		}
	}
}
