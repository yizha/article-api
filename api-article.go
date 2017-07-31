/*
   /article/create        GET   [draft (create)]                                        no lock
   /article/edit          GET   [version (read) --> draft (create)]                     lock on draft
   /article/save          POST  [draft (update)]                                        lock on draft
   /article/submit-self   POST  [draft (save) --> version (create) --> draft (delete)]  lock on draft
   /article/submit-other  GET   [draft (read) --> version (create) --> draft (delete)]  lock on draft
   /article/discard-self  GET   [draft (delete)]                                        lock on draft
   /article/discard-other GET   [draft (delete)]                                        lock on draft
   /article/publish       GET   [version (read) --> publish (upsert)]                   lock on publish
   /article/unpublish     GET   [publish (delete)]                                      lock on publish
*/

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	//elastic "gopkg.in/olivere/elastic.v5"
	elastic "github.com/yizha/elastic"
)

const (
	ESScriptSaveArticle = `
if (ctx._source.locked_by != params.username) {
  ctx.op = "none"
} else {
  ctx._source.guid = params.guid;
  ctx._source.headline = params.headline;
  ctx._source.summary = params.summary;
  ctx._source.content = params.content;
  ctx._source.tag = params.tag;
  ctx._source.note = params.note;
  ctx._source.revised_at = params.revised_at;
}`

	ESScriptDiscardArticle = `
if (ctx._source.locked_by != params.username) {
  ctx.op = "none"
} else {
  ctx.op = "delete"
}
`
)

var (
	draftLock   = NewUniqStrMutex()
	publishLock = NewUniqStrMutex()
)

type JSONTime struct {
	T time.Time
}

func (t *JSONTime) MarshalJSON() ([]byte, error) {
	s := fmt.Sprintf(`"%s"`, t.T.Format("2006-01-02T15:04:05.000Z"))
	return []byte(s), nil
}

func (t *JSONTime) UnmarshalJSON(data []byte) error {
	s := string(data)
	if s == `"null"` {
		// set T to the zero value of time.Time
		t.T = time.Date(1, 1, 1, 0, 0, 0, 0, time.FixedZone("UTC", 0))
		return nil
	}
	size := len(s)
	if size != 26 || s[0] != '"' || s[25] != '"' {
		return fmt.Errorf("invalid datetime string: %v", s)
	}
	var err error
	t.T, err = time.Parse("2006-01-02T15:04:05.000Z", s[1:25])
	return err
}

type Article struct {
	Id          string    `json:"id,omitempty"`
	Guid        string    `json:"guid"`
	Version     string    `json:"version,omitempty"`
	Headline    string    `json:"headline"`
	Summary     string    `json:"summary"`
	Content     string    `json:"content"`
	Tag         []string  `json:"tag"`
	Note        string    `json:"note"`
	CreatedAt   *JSONTime `json:"created_at"`
	CreatedBy   string    `json:"created_by"`
	RevisedAt   *JSONTime `json:"revised_at"`
	RevisedBy   string    `json:"revised_by"`
	FromVersion string    `json:"from_version"`
	LockedBy    string    `json:"locked_by,omitempty"`
}

func (a *Article) NilZeroTimeFields() *Article {
	if a.CreatedAt != nil && a.CreatedAt.T.IsZero() {
		a.CreatedAt = nil
	}
	if a.RevisedAt != nil && a.RevisedAt.T.IsZero() {
		a.RevisedAt = nil
	}
	return a
}

func unmarshalArticle(data []byte) (*Article, error) {
	var a Article
	err := json.Unmarshal(data, &a)
	if err != nil {
		return nil, err
	}
	return (&a).NilZeroTimeFields(), nil
}

func getFullArticle(
	client *elastic.Client,
	ctx context.Context,
	index, typ, id string,
	logger *JsonLogger) (*Article, *HttpResponseData) {
	source := elastic.NewFetchSourceContext(true).Include(
		"guid",
		"headline",
		"summary",
		"content",
		"tag",
		"created_at",
		"created_by",
		"revised_at",
		"revised_by",
		"version",
		"from_version",
		"note",
		"locked_by",
	)
	return getArticle(client, ctx, index, typ, id, source, logger)
}

func getArticle(
	client *elastic.Client,
	ctx context.Context,
	index, typ, id string,
	source *elastic.FetchSourceContext,
	logger *JsonLogger) (*Article, *HttpResponseData) {
	getService := client.Get()
	getService.Index(index)
	getService.Type(typ)
	getService.Realtime(true)
	getService.Id(id)
	getService.FetchSourceContext(source)
	resp, err := getService.Do(ctx)
	if err != nil {
		if elastic.IsNotFound(err) {
			body := fmt.Sprintf("article %v not found in index %v type %v!", id, index, typ)
			logger.Perror(body)
			return nil, CreateNotFoundRespData(body)
		} else {
			body := fmt.Sprintf("failed to query elasticsearch, error: %v", err)
			logger.Perror(body)
			return nil, CreateInternalServerErrorRespData(body)
		}
	} else if !resp.Found {
		body := fmt.Sprintf("article %v not found in index %v type %v!", id, index, typ)
		logger.Perror(body)
		return nil, CreateNotFoundRespData(body)
	} else {
		article := &Article{}
		if err := json.Unmarshal(*resp.Source, article); err != nil {
			body := fmt.Sprintf("unmarshal article %v error: %v", id, err)
			logger.Perror(body)
			return nil, CreateInternalServerErrorRespData(body)
		} else {
			article.Id = resp.Id
			return article, nil
		}
	}
}

/*func marshalArticle(a *Article, status int) *HttpResponseData {
	bytes, err := json.Marshal(a)
	if err != nil {
		body := fmt.Sprintf("error marshaling article: %v", err)
		return CreateInternalServerErrorRespData(body)
	} else {
		return CreateRespData(status, ContentTypeValueJSON, string(bytes))
	}
}*/

func parseArticleId(id string) (string, int64, error) {
	idx := strings.LastIndex(id, ":")
	if idx <= 0 {
		return id, int64(0), nil
	} else {
		guid := id[0:idx]
		ver, err := strconv.ParseInt(id[idx+1:], 10, 64)
		if err == nil {
			return guid, ver, nil
		} else {
			return guid, int64(0), err
		}
	}
}

func addArticleAuditLogFields(action string, h EndpointHandler) EndpointHandler {
	return func(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
		d := h(app, w, r)
		id := StringFromReq(r, CtxKeyId)
		if id == "" {
			if a, ok := d.Data.(Article); ok {
				id = a.Id
			}
		}
		fields := make(map[string]interface{})
		fields["audit"] = "article"
		fields["action"] = action
		fields["user"] = CmsUserFromReq(r).Username
		if id != "" {
			guid, ver, err := parseArticleId(id)
			if err == nil {
				fields["article_guid"] = guid
				if ver > 0 {
					fields["article_version"] = ver
				}
			} else {
				CtxLoggerFromReq(r).Perrorf("failed to parse article id %v, error %v", id, err)
			}
		}
		CtxLoggerFromReq(r).AddFields(fields)
		return d
	}
}

func createArticle(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	logger := CtxLoggerFromReq(r)
	username := CmsUserFromReq(r).Username
	t := &JSONTime{time.Now().UTC()}
	article := &Article{
		Headline:    "",
		Summary:     "",
		Content:     "",
		Tag:         []string{},
		Note:        "",
		CreatedBy:   username,
		CreatedAt:   t,
		RevisedBy:   username,
		RevisedAt:   t,
		LockedBy:    username,
		FromVersion: "0",
	}
	// don't set Id or OpType in order to have id auto-generated by elasticsearch
	idxService := app.Elastic.Client.Index()
	idxService.Index(app.Conf.ArticleIndex.Name)
	idxService.Type(app.Conf.ArticleIndexTypes.Draft)
	idxService.BodyJson(article)
	idxService.Refresh("wait_for")
	resp, err := idxService.Do(context.Background())
	if err != nil {
		body := fmt.Sprintf("error creating new article doc: %v", err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	} else {
		article.Id = resp.Id
		if bytes, err := json.Marshal(article); err == nil {
			d := CreateRespData(http.StatusOK, ContentTypeValueJSON, bytes)
			// save article so that we can log auto-generated article-id
			// with context-logger
			d.Data = article
			logger.Pinfof("user %v created article draft %v", username, article.Id)
			return d
		} else {
			body := fmt.Sprintf("failed to marshal article %v, error: %v", article.Id, err)
			logger.Perror(body)
			return CreateInternalServerErrorRespData(body)
		}
	}
}

func saveArticleDraft(app *AppRuntime, user *CmsUser, article *Article, logger *JsonLogger, waitForRefresh bool) (*Article, *HttpResponseData) {
	username := user.Username
	script := elastic.NewScript(ESScriptSaveArticle)
	article.RevisedAt = &JSONTime{time.Now().UTC()}
	script.Type("inline").Lang("painless").Params(map[string]interface{}{
		"guid":       article.Guid,
		"headline":   article.Headline,
		"summary":    article.Summary,
		"content":    article.Content,
		"tag":        article.Tag,
		"note":       article.Note,
		"username":   username,
		"revised_at": article.RevisedAt,
	})
	client := app.Elastic.Client
	index := app.Conf.ArticleIndex.Name
	typ := app.Conf.ArticleIndexTypes.Draft
	articleId := article.Id
	updService := client.Update()
	updService.Index(index)
	updService.Type(typ)
	updService.Id(articleId)
	updService.Script(script)
	updService.DetectNoop(true)
	if waitForRefresh {
		updService.Refresh("wait_for")
	}
	ctx := context.Background()
	resp, err := updService.Do(ctx)
	//fmt.Printf("resp: %T, %+v\n", resp, resp)
	//fmt.Printf("error: %v\n", err)
	if err != nil {
		if elastic.IsNotFound(err) {
			body := fmt.Sprintf("article draft %v not found!", article.Id)
			logger.Perror(body)
			return nil, CreateNotFoundRespData(body)
		} else {
			body := fmt.Sprintf("failed to update article draft %v, error: %v", article.Id, err)
			logger.Perror(body)
			return nil, CreateInternalServerErrorRespData(body)
		}
	} else {
		if resp.Result == "noop" {
			body := fmt.Sprintf("Save article draft (%v) locked by another user is not allowed!", article.Id)
			logger.Perror(body)
			return nil, CreateForbiddenRespData(body)
		} else if resp.Result == "updated" {
			logger.Pinfof("user %v saved article draft %v", username, article.Id)
			article, d := getFullArticle(client, ctx, index, typ, articleId, logger)
			fmt.Printf("got full article:\n %+v\n", article)
			if d != nil {
				return nil, d
			} else {
				return article, nil
			}
		} else {
			body := fmt.Sprintf(`unknown "result" in update response: %v`, resp.Result)
			logger.Perror(body)
			return nil, CreateInternalServerErrorRespData(body)
		}
	}
}

func saveArticle(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	logger := CtxLoggerFromReq(r)
	bytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		body := fmt.Sprintf("failed to read request body, error: %v", err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	}
	article, err := unmarshalArticle(bytes)
	if err != nil {
		body := fmt.Sprintf("failed to unmarshal article, error: %v", err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	}
	article.Id = StringFromReq(r, CtxKeyId)
	article.Guid = article.Id
	user := CmsUserFromReq(r)
	// lock on the article draft
	lock := draftLock.Get(article.Id)
	lock.Lock()
	defer lock.Unlock()
	// save article
	article, d := saveArticleDraft(app, user, article, logger, true)
	if d != nil {
		return d
	} else {
		logger.Pinfof("user %v saved article draft %v", user.Username, article.Id)
		return CreateRespData(http.StatusOK, ContentTypeValueText, []byte{})
	}
}

func submitArticleSelf(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	logger := CtxLoggerFromReq(r)
	// first save the article to draft
	bytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		body := fmt.Sprintf("failed to read request body, error: %v", err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	}
	article, err := unmarshalArticle(bytes)
	if err != nil {
		body := fmt.Sprintf("failed to unmarshal article, error: %v", err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	}
	article.Id = StringFromReq(r, CtxKeyId)
	article.Guid = article.Id
	user := CmsUserFromReq(r)
	// lock on the article draft
	lock := draftLock.Get(article.Id)
	lock.Lock()
	defer lock.Unlock()
	// save article draft
	article, d := saveArticleDraft(app, user, article, logger, false)
	if d != nil {
		return d
	}
	// set article props for the new version
	jt := article.RevisedAt
	ver := jt.T.UnixNano()
	verGuid := fmt.Sprintf("%v:%v", article.Guid, ver)
	article.Id = verGuid
	article.Version = strconv.FormatInt(ver, 10)
	if article.FromVersion == "0" { // it's the first version
		article.CreatedAt = jt
		article.CreatedBy = user.Username
	}
	article.RevisedAt = jt
	article.RevisedBy = user.Username
	article.LockedBy = ""
	// create the new version
	ctx := context.Background()
	idxService := app.Elastic.Client.Index()
	idxService.Index(app.Conf.ArticleIndex.Name)
	idxService.Type(app.Conf.ArticleIndexTypes.Version)
	idxService.OpType(ESIndexOpCreate)
	idxService.Id(article.Id)
	idxService.BodyJson(article)
	idxResp, err := idxService.Do(ctx)
	if err != nil {
		body := fmt.Sprintf("failed to create new article version %v, error: %v", verGuid, err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	} else if !idxResp.Created {
		body := fmt.Sprintf("no reason but article new version %v is not created!", verGuid)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	}
	// delete article from draft
	// no need to check user here as we have successfully saved it
	delService := app.Elastic.Client.Delete()
	delService.Index(app.Conf.ArticleIndex.Name)
	delService.Type(app.Conf.ArticleIndexTypes.Draft)
	delService.Id(article.Guid)
	delService.Refresh("wait_for")
	_, err = delService.Do(ctx)
	if err != nil {
		body := fmt.Sprintf("failed to delete article draft %v, error: %v", article.Guid, err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	}
	// return article version
	article.Headline = ""
	article.Summary = ""
	article.Content = ""
	article.Tag = nil
	article.Note = ""
	if bytes, err := json.Marshal(article); err == nil {
		logger.Pinfof("user %v submited article version %v", user.Username, verGuid)
		return CreateRespData(http.StatusOK, ContentTypeValueJSON, bytes)
	} else {
		body := fmt.Sprintf("error marshaling article %v: %v", verGuid, err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	}
}

func discardArticleSelf(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	logger := CtxLoggerFromReq(r)
	articleId := StringFromReq(r, CtxKeyId)
	username := CmsUserFromReq(r).Username

	script := elastic.NewScript(ESScriptDiscardArticle)
	script.Type("inline").Lang("painless").Params(map[string]interface{}{
		"username": username,
	})
	updService := app.Elastic.Client.Update()
	updService.Index(app.Conf.ArticleIndex.Name)
	updService.Type(app.Conf.ArticleIndexTypes.Draft)
	updService.Id(articleId)
	updService.Script(script)
	updService.DetectNoop(true)
	updService.Refresh("wait_for")
	resp, err := updService.Do(context.Background())
	if err != nil {
		if elastic.IsNotFound(err) {
			body := fmt.Sprintf("article draft %v not found!", articleId)
			logger.Perror(body)
			return CreateNotFoundRespData(body)
		} else {
			body := fmt.Sprintf("failed to delete article draft %v, error: %v", articleId, err)
			logger.Perror(body)
			return CreateInternalServerErrorRespData(body)
		}
	} else {
		if resp.Result == "noop" {
			body := "delete article locked by another user is not allowed!"
			logger.Perror(body)
			return CreateForbiddenRespData(body)
		} else if resp.Result == "deleted" {
			logger.Pinfof("user %v deleted article draft %v", username, articleId)
			return CreateRespData(http.StatusOK, ContentTypeValueText, []byte{})
		} else {
			body := fmt.Sprintf(`unknown "result" in update response: %v`, resp.Result)
			logger.Perror(body)
			return CreateInternalServerErrorRespData(body)
		}
	}
}

func submitArticleOther(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	logger := CtxLoggerFromReq(r)
	user := CmsUserFromReq(r)
	// first get the draft article
	articleId := StringFromReq(r, CtxKeyId)
	client := app.Elastic.Client
	index := app.Conf.ArticleIndex.Name
	typ := app.Conf.ArticleIndexTypes.Draft
	ctx := context.Background()
	article, d := getFullArticle(client, ctx, index, typ, articleId, logger)
	if d != nil {
		return d
	}
	article.Guid = article.Id // in case it is a newly created article without any "save"
	// lock on the article draft
	lock := draftLock.Get(article.Id)
	lock.Lock()
	defer lock.Unlock()
	// set article props for the new version
	jt := article.RevisedAt
	ver := jt.T.UnixNano()
	verGuid := fmt.Sprintf("%v:%v", article.Guid, ver)
	article.Id = verGuid
	article.Version = strconv.FormatInt(ver, 10)
	if article.FromVersion == "0" { // it's the first version
		article.CreatedAt = jt
		article.CreatedBy = user.Username
	}
	article.RevisedAt = jt
	article.RevisedBy = user.Username
	article.LockedBy = ""
	// create the new version
	idxService := client.Index()
	idxService.Index(index)
	idxService.Type(app.Conf.ArticleIndexTypes.Version)
	idxService.OpType(ESIndexOpCreate)
	idxService.Id(article.Id)
	idxService.BodyJson(article)
	idxResp, err := idxService.Do(ctx)
	if err != nil {
		body := fmt.Sprintf("failed to create new article version %v, error: %v", verGuid, err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	} else if !idxResp.Created {
		body := fmt.Sprintf("no reason but article new version %v is not created!", verGuid)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	}
	// delete article from draft
	delService := app.Elastic.Client.Delete()
	delService.Index(app.Conf.ArticleIndex.Name)
	delService.Type(app.Conf.ArticleIndexTypes.Draft)
	delService.Id(article.Guid)
	delService.Refresh("wait_for")
	_, err = delService.Do(ctx)
	if err != nil {
		body := fmt.Sprintf("failed to delete article draft %v, error: %v", article.Guid, err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	}
	// return article version
	article.Headline = ""
	article.Summary = ""
	article.Content = ""
	article.Tag = nil
	article.Note = ""
	if bytes, err := json.Marshal(article); err == nil {
		logger.Pinfof("user %v submited article version %v", user.Username, verGuid)
		return CreateRespData(http.StatusOK, ContentTypeValueJSON, bytes)
	} else {
		body := fmt.Sprintf("error marshaling article %v: %v", verGuid, err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	}
}

func discardArticleOther(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	logger := CtxLoggerFromReq(r)
	articleId := StringFromReq(r, CtxKeyId)
	username := CmsUserFromReq(r).Username

	delService := app.Elastic.Client.Delete()
	delService.Index(app.Conf.ArticleIndex.Name)
	delService.Type(app.Conf.ArticleIndexTypes.Draft)
	delService.Id(articleId)
	delService.Refresh("wait_for")
	_, err := delService.Do(context.Background())
	if err != nil {
		body := fmt.Sprintf("failed to delete article draft %v, error: %v", articleId, err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	} else {
		logger.Pinfof("user %v deleted article draft %v", username, articleId)
		return CreateRespData(http.StatusOK, ContentTypeValueText, []byte{})
	}
}

func editArticle(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	logger := CtxLoggerFromReq(r)
	// first get the article from the version type
	articleId := StringFromReq(r, CtxKeyId)
	client := app.Elastic.Client
	index := app.Conf.ArticleIndex.Name
	typ := app.Conf.ArticleIndexTypes.Version
	ctx := context.Background()
	article, d := getFullArticle(client, ctx, index, typ, articleId, logger)
	if d != nil {
		return d
	}
	user := CmsUserFromReq(r)
	// editing an article version created by another user,
	// need to check if there is EditOther role
	//fmt.Printf("\n\n===> login username: %s, article revised_by: %s\n\n", user.Username, article.RevisedBy)
	if user.Username != article.RevisedBy {
		if CmsRoleArticleEditOther&user.Role == 0 {
			body := "You're not allowed to edit article version created by another user!"
			return CreateForbiddenRespData(body)
		}
	}
	// set article props
	jt := &JSONTime{time.Now().UTC()}
	username := user.Username
	article.Id = article.Guid
	article.FromVersion = article.Version
	article.Version = "0"
	article.RevisedAt = jt
	article.RevisedBy = username
	article.LockedBy = username
	// now try to index (create) it as type draft
	idxService := client.Index()
	idxService.Index(index)
	idxService.Type(app.Conf.ArticleIndexTypes.Draft)
	idxService.OpType(ESIndexOpCreate)
	idxService.Id(article.Id)
	idxService.BodyJson(article)
	idxService.Refresh("wait_for")

	lock := draftLock.Get(article.Id)
	lock.Lock()
	defer lock.Unlock()
	resp, err := idxService.Do(ctx)
	if err != nil {
		body := fmt.Sprintf("failed to create article draft %v, error: %v", article.Id, err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	} else if !resp.Created {
		body := fmt.Sprintf("no reason but article draft %v is not created!", article.Id)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	} else {
		if bytes, err := json.Marshal(article); err == nil {
			logger.Pinfof("user %v created article draft %v", username, article.Id)
			return CreateRespData(http.StatusOK, ContentTypeValueJSON, bytes)
		} else {
			body := fmt.Sprintf("failed to marshal article draft %v, error: %v", article.Id, err)
			logger.Perror(body)
			return CreateInternalServerErrorRespData(body)
		}
	}
}

func publishArticle(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	logger := CtxLoggerFromReq(r)
	// load article from version
	client := app.Elastic.Client
	ctx := context.Background()
	index := app.Conf.ArticleIndex.Name
	typ := app.Conf.ArticleIndexTypes.Version
	id := StringFromReq(r, CtxKeyId)
	article, d := getFullArticle(client, ctx, index, typ, id, logger)
	if d != nil {
		return d
	}
	// upsert it into publish
	guid := article.Guid
	article.Id = guid
	article.LockedBy = ""
	updService := client.Update()
	updService.Index(index)
	updService.Type(app.Conf.ArticleIndexTypes.Publish)
	updService.Id(guid)
	updService.Doc(article)
	updService.DocAsUpsert(true)
	updService.Refresh("wait_for")

	lock := publishLock.Get(guid)
	lock.Lock()
	defer lock.Unlock()
	_, err := updService.Do(ctx)
	articleVerGuid := fmt.Sprintf("%v:%v", guid, article.Version)
	if err != nil {
		body := fmt.Sprintf("failed to publish article version %v, error: %v", articleVerGuid, err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	} else {
		logger.Pinfof("user %v published article version %v", CmsUserFromReq(r).Username, articleVerGuid)
		return CreateRespData(http.StatusOK, ContentTypeValueText, []byte{})
	}
}

func unpublishArticle(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	logger := CtxLoggerFromReq(r)
	articleId := StringFromReq(r, CtxKeyId)
	delService := app.Elastic.Client.Delete()
	delService.Index(app.Conf.ArticleIndex.Name)
	delService.Type(app.Conf.ArticleIndexTypes.Publish)
	delService.Id(articleId)
	delService.Refresh("wait_for")

	lock := publishLock.Get(articleId)
	lock.Lock()
	defer lock.Unlock()
	_, err := delService.Do(context.Background())
	if err != nil {
		if elastic.IsNotFound(err) {
			body := fmt.Sprintf("article %v not found!", articleId)
			logger.Perror(body)
			return CreateNotFoundRespData(body)
		} else {
			body := fmt.Sprintf("failed to unpublish article %v, error: %v", articleId, err)
			logger.Perror(body)
			return CreateInternalServerErrorRespData(body)
		}
	} else {
		logger.Pinfof("user %v unpublished article %v", CmsUserFromReq(r).Username, articleId)
		return CreateRespData(http.StatusOK, ContentTypeValueText, []byte{})
	}
}

type ArticleVersions []*Article

func (v ArticleVersions) Len() int      { return len(v) }
func (v ArticleVersions) Swap(i, j int) { v[i], v[j] = v[j], v[i] }

// should convert version to int64 then compare but it is okay to compare strings directly
func (v ArticleVersions) Less(i, j int) bool { return v[i].Version < v[j].Version }

type CmsArticle struct {
	Guid      string          `json:"guid"`
	CreatedAt *JSONTime       `json:"created_at"`
	Draft     *Article        `json:"draft,omitempty"`
	Versions  ArticleVersions `json:"versions,omitempty"`
	Publish   *Article        `json:"publish,omitempty"`
}

type CmsArticles []*CmsArticle

func (v CmsArticles) Len() int           { return len(v) }
func (v CmsArticles) Swap(i, j int)      { v[i], v[j] = v[j], v[i] }
func (v CmsArticles) Less(i, j int) bool { return v[i].CreatedAt.T.After(v[j].CreatedAt.T) }

func getSearchTypesFromQueryString(values url.Values, validTypes map[string]bool) []string {
	input, ok := values["type"]
	if !ok || len(input) <= 0 { // no "type", default to all valid types
		types := make([]string, 0, len(validTypes))
		for t, _ := range validTypes {
			types = append(types, t)
		}
		return types
	}
	types := make([]string, 0, len(validTypes))
	for _, t := range strings.Split(input[0], ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		if _, ok := validTypes[t]; ok {
			types = append(types, t)
		}
	}
	return types
}

func getBeforeTime(values url.Values) time.Time {
	if t, err := time.Parse("2006-01-02T15:04:05.000Z", values.Get("before")); err == nil {
		return t
	} else {
		return time.Now().UTC().Add(-time.Hour * 72)
	}
}

type CmsArticlesResponseBody struct {
	Articles   CmsArticles `json:"articles"`
	CursorMark string      `json:"cursor_mark,omitempty"`
	Before     *JSONTime   `json:"before"`
}

func getCmsArticles(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	logger := CtxLoggerFromReq(r)
	q := r.URL.Query()
	before := getBeforeTime(q)
	inputTypes := getSearchTypesFromQueryString(q, app.Conf.ArticleIndexTypeMap)
	if len(inputTypes) <= 0 {
		return CreateJsonRespData(http.StatusOK, &CmsArticlesResponseBody{
			Articles: make([]*CmsArticle, 0),
			Before:   &JSONTime{T: before},
		})
	}
	searchAfter, d := DecodeCursorMark(q)
	if d != nil {
		return d
	}
	search := app.Elastic.Client.Search(app.Conf.ArticleIndex.Name)
	search.Type(inputTypes...)
	query := elastic.NewBoolQuery().Filter(
		elastic.NewExistsQuery("guid"),
		elastic.NewRangeQuery("created_at").Gte(before),
	)
	search.Query(query)
	search.Size(10000)
	search.FetchSource(true)
	search.SortBy(
		elastic.NewFieldSort("created_at").Desc().UnmappedType("date"),
		elastic.NewFieldSort("_uid"),
	)
	if searchAfter != nil {
		search.SearchAfter(searchAfter...)
	}
	resp, err := search.Do(context.Background())
	if err != nil {
		body := fmt.Sprintf("failed to query elasticsearch, error: %v", err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	}
	if resp.Hits.TotalHits <= 0 {
		return CreateJsonRespData(http.StatusOK, &CmsArticlesResponseBody{
			Articles: make([]*CmsArticle, 0),
			Before:   &JSONTime{T: before},
		})
	}
	types := app.Conf.ArticleIndexTypes
	articleMap := make(map[string]*CmsArticle)
	var lastSort []interface{}
	for _, hit := range resp.Hits.Hits {
		one, err := unmarshalArticle(*hit.Source)
		if err != nil {
			logger.Pwarnf("failed to unmarshal article (%v): %v, error: %v", hit.Type, string(*hit.Source), err)
			continue
		}
		one.Id = hit.Id
		var a *CmsArticle
		var ok bool
		if a, ok = articleMap[one.Guid]; !ok {
			a = &CmsArticle{
				Guid:      one.Guid,
				CreatedAt: one.CreatedAt,
				Versions:  make([]*Article, 0),
			}
			articleMap[a.Guid] = a
		}
		if hit.Type == types.Draft {
			a.Draft = one
		} else if hit.Type == types.Version {
			a.Versions = append(a.Versions, one)
		} else if hit.Type == types.Publish {
			a.Publish = one
		} else { // should not happen
			logger.Pwarnf("unknown type %v in article index.", hit.Type)
		}
		lastSort = hit.Sort
	}
	var articles CmsArticles = make([]*CmsArticle, 0, len(articleMap))
	for _, a := range articleMap {
		if len(a.Versions) > 0 {
			sort.Stable(sort.Reverse(a.Versions))
		}
		articles = append(articles, a)
	}
	if len(articles) > 0 {
		cursorMark, err := EncodeCursorMark(lastSort)
		if err != nil {
			return &HttpResponseData{
				Status: http.StatusInternalServerError,
				Header: map[string][]string{
					"Content-Type": []string{"text/plain"},
				},
				Body: strings.NewReader(fmt.Sprintf("failed to encode sort %v, error: %v!", lastSort, err)),
			}
		}
		sort.Stable(articles)
		return CreateJsonRespData(http.StatusOK, &CmsArticlesResponseBody{
			Articles:   articles,
			CursorMark: cursorMark,
			Before:     &JSONTime{T: before},
		})
	} else {
		return CreateJsonRespData(http.StatusOK, &CmsArticlesResponseBody{
			Articles: make([]*CmsArticle, 0),
			Before:   &JSONTime{T: before},
		})
	}
}

func getCmsArticle(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	logger := CtxLoggerFromReq(r)
	articleId := StringFromReq(r, CtxKeyId)

	types := app.Conf.ArticleIndexTypes
	search := app.Elastic.Client.Search(app.Conf.ArticleIndex.Name)
	search.Type(types.Draft, types.Version, types.Publish)
	search.Query(elastic.NewConstantScoreQuery(elastic.NewTermQuery("guid", articleId)))
	search.FetchSource(true)
	resp, err := search.Do(context.Background())
	if err != nil {
		body := fmt.Sprintf("failed to query elasticsearch, error: %v", err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	}
	if resp.Hits.TotalHits <= 0 {
		body := fmt.Sprintf("Article %v is not found!", articleId)
		return CreateNotFoundRespData(body)
	}
	article := &CmsArticle{
		Versions: make([]*Article, 0),
	}
	for _, hit := range resp.Hits.Hits {
		var a *Article
		var err error
		if hit.Type == types.Draft {
			if a, err = unmarshalArticle(*hit.Source); err == nil {
				article.Draft = a
				article.Guid = a.Guid
				article.CreatedAt = a.CreatedAt
			} else {
				logger.Pwarnf("failed to unmarshal article draft: %v, error: %v", string(*hit.Source), err)
			}
		} else if hit.Type == types.Version {
			if a, err = unmarshalArticle(*hit.Source); err == nil {
				article.Versions = append(article.Versions, a)
				article.Guid = a.Guid
				article.CreatedAt = a.CreatedAt
			} else {
				logger.Pwarnf("failed to unmarshal article version: %v, error: %v", string(*hit.Source), err)
			}
		} else if hit.Type == types.Publish {
			if a, err = unmarshalArticle(*hit.Source); err == nil {
				article.Publish = a
				article.Guid = a.Guid
				article.CreatedAt = a.CreatedAt
			} else {
				logger.Pwarnf("failed to unmarshal article publish: %v, error: %v", string(*hit.Source), err)
			}
		} else {
			logger.Pwarnf("unknown type %v in article index.", hit.Type)
		}
	}
	if len(article.Versions) > 0 {
		sort.Stable(article.Versions)
	}
	return CreateJsonRespData(http.StatusOK, article)
}

func ArticleCreate() EndpointHandler {
	h := addArticleAuditLogFields("create", createArticle)
	h = RequireOneRole(CmsRoleArticleCreate, h)
	return RequireAuth(h)
}

func ArticleSave() EndpointHandler {
	h := addArticleAuditLogFields("save", saveArticle)
	h = GetRequiredStringArg("id", CtxKeyId, h)
	h = RequireOneRole(CmsRoleArticleCreate|CmsRoleArticleEditSelf|CmsRoleArticleEditOther, h)
	return RequireAuth(h)
}

func ArticleSubmitSelf() EndpointHandler {
	h := addArticleAuditLogFields("submit", submitArticleSelf)
	h = GetRequiredStringArg("id", CtxKeyId, h)
	h = RequireOneRole(CmsRoleArticleCreate|CmsRoleArticleEditSelf|CmsRoleArticleEditOther, h)
	return RequireAuth(h)
}

func ArticleDiscardSelf() EndpointHandler {
	h := addArticleAuditLogFields("discard", discardArticleSelf)
	h = GetRequiredStringArg("id", CtxKeyId, h)
	h = RequireOneRole(CmsRoleArticleCreate|CmsRoleArticleEditSelf|CmsRoleArticleEditOther, h)
	return RequireAuth(h)
}

func ArticleSubmitOther() EndpointHandler {
	h := addArticleAuditLogFields("submit", submitArticleOther)
	h = GetRequiredStringArg("id", CtxKeyId, h)
	h = RequireOneRole(CmsRoleArticleSubmit, h)
	return RequireAuth(h)
}

func ArticleDiscardOther() EndpointHandler {
	h := addArticleAuditLogFields("discard", discardArticleOther)
	h = GetRequiredStringArg("id", CtxKeyId, h)
	h = RequireOneRole(CmsRoleArticleSubmit, h)
	return RequireAuth(h)
}

func ArticleEdit() EndpointHandler {
	h := addArticleAuditLogFields("edit", editArticle)
	h = GetRequiredStringArg("id", CtxKeyId, h)
	h = RequireOneRole(CmsRoleArticleEditSelf|CmsRoleArticleEditOther, h)
	return RequireAuth(h)
}

func ArticlePublish() EndpointHandler {
	h := addArticleAuditLogFields("publish", publishArticle)
	h = GetRequiredStringArg("id", CtxKeyId, h)
	h = RequireOneRole(CmsRoleArticlePublish, h)
	return RequireAuth(h)
}

func ArticleUnpublish() EndpointHandler {
	h := addArticleAuditLogFields("unpublish", unpublishArticle)
	h = GetRequiredStringArg("id", CtxKeyId, h)
	h = RequireOneRole(CmsRoleArticlePublish, h)
	return RequireAuth(h)
}

func ArticleGet() EndpointHandler {
	h := GetRequiredStringArg("id", CtxKeyId, getCmsArticle)
	return RequireAuth(h)
}

func ArticlesGet() EndpointHandler {
	return RequireAuth(getCmsArticles)
}
