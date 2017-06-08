package main

import (
	"context"
	"fmt"

	elastic "gopkg.in/olivere/elastic.v5"
)

type ESIndex struct {
	Name       string
	Definition string
}

type Elastic struct {
	Client  *elastic.Client
	Context context.Context
	Version string
}

func (es *Elastic) CreateIndex(index *ESIndex) (bool, string) {
	if exists, err := es.Client.IndexExists(index.Name).Do(es.Context); err != nil {
		return false, err.Error()
	} else if exists {
		return true, fmt.Sprintf("index %v already exists.", index.Name)
	}
	if _, err := es.Client.CreateIndex(index.Name).BodyString(index.Definition).Do(es.Context); err == nil {
		return true, fmt.Sprintf("created index %v.", index.Name)
	} else {
		return false, err.Error()
	}
}

func (es *Elastic) DeleteIndex(index *ESIndex) (bool, string) {
	if exists, err := es.Client.IndexExists(index.Name).Do(es.Context); err != nil {
		return false, err.Error()
	} else if !exists {
		return true, fmt.Sprintf("index %v doesn't exist.", index.Name)
	}
	if _, err := es.Client.DeleteIndex(index.Name).Do(es.Context); err == nil {
		return true, fmt.Sprintf("deleted index %v.", index.Name)
	} else {
		return false, err.Error()
	}
}

func NewElastic(hosts []string) (*Elastic, error) {
	urls := make([]string, len(hosts))
	for i := 0; i < len(hosts); i++ {
		urls[i] = fmt.Sprintf("http://%v", hosts[i])
	}
	client, err := elastic.NewClient(
		elastic.SetMaxRetries(3),
		elastic.SetURL(urls...))
	if err != nil {
		return nil, err
	}

	if ver, err := client.ElasticsearchVersion(urls[0]); err == nil {
		return &Elastic{
			Client:  client,
			Context: context.Background(),
			Version: ver,
		}, nil
	} else {
		return nil, err
	}
}
