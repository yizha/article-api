package main

import (
	"fmt"
	"os"
	//"time"
)

type AppRuntime struct {
	logger  *JsonLogger
	conf    *AppConf
	elastic *Elastic
}

func main() {

	logger := NewJsonLogger(os.Stdout).SetIncludeFile(true).SetFields(LogFields{
		"_log_type": "app",
	})
	logger.Printf("starting with args %v", os.Args)

	conf := ParseArgs(os.Args)
	logger.Printf("loaded app-conf: %v", conf.String())

	elastic, err := NewElastic(conf.ESHosts)
	if err != nil {
		panic(err)
	}
	if ok, msg := elastic.CreateIndex(conf.ArticleIndex); ok {
		logger.Print(msg)
	} else {
		panic(fmt.Sprintf("failed to create index %v, error: %v", conf.ArticleIndex.Name, msg))
	}

	app := &AppRuntime{
		logger:  logger,
		conf:    conf,
		elastic: elastic,
	}

	err = StartAPIServer(app)
	if err != nil {
		panic(err)
	}
}
