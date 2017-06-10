package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestMarshalJSONTime(t *testing.T) {
	ts := time.Now().UTC()
	jt := &JSONTime{ts}
	bytes, err := json.Marshal(jt)
	if err != nil {
		t.Errorf("marshal JSONTime error: %v", err)
		return
	}
	actual := string(bytes)
	expect := fmt.Sprintf(`"%s"`, ts.Format("2006-01-02T15:04:05.000Z"))
	if expect != actual {
		t.Errorf("expecting %v, got %v", expect, actual)
		return
	}
}

func TestUnmarshalJSONTime(t *testing.T) {
	// positive case: "null"
	jt := &JSONTime{time.Now().UTC()}
	s := `"null"`
	err := json.Unmarshal([]byte(s), jt)
	if err != nil {
		t.Errorf("unmarshal %v to JSONTime error: %v", s, err)
		return
	}
	if !jt.T.IsZero() {
		t.Errorf(`"null" should be unmarshaled to ZERO time.Time, but got %v`, jt.T.Format("2006-01-02T15:04:05.000Z"))
	}

	// positive case: correct date/time string
	jt = &JSONTime{}
	s = `"2012-05-03T13:48:00.000Z"`
	err = json.Unmarshal([]byte(s), jt)
	if err != nil {
		t.Errorf("unmarshal %v to JSONTime error: %v", s, err)
		return
	}
	expect := s
	actual := fmt.Sprintf(`"%s"`, jt.T.Format("2006-01-02T15:04:05.000Z"))
	if expect != actual {
		t.Errorf("expecting %v, got %v", expect, actual)
		return
	}

	// negative case: wrong date/time string
	jt = &JSONTime{}
	ss := []string{
		`"2012-05-03T13:48:00.000z"`,
		`"aaaaaaaaaaaaaaaaaaaaaaaa"`,
		`2012-05-03T13:48:00.000z`,
	}
	for _, s := range ss {
		err = json.Unmarshal([]byte(s), jt)
		if err == nil {
			t.Errorf("expecting error, but got %v", jt.T.Format("2006-01-02T15:04:05.000Z"))
			return
		}
	}
}

func TestUnmarshalArticle(t *testing.T) {
	revisedAt := "2012-05-03T13:48:00.000Z"
	s := fmt.Sprintf(`{"created_at":"null","revised_at":"%v"}`, revisedAt)
	a, err := unmarshalArticle([]byte(s))
	if err != nil {
		t.Errorf("unmarshalArticle(...) failed with error %v", err)
		return
	}
	if a.CreatedAt != nil {
		t.Errorf("expecting .CreatedAt=nil, but got %v", a.CreatedAt)
		return
	}
	if a.RevisedAt == nil {
		t.Error("expecting non-nil .RevisedAt, but got nil!")
		return
	}
	actual := a.RevisedAt.T.Format("2006-01-02T15:04:05.000Z")
	if actual != revisedAt {
		t.Errorf("expecting .RevisedAt=%v, but got %v", revisedAt, actual)
		return
	}
}
