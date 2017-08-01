package main

import (
	"bytes"
	"context"
	//	"encoding/json"
	"fmt"
	"html/template"
	//	"io/ioutil"
	"net/http"
	//	"net/url"
	//	"strconv"
	//	"strings"
	//	"time"
	//elastic "gopkg.in/olivere/elastic.v5"
	elastic "github.com/yizha/elastic"
)

const (
	ARTICLE_TPL = `<html>
<head></head>
<body>
<h1>Headline: {{.Headline}}</h1>
<div><p>Created at/by: {{.CreatedAt}}/{{.CreatedBy}}</p></div>
<div><p>Revised at/by: {{.RevisedAt}}/{{.RevisedBy}}</p></div>
<br/>
<div><p>Summary: {{.Summary}}</p></div>
<br/>
<div><p>Content: {{.Content}}</p></div>
</body>
</html>
`

	ARTICLE_LIST_TPL = `<html>
<head></head>
<body>
<div>
<ul>
{{ range $idx,$cluster := .Articles }}
<li><a href="/article?id={{.Guid}}:{{.Version}}" target="_blank">{{.Headline}}</a></li>
{{ end }}
</ul>
</div>
{{ if gt .MoreSize 0 }}
<br/>
<div>
<a href="/articles?size={{.MoreSize}}">More</a>
</div>
{{ end }}
</body>
</html>
`
)

var (
	articleTpl     = template.Must(template.New("article").Parse(ARTICLE_TPL))
	articleListTpl = template.Must(template.New("article").Parse(ARTICLE_LIST_TPL))
)

func getFEArticle(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	logger := CtxLoggerFromReq(r)
	articleId := StringFromReq(r, CtxKeyId)

	article, d := getFullArticle(
		app.Elastic.Client,
		context.Background(),
		app.Conf.ArticleIndex.Name,
		app.Conf.ArticleIndexTypes.Version,
		articleId,
		logger)
	if d != nil {
		return d
	}
	buf := &bytes.Buffer{}
	if err := articleTpl.Execute(buf, article); err != nil {
		body := fmt.Sprintf("failed to generate article page, error: %v", err)
		CreateInternalServerErrorRespData(body)
	}
	return CreateRespData(http.StatusOK, "text/html; charset=UTF-8", buf.Bytes())
}

type FEArticlesResponse struct {
	Articles []*Article
	MoreSize int
}

func getFEArticles(app *AppRuntime, w http.ResponseWriter, r *http.Request) *HttpResponseData {
	logger := CtxLoggerFromReq(r)
	size, d := ParseQueryIntValue(r.URL.Query(), "size", false, 10, 1, 100)
	if d != nil {
		return d
	}

	search := app.Elastic.Client.Search(app.Conf.ArticleIndex.Name)
	search.Type(app.Conf.ArticleIndexTypes.Publish)
	search.Query(elastic.NewMatchAllQuery())
	search.Size(size)
	search.FetchSource(true)
	search.Sort("revised_at", false)
	resp, err := search.Do(context.Background())
	if err != nil {
		body := fmt.Sprintf("failed to query elasticsearch, error: %v", err)
		logger.Perror(body)
		return CreateInternalServerErrorRespData(body)
	}
	articles := make([]*Article, 0)
	for _, hit := range resp.Hits.Hits {
		one, err := unmarshalArticle(*hit.Source)
		if err != nil {
			logger.Pwarnf("failed to unmarshal article (%v): %v, error: %v", hit.Type, string(*hit.Source), err)
			continue
		}
		one.Id = hit.Id
		articles = append(articles, one)
	}
	moreSize := size
	if len(resp.Hits.Hits) == size {
		moreSize += size
	} else {
		moreSize = 0
	}

	articlesResp := &FEArticlesResponse{
		Articles: articles,
		MoreSize: moreSize,
	}

	buf := &bytes.Buffer{}
	if err := articleListTpl.Execute(buf, articlesResp); err != nil {
		body := fmt.Sprintf("failed to generate article page, error: %v", err)
		return CreateInternalServerErrorRespData(body)
	}
	return CreateRespData(http.StatusOK, "text/html; charset=UTF-8", buf.Bytes())

}

func FEArticlePage() EndpointHandler {
	return GetRequiredStringArg("id", CtxKeyId, getFEArticle)
}

func FEArticlesPage() EndpointHandler {
	return getFEArticles
}
