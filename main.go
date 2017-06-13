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

	// create logger
	logger, err := NewJsonLoggerFromSpec(conf.LoggingSpec)
	if err != nil {
		panic(fmt.Sprintf("failed to create log from spec %v, error: %v", conf.LoggingSpec.String(), err))
	}
	logger.SetFields(LogFields{
		"log_group": "app",
	})
	logger.Pinfof("starting with conf: %v", conf.String())

	// init elasticsearch client
	elastic, err := NewElastic(conf.ESHosts)
	if err != nil {
		panic(err)
	}

	// create the index if it doesn't exist
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
