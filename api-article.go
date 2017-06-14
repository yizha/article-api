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
	draftLock   = &UniqStrMutex{}
	publishLock = &UniqStrMutex{}
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
	Guid        string    `json:"guid,omitempty"`
	Version     int64     `json:"version,omitempty"`
	Headline    string    `json:"headline,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	Content     string    `json:"content,omitempty"`
	Tag         []string  `json:"tag,omitempty"`
	Note        string    `json:"note,omitempty"`
	CreatedAt   *JSONTime `json:"created_at,omitempty"`
	CreatedBy   string    `json:"created_by,omitempty"`
	RevisedAt   *JSONTime `json:"revised_at,omitempty"`
	RevisedBy   string    `json:"revised_by,omitempty"`
	FromVersion int64     `json:"from_version,omitempty"`
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
	getService.Realtime(false)
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

func addAuditLogFields(action string, h EndpointHandler) EndpointHandler {
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
	article := &Article{
		LockedBy:    username,
		FromVersion: 0,
	}
	// don't set Id or OpType in order to have id auto-generated by elasticsearch
	idxService := app.Elastic.Client.Index()
	idxService.Index(app.Conf.ArticleIndex.Name)
	idxService.Type(app.Conf.ArticleIndexTypes.Draft)
	idxService.BodyJson(article)
	resp, err := idxService.Do(context.Background())
	if err != nil {
		body := fmt.Sprintf("error creating new article doc: %v", err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	} else {
		article.Id = resp.Id
		article.Guid = resp.Id
		article.CreatedBy = username
		article.LockedBy = username
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

func saveArticleDraft(app *AppRuntime, user *CmsUser, article *Article, logger *JsonLogger) (*Article, *HttpResponseData) {
	username := user.Username
	script := elastic.NewScript(ESScriptSaveArticle)
	article.RevisedAt = &JSONTime{time.Now().UTC()}
	script.Type("inline").Lang("painless").Params(map[string]interface{}{
		"headline":   article.Headline,
		"summary":    article.Summary,
		"content":    article.Content,
		"tag":        article.Tag,
		"note":       article.Note,
		"username":   username,
		"revised_at": article.RevisedAt,
	})
	updService := app.Elastic.Client.Update()
	updService.Index(app.Conf.ArticleIndex.Name)
	updService.Type(app.Conf.ArticleIndexTypes.Draft)
	updService.Id(article.Id)
	updService.Script(script)
	updService.DetectNoop(true)
	resp, err := updService.Do(context.Background())
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
			return article, nil
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
	user := CmsUserFromReq(r)
	// lock on the article draft
	lock := draftLock.Get(article.Id)
	lock.Lock()
	defer lock.Unlock()
	// save article
	article, d := saveArticleDraft(app, user, article, logger)
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
	user := CmsUserFromReq(r)
	// lock on the article draft
	lock := draftLock.Get(article.Id)
	lock.Lock()
	defer lock.Unlock()
	// save article draft
	article, d := saveArticleDraft(app, user, article, logger)
	if d != nil {
		return d
	}
	// set article props for the new version
	jt := article.RevisedAt
	ver := jt.T.UnixNano()
	verGuid := fmt.Sprintf("%v:%v", article.Guid, ver)
	article.Id = verGuid
	article.Version = ver
	if article.FromVersion == 0 { // it's a create
		article.CreatedAt = jt
		article.CreatedBy = user.Username
		article.RevisedAt = nil
		article.RevisedBy = ""
	} else { // it's an edit
		article.RevisedAt = jt
		article.RevisedBy = user.Username
	}
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

	script := elastic.NewScript(ESScriptSaveArticle)
	script.Type("inline").Lang("painless").Params(map[string]interface{}{
		"username": username,
	})
	updService := app.Elastic.Client.Update()
	updService.Index(app.Conf.ArticleIndex.Name)
	updService.Type(app.Conf.ArticleIndexTypes.Draft)
	updService.Id(articleId)
	updService.Script(script)
	updService.DetectNoop(true)
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
		} else if resp.Result == "updated" {
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
	// lock on the article draft
	lock := draftLock.Get(article.Id)
	lock.Lock()
	defer lock.Unlock()
	// set article props for the new version
	jt := article.RevisedAt
	ver := jt.T.UnixNano()
	verGuid := fmt.Sprintf("%v:%v", article.Guid, ver)
	article.Id = verGuid
	article.Version = ver
	if article.FromVersion == 0 { // it's a create
		article.CreatedAt = jt
		article.CreatedBy = user.Username
		article.RevisedAt = nil
		article.RevisedBy = ""
	} else { // it's an edit
		article.RevisedAt = jt
		article.RevisedBy = user.Username
	}
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
	// set article props
	jt := &JSONTime{time.Now().UTC()}
	username := CmsUserFromReq(r).Username
	article.Id = article.Guid
	article.FromVersion = article.Version
	article.Version = 0
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

func ArticleCreate() EndpointHandler {
	h := addAuditLogFields("create", createArticle)
	h = RequireOneRole(CmsRoleArticleCreate, h)
	return RequireAuth(h)
}

func ArticleSave() EndpointHandler {
	h := addAuditLogFields("save", saveArticle)
	h = GetRequiredStringArg("id", CtxKeyId, h)
	h = RequireOneRole(CmsRoleArticleCreate|CmsRoleArticleEdit, h)
	return RequireAuth(h)
}

func ArticleSubmitSelf() EndpointHandler {
	h := addAuditLogFields("submit", submitArticleSelf)
	h = GetRequiredStringArg("id", CtxKeyId, h)
	h = RequireOneRole(CmsRoleArticleCreate|CmsRoleArticleEdit, h)
	return RequireAuth(h)
}

func ArticleDiscardSelf() EndpointHandler {
	h := addAuditLogFields("discard", discardArticleSelf)
	h = GetRequiredStringArg("id", CtxKeyId, h)
	h = RequireOneRole(CmsRoleArticleCreate|CmsRoleArticleEdit, h)
	return RequireAuth(h)
}

func ArticleSubmitOther() EndpointHandler {
	h := addAuditLogFields("submit", submitArticleOther)
	h = GetRequiredStringArg("id", CtxKeyId, h)
	h = RequireOneRole(CmsRoleArticleSubmit, h)
	return RequireAuth(h)
}

func ArticleDiscardOther() EndpointHandler {
	h := addAuditLogFields("discard", discardArticleOther)
	h = GetRequiredStringArg("id", CtxKeyId, h)
	h = RequireOneRole(CmsRoleArticleSubmit, h)
	return RequireAuth(h)
}

func ArticleEdit() EndpointHandler {
	h := addAuditLogFields("edit", editArticle)
	h = GetRequiredStringArg("id", CtxKeyId, h)
	h = RequireOneRole(CmsRoleArticleEdit, h)
	return RequireAuth(h)
}

func ArticlePublish() EndpointHandler {
	h := addAuditLogFields("publish", publishArticle)
	h = GetRequiredStringArg("id", CtxKeyId, h)
	h = RequireOneRole(CmsRoleArticlePublish, h)
	return RequireAuth(h)
}

func ArticleUnpublish() EndpointHandler {
	h := addAuditLogFields("unpublish", unpublishArticle)
	h = GetRequiredStringArg("id", CtxKeyId, h)
	h = RequireOneRole(CmsRoleArticlePublish, h)
	return RequireAuth(h)
}
