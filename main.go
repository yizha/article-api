package main

import (
	"os"
	"time"
)

func setup(conf *AppConf) {
	// delete indices
	if conf.DeleteIndics {
		if err := conf.Elastic.DeleteIndex(conf.ArticleLockIndex); err != nil {
			panic(err)
		}
		if err := conf.Elastic.DeleteIndex(conf.ArticleIndex); err != nil {
			panic(err)
		}
		time.Sleep(5 * time.Second)
	}
	// create indices
	if err := conf.Elastic.CreateIndex(conf.ArticleIndex); err != nil {
		panic(err)
	}
	if err := conf.Elastic.CreateIndex(conf.ArticleLockIndex); err != nil {
		panic(err)
	}
}

func main() {

	conf := ParseArgs(os.Args)
	conf.AppLogger.Printf("loaded conf: %v", conf.String())

	setup(conf)
}
