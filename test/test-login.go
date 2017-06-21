package main

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"

	"github.com/yizha/elastic"
)

const HeaderAuthToken = "X-Auth-Token"

type LoginTestCase struct {
	Host         string
	Uri          string
	AuthToken    func() (string, error)
	ExpectStatus int

	client *http.Client
}

func (c *LoginTestCase) Desc() string {
	return fmt.Sprintf("%s %v", c.Uri, c.ExpectStatus)
	/*if c.AuthToken != nil {
		return fmt.Sprintf("%s (token) %v", c.Uri, c.ExpectStatus)
	} else {
		return fmt.Sprintf("%s %v", c.Uri, c.ExpectStatus)
	}*/
}

func (c *LoginTestCase) Run() error {
	url := fmt.Sprintf("http://%v%v", c.Host, c.Uri)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if c.AuthToken != nil {
		token, err := c.AuthToken()
		if err != nil {
			return fmt.Errorf("failed to get auth token, error: %v", err)
		}
		req.Header.Set(HeaderAuthToken, token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode == c.ExpectStatus {
		return nil
	} else {
		return fmt.Errorf("got %v", resp.StatusCode)
	}
}

type LoginTestCaseGroup struct {
	desc           string
	host           string
	hclient        *http.Client
	esclient       *elastic.Client
	stopOnNG       bool
	userIndex      string
	userType       string
	rootUserName   string
	rootUserPass   string
	mgrUserName    string
	mgrUserPass    string
	nonMgrUserName string
	nonMgrUserPass string
}

func (g *LoginTestCaseGroup) Desc() string {
	return g.desc
}

func (g *LoginTestCaseGroup) StopOnNG() bool {
	return g.stopOnNG
}

func (g *LoginTestCaseGroup) Setup() error {
	err := g.TearDown()
	if err != nil {
		return err
	}
	// create root user
	return CreateUser(g.esclient, g.userIndex, g.userType, g.rootUserName, g.rootUserPass, []string{"login:manage"})
}

func (g *LoginTestCaseGroup) TearDown() error {
	// delete test users
	err := DeleteDocs(g.esclient, g.userIndex, "username", g.mgrUserName, g.nonMgrUserName, g.rootUserName)
	if err != nil {
		return fmt.Errorf("failed to delete test users, error: %v", err)
	}
	return nil
}

func loginUri(path, username, password, roles string) string {
	buf := &bytes.Buffer{}
	fmt.Fprintf(buf, "/api%s", path)
	firstArg := true
	addArg := func(b *bytes.Buffer, name, val string) {
		if len(val) > 0 {
			if firstArg {
				firstArg = false
				fmt.Fprint(b, "?")
			} else {
				fmt.Fprint(b, "&")
			}
			fmt.Fprintf(b, "%s=%s", name, strings.TrimSpace(val))
		}
	}
	addArg(buf, "username", username)
	addArg(buf, "password", password)
	addArg(buf, "role", roles)
	return buf.String()
}

func (g *LoginTestCaseGroup) GetTestCases() ([]TestCase, error) {
	var loginCase = func(uri string, token func() (string, error), expectStatus int) *LoginTestCase {
		return &LoginTestCase{g.host, uri, token, expectStatus, g.hclient}
	}

	tokens := make(map[string]string)
	var cacheTokenFunc = func(name, pass string) func() (string, error) {
		return func() (string, error) {
			if t, ok := tokens[name]; ok {
				return t, nil
			} else {
				t, err := GetAuthToken(g.hclient, g.host, name, pass)
				if err != nil {
					return "", err
				} else {
					tokens[name] = t
					return t, nil
				}
			}
		}
	}
	rootToken := cacheTokenFunc(g.rootUserName, g.rootUserPass)
	mgrToken := cacheTokenFunc(g.mgrUserName, g.mgrUserPass)
	nonMgrToken := cacheTokenFunc(g.nonMgrUserName, g.nonMgrUserPass)

	var tokenFunc = func(name, pass string) func() (string, error) {
		return func() (string, error) {
			t, err := GetAuthToken(g.hclient, g.host, name, pass)
			if err != nil {
				return "", err
			} else {
				return t, nil
			}
		}
	}

	var badToken = func() (string, error) { return "asdf", nil }

	cases := make([]TestCase, 0)

	// missing args
	cases = append(cases, loginCase(loginUri("/login", "", "", ""), nil, 400))
	cases = append(cases, loginCase(loginUri("/login", "xyz", "", ""), nil, 400))
	cases = append(cases, loginCase(loginUri("/login", "", "xyz", ""), nil, 400))

	// no such user
	cases = append(cases, loginCase(loginUri("/login", "xyz", "xyz", ""), nil, 403))

	// wrong password
	cases = append(cases, loginCase(loginUri("/login", g.rootUserName, "xyz", ""), nil, 403))

	// good
	cases = append(cases, loginCase(loginUri("/login", g.rootUserName, g.rootUserPass, ""), nil, 200))

	// no token
	cases = append(cases, loginCase(loginUri("/login/create", "", "", ""), nil, 403))
	cases = append(cases, loginCase(loginUri("/login/create", "", "", ""), nil, 403))
	cases = append(cases, loginCase(loginUri("/login/create", "user1", "", ""), nil, 403))
	cases = append(cases, loginCase(loginUri("/login/create", "", "pass1", ""), nil, 403))

	// missing args
	cases = append(cases, loginCase(loginUri("/login/create", "", "", ""), rootToken, 400))
	cases = append(cases, loginCase(loginUri("/login/create", "user1", "", ""), rootToken, 400))
	cases = append(cases, loginCase(loginUri("/login/create", "", "pass1", ""), rootToken, 400))

	// create user
	cases = append(cases, loginCase(loginUri("/login", g.nonMgrUserName, g.nonMgrUserPass, ""), nil, 403))
	cases = append(cases, loginCase(loginUri("/login/create", g.nonMgrUserName, g.nonMgrUserPass, ""), rootToken, 200))
	cases = append(cases, loginCase(loginUri("/login", g.nonMgrUserName, g.nonMgrUserPass, ""), nil, 200))

	// create dup user
	cases = append(cases, loginCase(loginUri("/login/create", g.nonMgrUserName, g.nonMgrUserPass, ""), rootToken, 409))

	// non mgr user cannot manage login
	cases = append(cases, loginCase(loginUri("/login/create", g.nonMgrUserName, g.nonMgrUserPass, ""), nonMgrToken, 403))
	cases = append(cases, loginCase(loginUri("/login/update", g.nonMgrUserName, g.nonMgrUserPass, ""), nonMgrToken, 403))
	cases = append(cases, loginCase(loginUri("/login/delete", g.nonMgrUserName, g.nonMgrUserPass, ""), nonMgrToken, 403))

	// create a manage user
	cases = append(cases, loginCase(loginUri("/login/create", g.mgrUserName, g.mgrUserPass, "login:manage"), rootToken, 200))

	// update self
	cases = append(cases, loginCase(loginUri("/login/update", g.nonMgrUserName, "newpass", "login:manage"), mgrToken, 200))

	// update another user's password and role
	cases = append(cases, loginCase(loginUri("/login", g.nonMgrUserName, "newpass", ""), nil, 200))

	// updated user (who now has manage role) still cannot delete self with old token
	cases = append(cases, loginCase(loginUri("/login/delete", g.nonMgrUserName, "", ""), nonMgrToken, 403))

	// updated user (who now has manage role) can delete itself with a newly created token
	cases = append(cases, loginCase(loginUri("/login/delete", g.nonMgrUserName, "", ""), tokenFunc(g.nonMgrUserName, "newpass"), 200))

	// try to login the deleted user again to make sure it is actually deleted
	cases = append(cases, loginCase(loginUri("/login", g.nonMgrUserName, "newpass", ""), nil, 403))

	// remove manage role from self
	cases = append(cases, loginCase(loginUri("/login/update", g.mgrUserName, "", " "), mgrToken, 200))

	// cannot delete with newly generated token after manage role is removed
	cases = append(cases, loginCase(loginUri("/login/delete", g.mgrUserName, "", ""), tokenFunc(g.mgrUserName, g.mgrUserPass), 403))

	// but still can delete self with previsouly generated token
	cases = append(cases, loginCase(loginUri("/login/delete", g.mgrUserName, "", ""), mgrToken, 200))

	// access with bad token
	cases = append(cases, loginCase(loginUri("/login/create", "user1", "pass1", ""), badToken, 403))
	cases = append(cases, loginCase(loginUri("/login/update", "user1", "pass2", ""), badToken, 403))
	cases = append(cases, loginCase(loginUri("/login/delete", "user1", "pass2", ""), badToken, 403))

	return cases, nil
}

func GetLoginTests(host string, hclient *http.Client, esclient *elastic.Client) TestCaseGroup {
	return &LoginTestCaseGroup{
		desc:           "Login API Group",
		host:           host,
		hclient:        hclient,
		esclient:       esclient,
		stopOnNG:       true,
		userIndex:      "user",
		userType:       "user",
		rootUserName:   "_test_root_username",
		rootUserPass:   "000",
		mgrUserName:    "_test_mgr_user",
		mgrUserPass:    "123",
		nonMgrUserName: "_test_non_mgr_user",
		nonMgrUserPass: "456",
	}
}
