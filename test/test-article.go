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

func (a *Article) VerGuid() string {
	return fmt.Sprintf("%s:%d", a.Guid, a.Version)
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
		a.Tag = []string{action}
		data, err := json.Marshal(a)
		if err != nil {
			return nil, err
		}
		return bytes.NewBuffer(data), nil
	} else {
		return nil, nil
	}
}

func (a *Article) HandleResponseBodyFor(action string, body []byte) error {
	if action == "create" {
		newArticle := &Article{}
		err := json.Unmarshal(body, newArticle)
		if err != nil {
			return err
		}
		//fmt.Printf("\nnew article returned from endpoint: %+v\n", newArticle)
		a.Id = newArticle.Id
		a.Guid = newArticle.Guid
		a.CreatedBy = newArticle.CreatedBy
		a.LockedBy = newArticle.LockedBy
		return nil
	} else if action == "submit-self" || action == "submit-other" {
		newArticle := &Article{}
		err := json.Unmarshal(body, newArticle)
		if err != nil {
			return err
		}
		a.Version = newArticle.Version
		a.FromVersion = newArticle.FromVersion
		a.RevisedAt = newArticle.RevisedAt
		a.RevisedBy = newArticle.RevisedBy
		return nil
	} else {
		return nil
	}
}

func getArticle(client *elastic.Client, index, type_, id string) (*Article, error) {
	get := client.Get()
	get.Index(index)
	get.Type(type_)
	get.Id(id)
	get.FetchSource(true)
	get.Realtime(true)
	resp, err := get.Do(context.Background())
	if err != nil {
		if elastic.IsNotFound(err) {
			return nil, nil
		} else {
			return nil, fmt.Errorf("failed to get article %v, error: %v", type_, err)
		}
	} else {
		a := Article{}
		err := json.Unmarshal(*resp.Source, &a)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal article, error: %v", err)
		} else {
			return &a, nil
		}
	}
}

type ArticleTestCase struct {
	Host         string
	Action       string
	ActionMethod func(string) string
	Article      *Article
	User         *UserToken
	ExpectStatus int

	client       *http.Client
	esclient     *elastic.Client
	articleIndex string
	articleTypes *ArticleTypes
	noIdInUri    bool
}

func (c *ArticleTestCase) Uri() string {
	buf := &bytes.Buffer{}
	fmt.Fprintf(buf, "/article/%s", c.Action)
	if !c.noIdInUri {
		id := c.Article.IdFor(c.Action)
		if len(id) > 0 {
			fmt.Fprintf(buf, "?id=%s", id)
		}
	}
	return buf.String()
}

func (c *ArticleTestCase) Desc() string {
	return fmt.Sprintf("%s %v", c.Uri(), c.ExpectStatus)
}

func (c *ArticleTestCase) Verify() error {
	action := c.Action
	if action == "create" {
		a, err := getArticle(c.esclient, c.articleIndex, c.articleTypes.Draft, c.Article.Guid)
		if err != nil {
			return err
		} else if a == nil {
			return fmt.Errorf("\narticle draft not found!")
		} else {
			//fmt.Printf("article from es: %+v\n", a)
			if a.LockedBy != c.User.Username {
				return fmt.Errorf("expecting locked_by=%v, but got %v", c.User.Username, a.LockedBy)
			} else if a.Guid != a.Id {
				return fmt.Errorf("article id (%v) != guid (%v)", a.Id, a.Guid)
			}
		}
	} else if action == "save" {
		a, err := getArticle(c.esclient, c.articleIndex, c.articleTypes.Draft, c.Article.Guid)
		if err != nil {
			return err
		} else if a == nil {
			return fmt.Errorf("article draft not found!")
		} else {
			if a.Tag == nil || len(a.Tag) != 1 || a.Tag[0] != action {
				return fmt.Errorf("expecting tag=[%s], bot got %v", action, a.Tag)
			}
		}
	} else if action == "submit-self" {
		a, err := getArticle(c.esclient, c.articleIndex, c.articleTypes.Draft, c.Article.Guid)
		if err != nil {
			return err
		} else if a != nil {
			return fmt.Errorf("article draft %s is not deleted!", a.Guid)
		}
		a, err = getArticle(c.esclient, c.articleIndex, c.articleTypes.Version, c.Article.VerGuid())
		if err != nil {
			return err
		} else if a == nil {
			return fmt.Errorf("couldn't find article version %s", c.Article.VerGuid())
		} else {
			//fmt.Printf("\narticle from es: %+v\n", a)
			if c.Article.FromVersion == 0 { // first version
				if a.RevisedBy != "" {
					return fmt.Errorf("first version article has revised_by=%s", a.RevisedBy)
				} else if a.CreatedBy != c.User.Username {
					return fmt.Errorf("expecting created_by=%s, but got %s", c.User.Username, a.CreatedBy)
				}
			} else { // revision
				if a.RevisedBy != c.User.Username {
					return fmt.Errorf("expecting revised_by=%s, but got %s", c.User.Username, a.RevisedBy)
				}
			}
		}
	} else if action == "discard-self" {
		a, err := getArticle(c.esclient, c.articleIndex, c.articleTypes.Draft, c.Article.Guid)
		if err != nil {
			return err
		} else if a != nil {
			fmt.Printf("\narticle from es: %+v\n", a)
			return fmt.Errorf("article draft %s is not deleted!", a.Guid)
		}
		return nil
	} else if action == "submit-other" {
		a, err := getArticle(c.esclient, c.articleIndex, c.articleTypes.Draft, c.Article.Guid)
		if err != nil {
			return err
		} else if a != nil {
			return fmt.Errorf("article draft %s is not deleted!", a.Guid)
		}
		a, err = getArticle(c.esclient, c.articleIndex, c.articleTypes.Version, c.Article.VerGuid())
		if err != nil {
			return err
		} else if a == nil {
			return fmt.Errorf("couldn't find article version %s", c.Article.VerGuid())
		} else {
			if c.Article.FromVersion == 0 { // first version
				if a.RevisedBy != "" {
					return fmt.Errorf("first version article has revised_by=%s", a.RevisedBy)
				} else if a.CreatedBy != c.User.Username {
					return fmt.Errorf("expecting created_by=%s, but got %s", c.User.Username, a.CreatedBy)
				}
			} else { // revision
				if a.RevisedBy != c.User.Username {
					return fmt.Errorf("expecting revised_by=%s, but got %s", c.User.Username, a.RevisedBy)
				}
			}
		}
	} else if action == "discard-other" {
		a, err := getArticle(c.esclient, c.articleIndex, c.articleTypes.Draft, c.Article.Guid)
		if err != nil {
			return err
		} else if a != nil {
			return fmt.Errorf("article draft %s is not deleted!", a.Guid)
		}
		return nil
	} else if action == "edit" {
		a, err := getArticle(c.esclient, c.articleIndex, c.articleTypes.Draft, c.Article.Guid)
		if err != nil {
			return err
		} else if a == nil {
			return fmt.Errorf("article draft %s is not there!", a.Guid)
		}
	} else if action == "publish" {
		a, err := getArticle(c.esclient, c.articleIndex, c.articleTypes.Publish, c.Article.Guid)
		if err != nil {
			return err
		} else if a == nil {
			return fmt.Errorf("article publish %s is not there!", a.VerGuid())
		}
		return nil
	} else if action == "unpublish" {
		a, err := getArticle(c.esclient, c.articleIndex, c.articleTypes.Publish, c.Article.Guid)
		if err != nil {
			return err
		} else if a != nil {
			return fmt.Errorf("article publish %s is not deleted!", a.VerGuid())
		}
	} else {
		return fmt.Errorf("unknown action %v!", action)
	}
	return nil
}

func (c *ArticleTestCase) Run() error {
	url := fmt.Sprintf("http://%v%v", c.Host, c.Uri())
	body, err := c.Article.RequestBodyFor(c.Action)
	if err != nil {
		return err
	}
	method := c.ActionMethod(c.Action)
	//fmt.Printf("method=%s,url=%s,body=(%T,%v)\n", method, url, body, body)
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return err
	}
	if c.User != nil {
		req.Header.Set(HeaderAuthToken, c.User.Token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body, error :%v", err)
	}
	if resp.StatusCode == c.ExpectStatus {
		if c.ExpectStatus == http.StatusOK {
			c.Article.HandleResponseBodyFor(c.Action, data)
			return c.Verify()
		} else {
			return nil
		}
	} else {
		return fmt.Errorf("got %v (%v)", resp.StatusCode, string(data))
	}
}

type ArticleTypes struct {
	Draft   string
	Version string
	Publish string
}

type UserToken struct {
	Username string
	Password string
	Role     []string
	Token    string
}

type ArticleTestCaseGroup struct {
	desc         string
	host         string
	hclient      *http.Client
	esclient     *elastic.Client
	stopOnNG     bool
	userIndex    string
	userType     string
	articleIndex string
	articleTypes *ArticleTypes
	createUser   *UserToken
	editUser     *UserToken
	submitUser   *UserToken
	publishUser  *UserToken
	godUser      *UserToken
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
	// create users then get token
	for _, u := range []*UserToken{g.createUser, g.editUser, g.submitUser, g.publishUser, g.godUser} {
		//fmt.Println("username:", u.Username, "role:", u.Role)
		err = CreateUser(g.esclient, g.userIndex, g.userType, u.Username, u.Password, u.Role)
		if err != nil {
			return fmt.Errorf("failed to create user %v, error: %v", u.Username, err)
		}
		token, err := GetAuthToken(g.hclient, g.host, u.Username, u.Password)
		if err != nil {
			return fmt.Errorf("failed to get token for user %v, error: %v", u.Username, err)
		}
		u.Token = token
	}
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
	err = DeleteDocs(g.esclient, g.userIndex, "username", g.createUser.Username, g.editUser.Username, g.submitUser.Username, g.publishUser.Username, g.godUser.Username)
	if err != nil {
		return fmt.Errorf("failed to delete test users: %v", err)
	}
	return nil
}

func (g *ArticleTestCaseGroup) GetTestCases() ([]TestCase, error) {
	var testCase = func(action string, a *Article, user *UserToken, sts int) *ArticleTestCase {
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
		return &ArticleTestCase{
			Host:   g.host,
			Action: action,
			ActionMethod: func(action string) string {
				if method, ok := methodActionMap[action]; ok {
					return method
				} else {
					return http.MethodPut
				}
			},
			Article:      a,
			User:         user,
			ExpectStatus: sts,

			client:       g.hclient,
			esclient:     g.esclient,
			articleIndex: g.articleIndex,
			articleTypes: g.articleTypes,
		}
	}
	var wrongMethodTestCase = func(action string, a *Article, user *UserToken, sts int) *ArticleTestCase {
		c := testCase(action, a, user, sts)
		c.ActionMethod = func(action string) string {
			return http.MethodPut
		}
		return c
	}

	var noArticleIdTestCase = func(action string, a *Article, user *UserToken, sts int) *ArticleTestCase {
		c := testCase(action, a, user, sts)
		c.noIdInUri = true
		return c
	}

	userWithBadToken := &UserToken{"user", "pass", []string{}, "badtoken"}

	cases := make([]TestCase, 0)

	a := &Article{Guid: "aaaaa"}

	// wrong request method
	cases = append(cases, wrongMethodTestCase("create", a, nil, 405))
	cases = append(cases, wrongMethodTestCase("save", a, nil, 405))
	cases = append(cases, wrongMethodTestCase("submit-self", a, nil, 405))
	cases = append(cases, wrongMethodTestCase("discard-self", a, nil, 405))
	cases = append(cases, wrongMethodTestCase("submit-other", a, nil, 405))
	cases = append(cases, wrongMethodTestCase("discard-other", a, nil, 405))
	cases = append(cases, wrongMethodTestCase("edit", a, nil, 405))
	cases = append(cases, wrongMethodTestCase("publish", a, nil, 405))
	cases = append(cases, wrongMethodTestCase("unpublish", a, nil, 405))

	// no token
	cases = append(cases, testCase("create", a, nil, 403))
	cases = append(cases, testCase("save", a, nil, 403))
	cases = append(cases, testCase("submit-self", a, nil, 403))
	cases = append(cases, testCase("discard-self", a, nil, 403))
	cases = append(cases, testCase("submit-other", a, nil, 403))
	cases = append(cases, testCase("discard-other", a, nil, 403))
	cases = append(cases, testCase("edit", a, nil, 403))
	cases = append(cases, testCase("publish", a, nil, 403))
	cases = append(cases, testCase("unpublish", a, nil, 403))

	// wrong token
	cases = append(cases, testCase("create", a, g.editUser, 403))
	cases = append(cases, testCase("save", a, g.submitUser, 403))
	cases = append(cases, testCase("submit-self", a, g.publishUser, 403))
	cases = append(cases, testCase("discard-self", a, g.publishUser, 403))
	cases = append(cases, testCase("submit-other", a, g.editUser, 403))
	cases = append(cases, testCase("discard-other", a, g.editUser, 403))
	cases = append(cases, testCase("edit", a, g.createUser, 403))
	cases = append(cases, testCase("publish", a, g.editUser, 403))
	cases = append(cases, testCase("unpublish", a, g.editUser, 403))

	// bad token
	cases = append(cases, testCase("create", a, userWithBadToken, 403))
	cases = append(cases, testCase("save", a, userWithBadToken, 403))
	cases = append(cases, testCase("submit-self", a, userWithBadToken, 403))
	cases = append(cases, testCase("discard-self", a, userWithBadToken, 403))
	cases = append(cases, testCase("submit-other", a, userWithBadToken, 403))
	cases = append(cases, testCase("discard-other", a, userWithBadToken, 403))
	cases = append(cases, testCase("edit", a, userWithBadToken, 403))
	cases = append(cases, testCase("publish", a, userWithBadToken, 403))
	cases = append(cases, testCase("unpublish", a, userWithBadToken, 403))

	// missing article id
	cases = append(cases, noArticleIdTestCase("save", a, g.createUser, 400))
	cases = append(cases, noArticleIdTestCase("submit-self", a, g.createUser, 400))
	cases = append(cases, noArticleIdTestCase("discard-self", a, g.createUser, 400))
	cases = append(cases, noArticleIdTestCase("submit-other", a, g.submitUser, 400))
	cases = append(cases, noArticleIdTestCase("discard-other", a, g.submitUser, 400))
	cases = append(cases, noArticleIdTestCase("edit", a, g.editUser, 400))
	cases = append(cases, noArticleIdTestCase("publish", a, g.publishUser, 400))
	cases = append(cases, noArticleIdTestCase("unpublish", a, g.publishUser, 400))

	// create --> save --> submit-self --> publish --> unpublish
	a = &Article{}
	cases = append(cases, testCase("create", a, g.createUser, 200))
	cases = append(cases, testCase("save", a, g.createUser, 200))
	cases = append(cases, testCase("submit-self", a, g.createUser, 200))
	cases = append(cases, testCase("publish", a, g.publishUser, 200))
	cases = append(cases, testCase("unpublish", a, g.publishUser, 200))

	// create --> save --> submit-other --> publish --> unpublish
	a = &Article{}
	cases = append(cases, testCase("create", a, g.createUser, 200))
	cases = append(cases, testCase("save", a, g.createUser, 200))
	cases = append(cases, testCase("submit-other", a, g.submitUser, 200))
	cases = append(cases, testCase("publish", a, g.publishUser, 200))
	cases = append(cases, testCase("unpublish", a, g.publishUser, 200))

	// create --> save --> submit-self --> edit --> submit-self --> publish --> unpublish
	a = &Article{}
	cases = append(cases, testCase("create", a, g.createUser, 200))
	cases = append(cases, testCase("save", a, g.createUser, 200))
	cases = append(cases, testCase("submit-self", a, g.createUser, 200))
	cases = append(cases, testCase("edit", a, g.editUser, 200))
	cases = append(cases, testCase("submit-self", a, g.editUser, 200))
	cases = append(cases, testCase("publish", a, g.publishUser, 200))
	cases = append(cases, testCase("unpublish", a, g.publishUser, 200))

	// create --> save --> submit-self --> edit --> submit-other --> publish --> unpublish
	a = &Article{}
	cases = append(cases, testCase("create", a, g.createUser, 200))
	cases = append(cases, testCase("save", a, g.createUser, 200))
	cases = append(cases, testCase("submit-self", a, g.createUser, 200))
	cases = append(cases, testCase("edit", a, g.editUser, 200))
	cases = append(cases, testCase("submit-other", a, g.submitUser, 200))
	cases = append(cases, testCase("publish", a, g.publishUser, 200))
	cases = append(cases, testCase("unpublish", a, g.publishUser, 200))

	// create --> save --> discard-self
	a = &Article{}
	cases = append(cases, testCase("create", a, g.createUser, 200))
	cases = append(cases, testCase("save", a, g.createUser, 200))
	cases = append(cases, testCase("discard-self", a, g.createUser, 200))

	// create --> save --> discard-other
	a = &Article{}
	cases = append(cases, testCase("create", a, g.createUser, 200))
	cases = append(cases, testCase("save", a, g.createUser, 200))
	cases = append(cases, testCase("discard-other", a, g.submitUser, 200))

	// create --> save --> submit-self --> edit --> discard-self
	a = &Article{}
	cases = append(cases, testCase("create", a, g.createUser, 200))
	cases = append(cases, testCase("save", a, g.createUser, 200))
	cases = append(cases, testCase("submit-self", a, g.createUser, 200))
	cases = append(cases, testCase("edit", a, g.editUser, 200))
	cases = append(cases, testCase("discard-self", a, g.editUser, 200))

	// create --> save --> submit-self --> edit --> discard-other
	a = &Article{}
	cases = append(cases, testCase("create", a, g.createUser, 200))
	cases = append(cases, testCase("save", a, g.createUser, 200))
	cases = append(cases, testCase("submit-self", a, g.createUser, 200))
	cases = append(cases, testCase("edit", a, g.editUser, 200))
	cases = append(cases, testCase("discard-other", a, g.submitUser, 200))

	// create --> discard-self
	a = &Article{}
	cases = append(cases, testCase("create", a, g.createUser, 200))
	cases = append(cases, testCase("discard-self", a, g.createUser, 200))

	// create --> discard-other
	a = &Article{}
	cases = append(cases, testCase("create", a, g.createUser, 200))
	cases = append(cases, testCase("discard-other", a, g.submitUser, 200))

	// god plays here
	a = &Article{}
	cases = append(cases, testCase("create", a, g.godUser, 200))
	cases = append(cases, testCase("save", a, g.godUser, 200))
	cases = append(cases, testCase("submit-self", a, g.godUser, 200))
	cases = append(cases, testCase("edit", a, g.godUser, 200))
	cases = append(cases, testCase("submit-other", a, g.godUser, 200))
	cases = append(cases, testCase("publish", a, g.godUser, 200))
	cases = append(cases, testCase("unpublish", a, g.godUser, 200))

	a = &Article{}
	cases = append(cases, testCase("create", a, g.godUser, 200))
	cases = append(cases, testCase("save", a, g.godUser, 200))
	cases = append(cases, testCase("discard-self", a, g.godUser, 200))

	a = &Article{}
	cases = append(cases, testCase("create", a, g.godUser, 200))
	cases = append(cases, testCase("save", a, g.godUser, 200))
	cases = append(cases, testCase("discard-other", a, g.godUser, 200))

	a = &Article{}
	cases = append(cases, testCase("create", a, g.godUser, 200))
	cases = append(cases, testCase("discard-self", a, g.godUser, 200))

	a = &Article{}
	cases = append(cases, testCase("create", a, g.godUser, 200))
	cases = append(cases, testCase("discard-other", a, g.godUser, 200))

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
		createUser:  &UserToken{"_test_user_create", "000", []string{"article:create"}, ""},
		editUser:    &UserToken{"_test_user_edit", "123", []string{"article:edit"}, ""},
		submitUser:  &UserToken{"_test_user_submit", "456", []string{"article:submit"}, ""},
		publishUser: &UserToken{"_test_user_publish", "789", []string{"article:publish"}, ""},
		godUser:     &UserToken{"_test_user_god", "666", []string{"article:create", "article:edit", "article:submit", "article:publish"}, ""},
	}
}
