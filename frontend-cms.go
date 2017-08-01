package main

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
)

var (
	pageTemplates = make(map[string]*template.Template)
)

func CmsPage(name string) EndpointHandler {
	return func(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
		logger := CtxLoggerFromReq(r)
		tpl, err := GetPageTemplate(app, name)
		if err != nil {
			body := err.Error()
			logger.Perrorf("unexpected error: %s", body)
			return CreateInternalServerErrorRespData(body)
		}
		buf := &bytes.Buffer{}
		if err := tpl.Execute(buf, nil); err != nil {
			body := fmt.Sprintf("failed to generate cms %s page, error: %v", name, err)
			return CreateInternalServerErrorRespData(body)
		}
		return CreateRespData(http.StatusOK, "text/html; charset=UTF-8", buf.Bytes())
	}
}
