package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/yizha/elastic"
)

type Article struct {
	Id          string   `json:"id,omitempty"`
	Guid        string   `json:"guid,omitempty"`
	Version     int64    `json:"version,omitempty"`
	Headline    string   `json:"headline,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Content     string   `json:"content,omitempty"`
	Tag         []string `json:"tag,omitempty"`
	Note        string   `json:"note,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	CreatedBy   string   `json:"created_by,omitempty"`
	RevisedAt   string   `json:"revised_at,omitempty"`
	RevisedBy   string   `json:"revised_by,omitempty"`
	FromVersion int64    `json:"from_version,omitempty"`
	LockedBy    string   `json:"locked_by,omitempty"`
}

func (a *Article) IdFor(action string) string {
	if action == "create" {
		return ""
	} else if action == "save" || action == "submit-self" || action == "submit-other" || action == "discard-self" || action == "discard-other" || action == "unpublish" {
		return a.Guid
	} else { // edit, publish
		return fmt.Sprintf("%s:%v", a.Guid, a.Version)
	}
}

func (a *Article) RequestBodyFor(action string) (io.Reader, error) {
	if action == "save" || action == "submit-self" {
		a.Headline = fmt.Sprintf("test headline create at %v", time.Now().UTC())
		data, err := json.Marshal(a)
		if err != nil {
			return nil, err
		}
		return bytes.NewBuffer(data), nil
	} else {
		return nil, nil
	}
}

func (a *Article) HandleResponseBodyFor(action string, body io.Reader) error {
	if action == "create" {
		data, err := ioutil.ReadAll(body)
		if err != nil {
			return err
		}
		newArticle := &Article{}
		err = json.Unmarshal(data, newArticle)
		if err != nil {
			return err
		}
		a.Id = newArticle.Id
		a.Guid = newArticle.Guid
		a.CreatedBy = newArticle.CreatedBy
		a.LockedBy = newArticle.LockedBy
		return nil
	} else if action == "submit-self" || action == "submit-other" {
		data, err := ioutil.ReadAll(body)
		if err != nil {
			return err
		}
		newArticle := &Article{}
		err = json.Unmarshal(data, newArticle)
		if err != nil {
			return err
		}
		a.Version = newArticle.Version
		a.RevisedAt = newArticle.RevisedAt
		a.RevisedBy = newArticle.RevisedBy
		return nil
	} else {
		return nil
	}
}

type ArticleTestCase struct {
	Host         string
	Action       string
	ActionMethod func(string) string
	Article      *Article
	AuthToken    string
	ExpectStatus int

	client   *http.Client
	esclient *elastic.Client
}

func (c *ArticleTestCase) Uri() string {
	buf := &bytes.Buffer{}
	fmt.Fprintf(buf, "/article/%s", c.Action)
	id := c.Article.IdFor(c.Action)
	if len(id) > 0 {
		fmt.Fprintf(buf, "?id=%s", id)
	}
	return buf.String()
}

func (c *ArticleTestCase) Desc() string {
	return fmt.Sprintf("%s %v", c.Uri(), c.ExpectStatus)
}

func (c *ArticleTestCase) Verify() error {
	return nil
}

func (c *ArticleTestCase) Run() error {
	url := fmt.Sprintf("http://%v%v", c.Host, c.Uri())
	body, err := c.Article.RequestBodyFor(c.Action)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(c.ActionMethod(c.Action), url, body)
	if err != nil {
		return err
	}
	if c.AuthToken != "" {
		req.Header.Set(HeaderAuthToken, c.AuthToken)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == c.ExpectStatus {
		if c.ExpectStatus == http.StatusOK {
			c.Article.HandleResponseBodyFor(c.Action, resp.Body)
			return c.Verify()
		} else {
			return nil
		}
	} else {
		return fmt.Errorf("got %v", resp.StatusCode)
	}
}

type ArticleTypes struct {
	Draft   string
	Version string
	Publish string
}

type ArticleTestCaseGroup struct {
	desc            string
	host            string
	hclient         *http.Client
	esclient        *elastic.Client
	stopOnNG        bool
	userIndex       string
	userType        string
	articleIndex    string
	articleTypes    *ArticleTypes
	createUserName  string
	createUserPass  string
	createUserRole  string
	editUserName    string
	editUserPass    string
	editUserRole    string
	submitUserName  string
	submitUserPass  string
	submitUserRole  string
	publishUserName string
	publishUserPass string
	publishUserRole string
	godName         string
	godPass         string
	godRole         string

	createToken  string
	editToken    string
	submitToken  string
	publishToken string
	godToken     string
}

func (g *ArticleTestCaseGroup) Desc() string {
	return g.desc
}

func (g *ArticleTestCaseGroup) StopOnNG() bool {
	return g.stopOnNG
}

func (g *ArticleTestCaseGroup) Setup() error {
	err := g.TearDown()
	if err != nil {
		return err
	}
	// create users
	for _, data := range [][]string{
		[]string{g.createUserName, g.createUserPass, g.createUserRole},
		[]string{g.editUserName, g.editUserPass, g.editUserRole},
		[]string{g.submitUserName, g.submitUserPass, g.submitUserRole},
		[]string{g.publishUserName, g.publishUserPass, g.publishUserRole},
		[]string{g.godName, g.godPass, g.godRole},
	} {
		err = CreateUser(g.esclient, g.userIndex, g.userType, data[0], data[1], data[2])
		if err != nil {
			return fmt.Errorf("failed to create user %v, error: %v", data[0], err)
		}
	}
	// get tokens
	createToken, err := GetAuthToken(g.hclient, g.host, g.createUserName, g.createUserPass)
	if err != nil {
		return err
	}
	g.createToken = createToken
	editToken, err := GetAuthToken(g.hclient, g.host, g.editUserName, g.editUserPass)
	if err != nil {
		return err
	}
	g.editToken = editToken
	submitToken, err := GetAuthToken(g.hclient, g.host, g.submitUserName, g.submitUserPass)
	if err != nil {
		return err
	}
	g.submitToken = submitToken
	publishToken, err := GetAuthToken(g.hclient, g.host, g.publishUserName, g.publishUserPass)
	if err != nil {
		return err
	}
	g.publishToken = publishToken
	godToken, err := GetAuthToken(g.hclient, g.host, g.godName, g.godPass)
	if err != nil {
		return err
	}
	g.godToken = godToken
	return nil
}

func (g *ArticleTestCaseGroup) TearDown() error {
	// delete ALL articles
	del := g.esclient.DeleteByQuery(g.articleIndex)
	del.Refresh("wait_for")
	del.Query(elastic.NewMatchAllQuery())
	ctx := context.Background()
	_, err := del.Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete test articles: %v", err)
	}
	// delete users
	err = DeleteDocs(g.esclient, g.userIndex, "username", g.createUserName, g.editUserName, g.submitUserName, g.publishUserName, g.godName)
	if err != nil {
		return fmt.Errorf("failed to delete test docs: %v", err)
	}
	return nil
}

func (g *ArticleTestCaseGroup) GetTestCases() ([]TestCase, error) {
	methodActionMap := map[string]string{
		"create":        http.MethodGet,
		"save":          http.MethodPost,
		"submit-self":   http.MethodPost,
		"discard-self":  http.MethodGet,
		"submit-other":  http.MethodGet,
		"discard-other": http.MethodGet,
		"edit":          http.MethodGet,
		"publish":       http.MethodGet,
		"unpublish":     http.MethodGet,
	}

	var goodMethodFunc = func(action string) string {
		if method, ok := methodActionMap[action]; ok {
			return method
		} else {
			return http.MethodPut
		}
	}
	var badMethodFunc = func(action string) string {
		return http.MethodPut
	}

	var testCase = func(action string, a *Article, token string, sts int) *ArticleTestCase {
		return &ArticleTestCase{g.host, action, goodMethodFunc, a, token, sts, g.hclient, g.esclient}
	}
	var wrongMethodTestCase = func(action string, a *Article, token string, sts int) *ArticleTestCase {
		return &ArticleTestCase{g.host, action, badMethodFunc, a, token, sts, g.hclient, g.esclient}
	}

	badToken := "aasdfaf"

	cases := make([]TestCase, 0)

	a := &Article{Guid: "aaaaa"}
	// wrong request method
	cases = append(cases, wrongMethodTestCase("create", a, "", 405))
	cases = append(cases, wrongMethodTestCase("save", a, "", 405))
	cases = append(cases, wrongMethodTestCase("submit-self", a, "", 405))
	cases = append(cases, wrongMethodTestCase("discard-self", a, "", 405))
	cases = append(cases, wrongMethodTestCase("submit-other", a, "", 405))
	cases = append(cases, wrongMethodTestCase("discard-other", a, "", 405))
	cases = append(cases, wrongMethodTestCase("edit", a, "", 405))
	cases = append(cases, wrongMethodTestCase("publish", a, "", 405))
	cases = append(cases, wrongMethodTestCase("unpublish", a, "", 405))

	// no token
	cases = append(cases, testCase("create", a, "", 403))
	cases = append(cases, testCase("save", a, "", 403))
	cases = append(cases, testCase("submit-self", a, "", 403))
	cases = append(cases, testCase("discard-self", a, "", 403))
	cases = append(cases, testCase("submit-other", a, "", 403))
	cases = append(cases, testCase("discard-other", a, "", 403))
	cases = append(cases, testCase("edit", a, "", 403))
	cases = append(cases, testCase("publish", a, "", 403))
	cases = append(cases, testCase("unpublish", a, "", 403))

	// good token but missing correct role
	cases = append(cases, testCase("create", a, g.editToken, 403))
	cases = append(cases, testCase("save", a, g.editToken, 403))
	cases = append(cases, testCase("submit-self", a, g.editToken, 403))
	cases = append(cases, testCase("discard-self", a, g.editToken, 403))
	cases = append(cases, testCase("submit-other", a, g.editToken, 403))
	cases = append(cases, testCase("discard-other", a, g.editToken, 403))
	cases = append(cases, testCase("edit", a, g.editToken, 403))
	cases = append(cases, testCase("publish", a, g.editToken, 403))
	cases = append(cases, testCase("unpublish", a, g.editToken, 403))

	// bad token
	cases = append(cases, testCase("create", a, badToken, 403))
	cases = append(cases, testCase("save", a, badToken, 403))
	cases = append(cases, testCase("submit-self", a, badToken, 403))
	cases = append(cases, testCase("discard-self", a, badToken, 403))
	cases = append(cases, testCase("submit-other", a, badToken, 403))
	cases = append(cases, testCase("discard-other", a, badToken, 403))
	cases = append(cases, testCase("edit", a, badToken, 403))
	cases = append(cases, testCase("publish", a, badToken, 403))
	cases = append(cases, testCase("unpublish", a, badToken, 403))

	return cases, nil
}

func GetArticleTests(host string, hclient *http.Client, esclient *elastic.Client) TestCaseGroup {
	return &ArticleTestCaseGroup{
		desc:         "Article API Group",
		host:         host,
		hclient:      hclient,
		esclient:     esclient,
		stopOnNG:     true,
		userIndex:    "user",
		userType:     "user",
		articleIndex: "article",
		articleTypes: &ArticleTypes{
			Draft:   "draft",
			Version: "version",
			Publish: "publish",
		},
		createUserName:  "_test_user_create",
		createUserPass:  "000",
		createUserRole:  "article:create",
		editUserName:    "_test_user_edit",
		editUserPass:    "123",
		editUserRole:    "article:edit",
		submitUserName:  "_test_user_submit",
		submitUserPass:  "456",
		submitUserRole:  "article:submit",
		publishUserName: "_test_user_publish",
		publishUserPass: "789",
		publishUserRole: "article:publish",
		godName:         "god",
		godPass:         "666",
		godRole:         "article:create,article:edit,article:submit,article:publish",
	}
}
