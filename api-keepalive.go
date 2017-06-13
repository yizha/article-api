package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

func Keepalive(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	//CtxLoggerFromReq(r).Print("logging from /keepalive handler.")
	ctx := context.Background()
	if resp, err := app.Elastic.Client.ClusterHealth().Do(ctx); err == nil {
		if resp.Status == "red" {
			body := "Elasticsearch server cluster health is RED!"
			return CreateInternalServerErrorRespData(body)
		} else {
			return &HttpResponseData{
				Status: http.StatusOK,
				Header: CreateHeader(HeaderContentType, ContentTypeValueText),
				Body:   strings.NewReader("I'm all good!"),
			}
		}
	} else {
		body := fmt.Sprintf("Failed to check elasticsearch server cluster health, error: %v", err)
		return CreateInternalServerErrorRespData(body)
	}
}
