package main

import (
	"fmt"
	"os"
	//"time"
)

type AppRuntime struct {
	Logger  *JsonLogger
	Conf    *AppConf
	Elastic *Elastic
}

func main() {

	conf := ParseArgs(os.Args)
	logger := NewJsonLogger(os.Stdout).SetFields(LogFields{
		"app_id":    conf.AppId,
		"log_group": "app",
	})
	logger.Pinfof("starting %v with conf: %v", conf.AppId, conf.String())

	elastic, err := NewElastic(conf.ESHosts)
	if err != nil {
		panic(err)
	}
	if ok, msg := elastic.CreateIndex(conf.ArticleIndex); ok {
		logger.Pinfo(msg)
	} else {
		panic(fmt.Sprintf("failed to create index %v, error: %v", conf.ArticleIndex.Name, msg))
	}

	app := &AppRuntime{
		Logger:  logger,
		Conf:    conf,
		Elastic: elastic,
	}

	err = StartAPIServer(app)
	if err != nil {
		panic(err)
	}
}
