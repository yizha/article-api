package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	HeaderRequestId string = "X-Request-Id"

	ContentTypeValueJSON string = "application/json; charset=utf-8"
	ContentTypeValueText string = "text/plain; charset=utf-8"

	ESIndexOpCreate string = "create"
)

func GetRequiredStringArg(app *AppRuntime, argName string, ctxKey CtxKey, h EndpointHandler) EndpointHandler {
	return func(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
		val, d := ParseQueryStringValue(r.URL.Query(), argName, true, "")
		if d != nil {
			return d
		} else {
			r = r.WithContext(WithCtxStringValue(r.Context(), ctxKey, val))
			return h(app, w, r)
		}
	}
}

func CreateRespData(status int, contentType, body string) *HttpResponseData {
	return &HttpResponseData{
		Status: status,
		Header: map[string][]string{
			"Content-Type": []string{contentType},
		},
		Body: strings.NewReader(body),
	}
}

func CreateInternalServerErrorRespData(body string) *HttpResponseData {
	return &HttpResponseData{
		Status: http.StatusInternalServerError,
		Header: map[string][]string{
			"Content-Type": []string{ContentTypeValueText},
		},
		Body: strings.NewReader(body),
	}
}

func CreateBadRequestRespData(body string) *HttpResponseData {
	return &HttpResponseData{
		Status: http.StatusBadRequest,
		Header: map[string][]string{
			"Content-Type": []string{ContentTypeValueText},
		},
		Body: strings.NewReader(body),
	}
}

func CreateNotFoundRespData(body string) *HttpResponseData {
	return &HttpResponseData{
		Status: http.StatusNotFound,
		Header: map[string][]string{
			"Content-Type": []string{ContentTypeValueText},
		},
		Body: strings.NewReader(body),
	}
}

func ParseQueryStringValue(
	data url.Values,
	name string,
	required bool,
	defaultValue string) (string, *HttpResponseData) {
	s := data.Get(name)
	if s == "" {
		if required {
			body := fmt.Sprintf(`missing query arg "%v"!`, name)
			return "", CreateBadRequestRespData(body)
		} else {
			return defaultValue, nil
		}
	} else {
		return s, nil
	}
}

func ParseQueryIntValue(
	data url.Values,
	name string,
	required bool,
	defaultValue, min, max int) (int, *HttpResponseData) {
	s := data.Get(name)
	if s == "" {
		if required {
			body := fmt.Sprintf(`missing query arg "%v"!`, name)
			return 0, CreateBadRequestRespData(body)
		} else {
			return defaultValue, nil
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		body := fmt.Sprintf("failed to convert %v (value: %v) to int, error: %v!", name, s, err)
		return 0, CreateBadRequestRespData(body)
	}
	if max >= min {
		if n < min || n > max {
			body := fmt.Sprintf("%v (value: %v) is not within allowed range %v~%v.", name, s, min, max)
			return 0, CreateBadRequestRespData(body)
		}
	}
	return n, nil
}

func ParseQueryLongValue(
	data url.Values,
	name string,
	required bool,
	defaultValue, min, max int64) (int64, *HttpResponseData) {
	s := data.Get(name)
	if s == "" {
		if required {
			body := fmt.Sprintf(`missing query arg "%v"!`, name)
			return 0, CreateBadRequestRespData(body)
		} else {
			return defaultValue, nil
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		body := fmt.Sprintf("failed to convert %v (value: %v) to int64, error: %v!", name, s, err)
		return 0, CreateBadRequestRespData(body)
	}
	if max >= min {
		if n < min || n > max {
			body := fmt.Sprintf("%v (value: %v) is not within allowed range %v~%v.", name, s, min, max)
			return 0, CreateBadRequestRespData(body)
		}
	}
	return n, nil
}

func DecodeCursorMark(data url.Values) ([]interface{}, *HttpResponseData) {
	s := data.Get("cursorMark")
	if s == "" {
		return nil, nil
	}
	if s == "*" {
		return nil, nil
	}
	bytes, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		body := fmt.Sprintf("failed to base64-decode cursorMark %v, error: %v!", s, err)
		return nil, CreateBadRequestRespData(body)
	}
	r := make([]interface{}, 0)
	if err := json.Unmarshal(bytes, &r); err != nil {
		body := fmt.Sprintf("failed to json-decode cursorMark %v, error: %v!", s, err)
		return nil, CreateBadRequestRespData(body)
	}
	return r, nil
}

func EncodeCursorMark(sorts []interface{}) (string, error) {
	if sorts == nil {
		return "", errors.New("empty sorts!")
	}
	bytes, err := json.Marshal(sorts)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}
