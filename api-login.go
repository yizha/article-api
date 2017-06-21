package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	elastic "github.com/yizha/elastic"
	"golang.org/x/crypto/bcrypt"
)

type AuthToken struct {
	Token  string `json:"token"`
	Expire string `json:"expire"`
	Role   uint32 `json:"role"`
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

func login(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	args := r.URL.Query()
	username, d := ParseQueryStringValue(args, "username", true, "")
	if d != nil {
		return d
	}
	password, d := ParseQueryStringValue(args, "password", true, "")
	if d != nil {
		return d
	}
	user, d := getCmsUser(app, username)
	if d != nil {
		return d
	}
	logger := CtxLoggerFromReq(r)
	//fmt.Println(password)
	//fmt.Println(user.Password)
	hashedPassword, err := base64.StdEncoding.DecodeString(user.Password)
	if err != nil {
		body := fmt.Sprintf("failed to hex decode user password loaded from elasticsearch, error: %v", err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	}
	if err = bcrypt.CompareHashAndPassword(hashedPassword, []byte(password)); err != nil {
		//fmt.Println(err)
		logger.Perror(fmt.Sprintf("wrong password: %v", err))
		return CreateForbiddenRespData("wrong password!")
	}
	// clean hashed-password as we don't want it to be in the token
	user.Password = ""
	if token, err := app.Conf.SCookie.Encode(TokenCookieName, user); err == nil {
		logger.Pinfof("user %v login successfully.", user.String())
		// substract one minute as buffer
		expire := time.Now().UTC().Add(app.Conf.SCookieMaxAge).Add(-1 * time.Minute)
		return CreateJsonRespData(http.StatusOK, &AuthToken{
			Token:  token,
			Expire: expire.Format("2006-01-02T15:04:05"),
			Role:   uint32(user.Role),
		})
	} else {
		body := fmt.Sprintf("failed to encode user, error: %v", err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	}
}

func createLogin(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	args := r.URL.Query()
	username, d := ParseQueryStringValue(args, "username", true, "")
	if d != nil {
		return d
	}
	password, d := ParseQueryStringValue(args, "password", true, "")
	if d != nil {
		return d
	}
	roleStr, d := ParseQueryStringValue(args, "role", false, "")
	if d != nil {
		return d
	}
	logger := CtxLoggerFromReq(r)
	//fmt.Println("starting hashing password ...")
	password, err := HashPassword(password)
	if err != nil {
		body := fmt.Sprintf("failed to hash (bcrypt) password, error: %v", err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	}
	//fmt.Printf("hashed password: %v\n", password)
	role := Names2Role(roleStr)
	user := &CmsUser{
		Username: username,
		Password: password,
		Role:     role,
	}
	idxService := app.Elastic.Client.Index()
	idxService.Index(app.Conf.UserIndex.Name)
	idxService.Type(app.Conf.UserIndexTypes.User)
	idxService.OpType(ESIndexOpCreate)
	idxService.Refresh("wait_for")
	idxService.Id(user.Username)
	idxService.BodyJson(user)
	resp, err := idxService.Do(context.Background())
	if err != nil {
		if elasticErr, ok := err.(*elastic.Error); ok {
			body := elasticErr.Error()
			logger.Perror(body)
			return CreateRespData(elasticErr.Status, ContentTypeValueText, []byte(body))
		} else {
			body := fmt.Sprintf("error indexing user doc, error: %v", err)
			logger.Perror(body)
			return CreateInternalServerErrorRespData(body)
		}
	} else if !resp.Created {
		body := "unknown error!"
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	} else {
		logger.Pinfof("user %v create login %v", CmsUserFromReq(r).Username, user.String())
		return CreateRespData(http.StatusOK, ContentTypeValueText, []byte{})
	}
}

func updateLogin(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	logger := CtxLoggerFromReq(r)
	args := r.URL.Query()
	// username
	username, d := ParseQueryStringValue(args, "username", true, "")
	if d != nil {
		return d
	}
	user := make(map[string]interface{})
	user["username"] = username
	// password
	password, d := ParseQueryStringValue(args, "password", false, "")
	if d != nil {
		return d
	}
	if len(password) > 0 {
		var err error
		password, err = HashPassword(password)
		if err != nil {
			body := fmt.Sprintf("failed to hash (bcrypt) password, error: %v", err)
			logger.Perror(body)
			return CreateInternalServerErrorRespData(body)
		}
		user["password"] = password
	}
	// for role we need to tell below cases from each other
	//  1. "role" is not set at all --> don't update user role
	//  2. "role" is set to blank string --> clear user role
	//  3. "role" is set to something --> update user role
	vals, ok := args["role"]
	if ok && len(vals) > 0 { // this covers case 2 & 3
		// this filters out invalid role names
		user["role"] = Role2Names(Names2Role(vals[0]))
	} // else covers case 1

	updService := app.Elastic.Client.Update()
	updService.Index(app.Conf.UserIndex.Name)
	updService.Type(app.Conf.UserIndexTypes.User)
	updService.Refresh("wait_for")
	updService.Id(username)
	updService.Doc(user)
	updService.DocAsUpsert(false)
	updService.DetectNoop(false)
	_, err := updService.Do(context.Background())
	if err != nil {
		body := fmt.Sprintf("error indexing user doc, error: %v", err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	} else {
		logger.Pinfof("user %v updated login %v", CmsUserFromReq(r).Username, username)
		return CreateRespData(http.StatusOK, ContentTypeValueText, []byte{})
	}
}

func deleteLogin(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	logger := CtxLoggerFromReq(r)
	username, d := ParseQueryStringValue(r.URL.Query(), "username", true, "")
	if d != nil {
		return d
	}
	delService := app.Elastic.Client.Delete()
	delService.Index(app.Conf.UserIndex.Name)
	delService.Type(app.Conf.UserIndexTypes.User)
	delService.Refresh("wait_for")
	delService.Id(username)
	_, err := delService.Do(context.Background())
	if err != nil {
		body := fmt.Sprintf("failed to delete login %v, error: %v", username, err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	} else {
		logger.Pinfof("user %v deleted login %v", CmsUserFromReq(r).Username, username)
		return CreateRespData(http.StatusOK, ContentTypeValueText, []byte{})
	}
}

func addLoginAuditLogFields(action string, h EndpointHandler) EndpointHandler {
	return func(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
		d := h(app, w, r)
		user := CmsUserFromReq(r)
		fields := make(map[string]interface{})
		fields["audit"] = "login"
		fields["action"] = action
		fields["user"] = user.Username
		CtxLoggerFromReq(r).AddFields(fields)
		return d
	}
}

func Login() EndpointHandler {
	return login
}

func LoginCreate() EndpointHandler {
	h := addLoginAuditLogFields("create", createLogin)
	h = RequireOneRole(CmsRoleLoginManage, h)
	return RequireAuth(h)
}

func LoginUpdate() EndpointHandler {
	h := addLoginAuditLogFields("update", updateLogin)
	h = RequireOneRole(CmsRoleLoginManage, h)
	return RequireAuth(h)
}

func LoginDelete() EndpointHandler {
	h := addLoginAuditLogFields("delete", deleteLogin)
	h = RequireOneRole(CmsRoleLoginManage, h)
	return RequireAuth(h)
}
