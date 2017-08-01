package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	//"time"

	elastic "github.com/yizha/elastic"
)

type AppRuntime struct {
	Logger        *JsonLogger
	Conf          *AppConf
	Elastic       *Elastic
	StaticMapping map[string]string
}

func createFirstUser(app *AppRuntime) {
	username := "void"
	password := "DoUpdateMePlease"
	hashedPassword, err := HashPassword(password)
	if err != nil {
		panic(err)
	}
	user := &CmsUser{
		Username: username,
		Password: hashedPassword,
		Role:     CmsRoleLoginManage,
	}
	ctx := context.Background()
	getService := app.Elastic.Client.Get()
	getService.Index(app.Conf.UserIndex.Name)
	getService.Index(app.Conf.UserIndexTypes.User)
	getService.Id(username)
	_, err = getService.Do(ctx)
	if err != nil {
		if elastic.IsNotFound(err) {
			idxService := app.Elastic.Client.Index()
			idxService.Index(app.Conf.UserIndex.Name)
			idxService.Type(app.Conf.UserIndexTypes.User)
			idxService.Id(username)
			idxService.BodyJson(user)
			_, err := idxService.Do(ctx)
			if err != nil {
				panic(err)
			}
			app.Logger.Pinfof("created user: %v, password: %v, role: %v", username, password, CmsRoleLoginManageName)
		}
	}
}

func createIndices(app *AppRuntime) {
	conf := app.Conf
	logger := app.Logger
	if ok, msg := app.Elastic.CreateIndex(conf.ArticleIndex); ok {
		logger.Pinfo(msg)
	} else {
		panic(fmt.Sprintf("failed to create index %v, error: %v", conf.ArticleIndex.Name, msg))
	}
	if ok, msg := app.Elastic.CreateIndex(conf.UserIndex); ok {
		logger.Pinfo(msg)
	} else {
		panic(fmt.Sprintf("failed to create index %v, error: %v", conf.UserIndex.Name, msg))
	}
}

func loadStaticMapping(app *AppRuntime) {
	staticMappingFilePath := path.Join(app.Conf.ServerRoot, "static-mapping.json")
	f, err := os.Open(staticMappingFilePath)
	if err != nil {
		app.Logger.Pinfof("skip loading static mapping file %v  due to error: %v", staticMappingFilePath, err)
		return
	}
	defer f.Close()
	bytes, err := ioutil.ReadAll(f)
	if err != nil {
		panic(fmt.Sprintf("failed to read static mapping file %v, error: %v", staticMappingFilePath, err))
	}
	if err := json.Unmarshal(bytes, &(app.StaticMapping)); err != nil {
		panic(fmt.Sprintf("failed to decode bytes from %v into json, error: %v", staticMappingFilePath, err))
	}
	app.Logger.Pinfof("loaded static mapping data: %v", app.StaticMapping)
}

func bootstrap(app *AppRuntime) {
	createIndices(app)
	createFirstUser(app)
	loadStaticMapping(app)
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

	app := &AppRuntime{
		Logger:        logger,
		Conf:          conf,
		Elastic:       elastic,
		StaticMapping: make(map[string]string),
	}

	bootstrap(app)

	StartAPIServer(app)
}
