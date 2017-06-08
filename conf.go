package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
)

var (
	articleIndexDef         string
	articleIndexTypePublish string
	articleIndexTypeVersion string
	articleIndexTypeDraft   string
	articleIndexTypeLock    string

	articleIndexTypes = &ArticleIndexTypes{
		Publish: "publish",
		Version: "version",
		Draft:   "draft",
		Lock:    "lock",
	}
)

type ArticleIndexTypes struct {
	Publish string
	Version string
	Draft   string
	Lock    string
}

func init() {
	textType := map[string]interface{}{"type": "text"}
	dateType := map[string]interface{}{"type": "date"}
	kwdType := map[string]interface{}{"type": "keyword"}
	articleMappingProps := map[string]map[string]interface{}{
		"headline":   textType,
		"summary":    textType,
		"content":    textType,
		"tag":        kwdType,
		"created_at": dateType,
		"created_by": kwdType,
		"edited_at":  dateType,
		"edited_by":  kwdType,
	}

	bytes, err := json.Marshal(map[string]interface{}{
		"settings": map[string]interface{}{
			"number_of_shards":     1,
			"number_of_replicas":   1,
			"index.mapper.dynamic": false,
		},
		"mappings": map[string]interface{}{
			articleIndexTypes.Publish: articleMappingProps,
			articleIndexTypes.Version: articleMappingProps,
			articleIndexTypes.Draft:   articleMappingProps,
			articleIndexTypes.Lock:    map[string]interface{}{},
		},
	})
	if err != nil {
		panic(err)
	}
	articleIndexDef = string(bytes)
}

// Application Configurations
type AppConf struct {

	// Host to bind the http server
	ServerIP string

	// Port to the http server listens on
	ServerPort int

	// Elasticsearch Hosts
	ESHosts []string

	// article index
	ArticleIndex *ESIndex

	// article index types
	ArticleIndexTypes *ArticleIndexTypes
}

func (c *AppConf) String() string {

	var x = struct {
		ServerIP         string   `json:"server-ip"`
		ServerPort       int      `json:"server-port"`
		ESHosts          []string `json:"es-hosts"`
		ArticleIndexName string   `json:"article-index"`
	}{
		ServerIP:         c.ServerIP,
		ServerPort:       c.ServerPort,
		ESHosts:          c.ESHosts,
		ArticleIndexName: c.ArticleIndex.Name,
	}
	bytes, err := json.Marshal(x)
	if err != nil {
		panic(err)
	}
	return string(bytes)
}

func checkIPAndPort(ip string, port int) error {
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("invalid IP address: %v!", ip)
	}
	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid port %v!", port)
	}
	return nil
}

func parseESHosts(s string) ([]string, error) {
	hosts := strings.Split(s, ",")
	for i := 0; i < len(hosts); i++ {
		host := hosts[i]
		parts := strings.Split(host, ":")
		ip := parts[0]
		port, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid port in elasticsearch host %v!", host)
		}
		err = checkIPAndPort(ip, port)
		if err != nil {
			return nil, fmt.Errorf("invalid elasticsearch host %v, reason: %v!", host, err)
		}
	}
	return hosts, nil
}

func ParseArgs(args []string) *AppConf {

	// parse command line args
	var cli = flag.NewFlagSet("story-api", flag.ExitOnError)
	var help = cli.Bool("help", false, "Print usage and exit.")
	var serverIP = cli.String("server-ip", "0.0.0.0", "IP address this API server binds to.")
	var serverPort = cli.Int("server-port", 8080, "Port this API server listens on.")
	var esHostStr = cli.String("es-hosts", "127.0.0.1:9200", "Elasticsearch server hosts (comma separated).")

	cli.Parse(args[1:])

	if *help {
		fmt.Fprintf(os.Stderr, "Usage of %v:\n", path.Base(args[0]))
		cli.PrintDefaults()
		fmt.Fprintln(os.Stderr)
		os.Exit(0)
	}

	// validate given args
	if err := checkIPAndPort(*serverIP, *serverPort); err != nil {
		panic(err)
	}
	esHosts, err := parseESHosts(*esHostStr)
	if err != nil {
		panic(err)
	}

	return &AppConf{
		ServerIP:   *serverIP,
		ServerPort: *serverPort,
		ESHosts:    esHosts,

		ArticleIndex:      &ESIndex{"article", articleIndexDef},
		ArticleIndexTypes: articleIndexTypes,
	}
}
