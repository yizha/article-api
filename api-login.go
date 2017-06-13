package main

import (
	//	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	//	"strings"

	"golang.org/x/crypto/bcrypt"
)

const TokenCookieName = "token"

type AuthToken struct {
	Token string `json:"token"`
}

type UserRole map[string]bool

func (r UserRole) MarshalJSON() ([]byte, error) {
	roles := make([]string, len(r))
	i := 0
	for k, _ := range r {
		roles[i] = k
		i += 1
	}
	return json.Marshal(roles)
}

func (r UserRole) UnmarshalJSON(data []byte) error {
	var roles []string
	if err := json.Unmarshal(data, roles); err != nil {
		return nil
	}
	if roles != nil && len(roles) > 0 {
		for _, v := range roles {
			r[v] = true
		}
	}
	return nil
}

type User struct {
	Username string
	Password string
	Role     UserRole
}

func UserFromReq(req *http.Request) *User {
	v := req.Context().Value(CtxKeyUser)
	if v == nil {
		return nil
	} else if user, ok := v.(*User); ok {
		return user
	} else {
		fmt.Fprintf(os.Stdout, "value (%T, %v) under key %v is not of type User!", v, v, CtxKeyUser)
		return nil
	}
}

func HashPassword(pass string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.MaxCost)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func getUser(app *AppRuntime, username string) (*User, error) {
	getService := app.Elastic.Client.Get()
	getService.Index(app.Conf.UserIndex.Name)
	getService.Type(app.Conf.UserIndexTypes.User)
	getService.FetchSource(true)
	getService.Realtime(false)
	getService.Id(username)
}

func Login(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	username, d := ParseQueryStringValue(r.URL.Query(), "username", true, "")
	if d != nil {
		return d
	}
	password, d := ParseQueryStringValue(r.URL.Query(), "password", true, "")
	if d != nil {
		return d
	}
	password, err := HashPassword(password)
	if err != nil {
		body := fmt.Sprintf("failed to hash (bcrypt) password, error: %v", err)
		return CreateInternalServerErrorRespData(body)
	}
	if password != "123" {
		return CreateForbiddenRespData("Wrong password!")
	}
	token, err := app.Conf.SCookie.Encode(TokenCookieName, &User{
		Username: username,
		Role: map[string]bool{
			"admin": true,
		},
	})
	if err == nil {
		return CreateJsonRespData(http.StatusOK, &AuthToken{token})
	} else {
		body := fmt.Sprintf("failed to encode auth data, error: %v", err)
		return CreateInternalServerErrorRespData(body)
	}
	/*ctx := context.Background()
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
	}*/
}
