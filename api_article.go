package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	elastic "gopkg.in/olivere/elastic.v5"
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
	Headline    string    `json:"headline,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	Content     string    `json:"content,omitempty"`
	Tag         []string  `json:"tag,omitempty"`
	CreatedAt   *JSONTime `json:"created_at,omitempty"`
	CreatedBy   string    `json:"created_by,omitempty"`
	RevisedAt   *JSONTime `json:"revised_at,omitempty"`
	RevisedBy   string    `json:"revised_by,omitempty"`
	Version     int64     `json:"version,omitempty"`
	FromVersion int64     `json:"from_version,omitempty"`
	Note        string    `json:"note,omitempty"`
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

func handleCreateArticle(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	username := StringFromReq(r, CtxKeyUser)
	article := &Article{
		LockedBy: username,
	}
	// don't set Id or OpType in order to have id auto-generated by elasticsearch
	idxService := app.Elastic.Client.Index()
	idxService.Index(app.Conf.ArticleIndex.Name)
	idxService.Type(app.Conf.ArticleIndexTypes.Draft)
	idxService.BodyJson(article)
	resp, err := idxService.Do(app.Elastic.Context)
	if err != nil {
		body := fmt.Sprintf("error creating new doc: %v", err)
		return CreateInternalServerErrorRespData(body)
	} else {
		article.Id = resp.Id
		article.CreatedBy = username
		if bytes, err := json.Marshal(article); err == nil {
			return CreateRespData(http.StatusOK, ContentTypeValueJSON, string(bytes))
		} else {
			body := fmt.Sprintf("failed to marshal Article object, error: %v", err)
			return CreateInternalServerErrorRespData(body)
		}
	}
}

func getFullArticle(
	client *elastic.Client,
	ctx context.Context,
	index, typ, articleId string) (*Article, *HttpResponseData) {
	source := elastic.NewFetchSourceContext(true).Include(
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
	return getArticle(client, ctx, index, typ, articleId, source)
}

func getArticle(
	client *elastic.Client,
	ctx context.Context,
	index, typ, articleId string,
	source *elastic.FetchSourceContext) (*Article, *HttpResponseData) {
	getService := client.Get()
	getService.Index(index)
	getService.Type(typ)
	getService.Realtime(true)
	getService.Id(articleId)
	getService.FetchSourceContext(source)
	resp, err := getService.Do(ctx)
	if err != nil {
		body := fmt.Sprintf("error querying elasticsearch, error: %v", err)
		return nil, CreateInternalServerErrorRespData(body)
	} else if !resp.Found {
		body := fmt.Sprintf("article version %v not found!", articleId)
		return nil, CreateNotFoundRespData(body)
	} else {
		article := &Article{}
		if err := json.Unmarshal(*resp.Source, article); err != nil {
			body := fmt.Sprintf("unmarshal article error: %v", err)
			return nil, CreateInternalServerErrorRespData(body)
		} else {
			return article, nil
		}
	}
}

func marshalArticle(a *Article, status int) *HttpResponseData {
	bytes, err := json.Marshal(a)
	if err != nil {
		body := fmt.Sprintf("error marshaling article: %v", err)
		return CreateInternalServerErrorRespData(body)
	} else {
		return CreateRespData(status, ContentTypeValueJSON, string(bytes))
	}
}

func handleEditArticle(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	// first get the article
	articleId := StringFromReq(r, CtxKeyId)
	client := app.Elastic.Client
	ctx := app.Elastic.Context
	index := app.Conf.ArticleIndex.Name
	article, d := getFullArticle(client, ctx, index, app.Conf.ArticleIndexTypes.Version, articleId)
	if d != nil {
		return d
	}
	// set article props
	user := StringFromReq(r, CtxKeyUser)
	article.FromVersion = article.Version
	article.RevisedAt = nil
	article.RevisedBy = user
	article.LockedBy = user
	// now try to index (create) it as type draft
	typ := app.Conf.ArticleIndexTypes.Draft
	idxService := client.Index()
	idxService.Index(index)
	idxService.Type(typ)
	idxService.OpType(ESIndexOpCreate)
	idxService.Id(articleId)
	idxService.BodyJson(article)
	resp, err := idxService.Do(ctx)
	if err != nil {
		body := fmt.Sprintf("error querying elasticsearch, error: %v", err)
		return CreateInternalServerErrorRespData(body)
	} else if !resp.Created {
		// same doc already there? try to load it
		source := elastic.NewFetchSourceContext(true).Include("from_version", "locked_by")
		article, d = getArticle(client, ctx, index, typ, articleId, source)
		if d != nil {
			return d
		} else {
			return marshalArticle(article, http.StatusConflict)
		}
	} else {
		return marshalArticle(article, http.StatusOK)
	}
}

func ArticleCreate(app *AppRuntime) EndpointHandler {
	return GetRequiredStringArg(app, "user", CtxKeyUser, handleCreateArticle)
}

func ArticleEdit(app *AppRuntime) EndpointHandler {
	h := GetRequiredStringArg(app, "user", CtxKeyUser, handleEditArticle)
	return GetRequiredStringArg(app, "id", CtxKeyId, h)
}
