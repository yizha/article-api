package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	elastic "github.com/yizha/elastic"
	"golang.org/x/crypto/bcrypt"
)

const TokenCookieName = "token"

type AuthToken struct {
	Token string `json:"token"`
}

type CmsRole uint64

const (

	// create article
	// implies save/submit/discard draft article created by self
	CmsRoleArticleCreate CmsRole = 1 << 0

	// edit article
	// implies save/submit/discard draft article created by self
	CmsRoleArticleEdit CmsRole = 1 << 1

	// submit/discard draft article created by others
	CmsRoleArticleDraftCleaner CmsRole = 1 << 2

	// publish/unpublish article
	CmsRoleArticlePublish CmsRole = 1 << 3
)

var (
	CmsRoleId2Name map[CmsRole]string
	CmsRoleName2Id map[string]CmsRole
)

func init() {
	CmsRoleId2Name = map[CmsRole]string{
		CmsRoleArticleCreate:       "create",
		CmsRoleArticleEdit:         "edit",
		CmsRoleArticleDraftCleaner: "draft-cleaner",
		CmsRoleArticlePublish:      "publish",
	}
	CmsRoleName2Id = make(map[string]CmsRole)
	for id, name := range CmsRoleId2Name {
		CmsRoleName2Id[name] = id
	}
}

func (r CmsRole) MarshalJSON() ([]byte, error) {
	roles := make([]string, 0)
	for id, name := range CmsRoleId2Name {
		if r&id == id {
			roles = append(roles, name)
		}
	}
	return json.Marshal(roles)
}

func (r CmsRole) UnmarshalJSON(data []byte) error {
	roles := make([]string, 0)
	if err := json.Unmarshal(data, &roles); err != nil {
		return err
	}
	if roles != nil && len(roles) > 0 {
		for _, name := range roles {
			if id, ok := CmsRoleName2Id[name]; ok {
				r = r | id
			} else {
				fmt.Fprintf(os.Stderr, "ignore unknown cms user role name %v!", name)
			}
		}
	}
	return nil
}

type CmsUser struct {
	Username string  `json:"username,omitempty"`
	Password string  `json:"password,omitempty"`
	Role     CmsRole `json:"role,omitempty"`
}

func CmsUserFromReq(req *http.Request) *CmsUser {
	v := req.Context().Value(CtxKeyCmsUser)
	if v == nil {
		return nil
	} else if user, ok := v.(*CmsUser); ok {
		return user
	} else {
		fmt.Fprintf(os.Stdout, "value (%T, %v) under key %v is not of type CmsUser!", v, v, CtxKeyCmsUser)
		return nil
	}
}

func HashPassword(pass string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(bytes), nil
}

func getCmsUser(app *AppRuntime, username string) (*CmsUser, *HttpResponseData) {
	getService := app.Elastic.Client.Get()
	getService.Index(app.Conf.UserIndex.Name)
	getService.Type(app.Conf.UserIndexTypes.User)
	getService.FetchSource(true)
	getService.Realtime(false)
	getService.Id(username)
	resp, err := getService.Do(context.Background())
	if err != nil {
		if elastic.IsNotFound(err) {
			return nil, CreateForbiddenRespData("no such user!")
		} else {
			body := fmt.Sprintf("failed to query elasticsearch, error: %v", err)
			return nil, CreateInternalServerErrorRespData(body)
		}
	} else {
		var user CmsUser
		if err = json.Unmarshal(*resp.Source, &user); err == nil {
			return &user, nil
		} else {
			body := fmt.Sprintf("failed to unmarshal cms user, error: %v", err)
			return nil, CreateInternalServerErrorRespData(body)
		}
	}
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
	user, d := getCmsUser(app, username)
	if d != nil {
		return d
	}
	//fmt.Println(password)
	//fmt.Println(user.Password)
	hashedPassword, err := base64.StdEncoding.DecodeString(user.Password)
	if err != nil {
		body := fmt.Sprintf("failed to hex decode user password loaded from elasticsearch, error: %v", err)
		return CreateInternalServerErrorRespData(body)
	}
	if err = bcrypt.CompareHashAndPassword(hashedPassword, []byte(password)); err != nil {
		//fmt.Println(err)
		return CreateForbiddenRespData("wrong password!")
	}
	user.Password = ""
	if token, err := app.Conf.SCookie.Encode(TokenCookieName, user); err == nil {
		return CreateJsonRespData(http.StatusOK, &AuthToken{token})
	} else {
		body := fmt.Sprintf("failed to encode user, error: %v", err)
		return CreateInternalServerErrorRespData(body)
	}
}

func CreateLogin(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	username, d := ParseQueryStringValue(r.URL.Query(), "username", true, "")
	if d != nil {
		return d
	}
	password, d := ParseQueryStringValue(r.URL.Query(), "password", true, "")
	if d != nil {
		return d
	}
	roleStr, d := ParseQueryStringValue(r.URL.Query(), "role", false, "")
	if d != nil {
		return d
	}
	var role CmsRole = 0
	if len(roleStr) > 0 {
		for _, roleName := range strings.Split(roleStr, ",") {
			if id, ok := CmsRoleName2Id[roleName]; ok {
				role = role | id
			} else {
				CtxLoggerFromReq(r).Pwarnf("ignore unknown cms user role name %v!", roleName)
			}
		}
	}
	//fmt.Println("starting hashing password ...")
	password, err := HashPassword(password)
	if err != nil {
		body := fmt.Sprintf("failed to hash (bcrypt) password, error: %v", err)
		return CreateInternalServerErrorRespData(body)
	}
	//fmt.Printf("hashed password: %v\n", password)
	user := &CmsUser{
		Username: username,
		Password: password,
		Role:     role,
	}
	idxService := app.Elastic.Client.Index()
	idxService.Index(app.Conf.UserIndex.Name)
	idxService.Type(app.Conf.UserIndexTypes.User)
	idxService.OpType(ESIndexOpCreate)
	idxService.Id(user.Username)
	idxService.BodyJson(user)
	resp, err := idxService.Do(context.Background())
	if err != nil {
		if elasticErr, ok := err.(*elastic.Error); ok {
			return CreateRespData(elasticErr.Status, ContentTypeValueText, err.Error())
		} else {
			body := fmt.Sprintf("error indexing user doc, error: %v", err)
			return CreateInternalServerErrorRespData(body)
		}
	} else if !resp.Created {
		return CreateInternalServerErrorRespData("unknown error!")
	} else {
		return CreateRespData(http.StatusOK, ContentTypeValueText, "")
	}
}
