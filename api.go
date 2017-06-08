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

func ParseQueryIntValue(
	data url.Values,
	name string,
	required bool,
	defaultValue, min, max int) (int, *HttpResponseData) {
	s := data.Get(name)
	if s == "" {
		if required {
			return 0, &HttpResponseData{
				Status: http.StatusBadRequest,
				Header: map[string][]string{
					"Content-Type": []string{"text/plain"},
				},
				Body: strings.NewReader(fmt.Sprintf("missing query arg %v!", name)),
			}
		} else {
			return defaultValue, nil
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, &HttpResponseData{
			Status: http.StatusBadRequest,
			Header: map[string][]string{
				"Content-Type": []string{"text/plain"},
			},
			Body: strings.NewReader(fmt.Sprintf("failed to convert %v (value: %v) to int, error: %v!", name, s, err)),
		}
	}
	if max >= min {
		if n < min || n > max {
			return 0, &HttpResponseData{
				Status: http.StatusBadRequest,
				Header: map[string][]string{
					"Content-Type": []string{"text/plain"},
				},
				Body: strings.NewReader(fmt.Sprintf("%v (value: %v) is not within allowed range %v~%v.", name, s, min, max)),
			}
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
			return 0, &HttpResponseData{
				Status: http.StatusBadRequest,
				Header: map[string][]string{
					"Content-Type": []string{"text/plain"},
				},
				Body: strings.NewReader(fmt.Sprintf("missing query arg %v!", name)),
			}
		} else {
			return defaultValue, nil
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, &HttpResponseData{
			Status: http.StatusBadRequest,
			Header: map[string][]string{
				"Content-Type": []string{"text/plain"},
			},
			Body: strings.NewReader(fmt.Sprintf("failed to convert %v (value: %v) to int64, error: %v!", name, s, err)),
		}
	}
	if max >= min {
		if n < min || n > max {
			return 0, &HttpResponseData{
				Status: http.StatusBadRequest,
				Header: map[string][]string{
					"Content-Type": []string{"text/plain"},
				},
				Body: strings.NewReader(fmt.Sprintf("%v (value: %v) is not within allowed range %v~%v.", name, s, min, max)),
			}
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
		return nil, &HttpResponseData{
			Status: http.StatusBadRequest,
			Header: map[string][]string{
				"Content-Type": []string{"text/plain"},
			},
			Body: strings.NewReader(fmt.Sprintf("failed to base64-decode cursorMark %v, error: %v!", s, err)),
		}
	}
	r := make([]interface{}, 0)
	if err := json.Unmarshal(bytes, &r); err != nil {
		return nil, &HttpResponseData{
			Status: http.StatusBadRequest,
			Header: map[string][]string{
				"Content-Type": []string{"text/plain"},
			},
			Body: strings.NewReader(fmt.Sprintf("failed to json-decode cursorMark %v, error: %v!", s, err)),
		}
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
