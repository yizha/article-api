package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path"

	elastic "gopkg.in/olivere/elastic.v5"
)

const (
	ArticleIndexDef = `
{
  "settings" : {
    "number_of_shards" :   1,
    "number_of_replicas" : 1
  },
  "mappings":{
    "publish":{
      "properties":{
        "headline":     {"type": "text"},
        "summary":      {"type": "text"},
        "content":      {"type": "text"},
        "tag":          {"type": "keyword"},
        "created_at":   {"type": "date"},
        "created_by":   {"type": "keyword"},
        "edited_at":    {"type": "date"},
        "edited_by":    {"type": "keyword"}
      }
    },
    "version":{
      "properties":{
        "headline":     {"type": "text"},
        "summary":      {"type": "text"},
        "content":      {"type": "text"},
        "tag":          {"type": "keyword"},
        "created_at":   {"type": "date"},
        "created_by":   {"type": "keyword"},
        "edited_at":    {"type": "date"},
        "edited_by":    {"type": "keyword"}
      }
    },
    "draft":{
      "properties":{
        "headline":     {"type": "text"},
        "summary":      {"type": "text"},
        "content":      {"type": "text"},
        "tag":          {"type": "keyword"},
        "created_at":   {"type": "date"},
        "created_by":   {"type": "keyword"},
        "edited_at":    {"type": "date"},
        "edited_by":    {"type": "keyword"}
      }
    }
  }
}`
	ArticleLockIndexDef = `
{
  "settings" : {
    "number_of_shards" :   1,
    "number_of_replicas" : 1
  },
  "mappings":{
    "lock":{
      "properties":{
      }
    }
  }
}`
)

type ESIndex struct {
	Name string
	Def  string
}

type Elastic struct {
	client *elastic.Client
	ctx    context.Context
	logger *log.Logger
}

func (es *Elastic) CreateIndex(index *ESIndex) error {
	if exists, err := es.client.IndexExists(index.Name).Do(es.ctx); err != nil {
		return err
	} else if exists {
		es.logger.Printf("index %v exists.", index.Name)
		return nil
	}
	if _, err := es.client.CreateIndex(index.Name).BodyString(index.Def).Do(es.ctx); err != nil {
		return err
	}
	es.logger.Printf("created index %v.", index.Name)
	return nil
}

func (es *Elastic) DeleteIndex(index *ESIndex) error {
	if exists, err := es.client.IndexExists(index.Name).Do(es.ctx); err != nil {
		return err
	} else if !exists {
		es.logger.Printf("index %v doesn't exist.", index.Name)
		return nil
	}
	if _, err := es.client.DeleteIndex(index.Name).Do(es.ctx); err != nil {
		return err
	}
	es.logger.Printf("deleted index %v.", index.Name)
	return nil
}

func newElastic(ip string, port int, logger *log.Logger) *Elastic {
	host := fmt.Sprintf("http://%v:%v", ip, port)
	client, err := elastic.NewClient(
		elastic.SetMaxRetries(3),
		elastic.SetURL(host))
	if err != nil {
		panic(err)
	}

	if ver, err := client.ElasticsearchVersion(host); err == nil {
		logger.Printf("Elasticsearch at %v, version: %v.", host, ver)
	} else {
		panic(err)
	}

	return &Elastic{
		client: client,
		ctx:    context.Background(),
		logger: logger,
	}
}

// Application Configurations
type AppConf struct {

	// Host to bind the http server
	ServerIP string

	// Port to the http server listens on
	ServerPort int

	// Elasticsearch Host
	ESIP string

	// Elasticsearch Port
	ESPort int

	// story index
	ArticleIndex *ESIndex

	// story lock index
	ArticleLockIndex *ESIndex

	// Elasticsearch client
	Elastic *Elastic

	// loggers
	AccessLogger, AppLogger *log.Logger

	// delete index flag
	DeleteIndics bool
}

func (c *AppConf) String() string {
	var buf = &bytes.Buffer{}

	fmt.Fprintf(buf, "\n")
	fmt.Fprintf(buf, "======== Application Configurations ========\n")
	fmt.Fprintf(buf, "Server IP:          %v\n", c.ServerIP)
	fmt.Fprintf(buf, "Server Port:        %v\n", c.ServerPort)
	fmt.Fprintf(buf, "ES Server IP:       %v\n", c.ESIP)
	fmt.Fprintf(buf, "ES Server Port:     %v\n", c.ESPort)
	fmt.Fprintf(buf, "Article Index:      %v\n", c.ArticleIndex.Name)
	fmt.Fprintf(buf, "Article Lock Index: %v\n", c.ArticleLockIndex.Name)
	fmt.Fprintf(buf, "============================================\n")
	fmt.Fprintf(buf, "\n")

	return buf.String()
}

func checkIPAndPort(ip *string, port *int) error {
	if net.ParseIP(*ip) == nil {
		return fmt.Errorf("invalid IP address: %v!", *ip)
	}
	if *port <= 0 || *port > 65535 {
		return fmt.Errorf("invalid port %v!", *port)
	}
	return nil
}

func ParseArgs(args []string) *AppConf {

	// parse command line args
	var cli = flag.NewFlagSet("story-api", flag.ExitOnError)
	var help = cli.Bool("help", false, "Print usage and exit.")
	var serverIP = cli.String("server-ip", "0.0.0.0", "IP address this API server binds to.")
	var serverPort = cli.Int("server-port", 80, "Port this API server listens on.")
	var esIP = cli.String("es-ip", "127.0.0.1", "Elasticsearch server IP address.")
	var esPort = cli.Int("es-port", 9200, "Elasticsearch server port.")
	var deleteIndices = cli.Bool("delete-indices", false, "Delete Elasticsearch indices on server startup.")

	cli.Parse(args[1:])

	if *help {
		fmt.Fprintf(os.Stderr, "Usage of %v:\n", path.Base(args[0]))
		cli.PrintDefaults()
		fmt.Fprintln(os.Stderr)
		os.Exit(0)
	}

	// validate given args
	if err := checkIPAndPort(serverIP, serverPort); err != nil {
		panic(err)
	}
	if err := checkIPAndPort(esIP, esPort); err != nil {
		panic(err)
	}

	accessLogger := log.New(os.Stdout, "", 0)
	appLogger := log.New(os.Stderr, "", log.Ldate|log.Ltime|log.Lmicroseconds|log.LUTC|log.Lshortfile)

	return &AppConf{
		ServerIP:   *serverIP,
		ServerPort: *serverPort,
		ESIP:       *esIP,
		ESPort:     *esPort,

		Elastic: newElastic(*esIP, *esPort, appLogger),

		ArticleIndex:     &ESIndex{"article", ArticleIndexDef},
		ArticleLockIndex: &ESIndex{"article_lock", ArticleLockIndexDef},

		AccessLogger: accessLogger,
		AppLogger:    appLogger,

		DeleteIndics: *deleteIndices,
	}
}
