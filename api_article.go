package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

/*
	"headline":     map[string]interface{}{"type": "text"},
	"summary":      map[string]interface{}{"type": "text", "index": "false"},
	"content":      map[string]interface{}{"type": "text"},
	"tag":          map[string]interface{}{"type": "keyword"},
	"created_at":   map[string]interface{}{"type": "date"},
	"created_by":   map[string]interface{}{"type": "keyword"},
	"revised_at":   map[string]interface{}{"type": "date"},
	"revised_by":   map[string]interface{}{"type": "keyword"},
	"version":      map[string]interface{}{"type": "long"},
	"from_version": map[string]interface{}{"type": "long"},
	"note":         map[string]interface{}{"type": "text", "index": "false"},
	"locked_by":    map[string]interface{}{"type": "keyword"},
*/
type JSONTime struct {
	T time.Time
}

func (t *JSONTime) MarshalJSON() ([]byte, error) {
	return []byte(t.T.Format("2006-01-02T15:04:05.000Z")), nil
}

func (t *JSONTime) UnmarshalJSON(data []byte) error {
	var err error
	t.T, err = time.Parse("2006-01-02T15:04:05.000Z", string(data))
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

func ArticleCreate(w http.ResponseWriter, r *http.Request, app *AppRuntime) *HttpResponseData {
	user, d := ParseQueryStringValue(r.URL.Query(), "user", true, "")
	if d != nil {
		return d
	}
	article := &Article{
		LockedBy: user,
	}
	idxService := app.Elastic.Client.Index()
	idxService.Index(app.Conf.ArticleIndex.Name)
	idxService.Type(app.Conf.ArticleIndexTypes.Draft)
	//idxService.OpType(ESIndexOpCreate)
	idxService.BodyJson(article)
	resp, err := idxService.Do(app.Elastic.Context)
	if err != nil {
		body := fmt.Sprintf("error creating new doc: %v", err)
		return CreateInternalServerErrorRespData(body)
	} else {
		article.Id = resp.Id
		article.CreatedBy = user
		if bytes, err := json.Marshal(article); err == nil {
			return CreateRespData(http.StatusOK, ContentTypeValueJSON, string(bytes))
		} else {
			body := fmt.Sprintf("failed to marshal Article object, error: %v", err)
			return CreateInternalServerErrorRespData(body)
		}
	}
}
