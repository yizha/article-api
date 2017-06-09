package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ArticleIndexTypes struct {
	Publish string
	Version string
	Draft   string
}

var (
	articleIndexDef string

	articleIndexTypes = &ArticleIndexTypes{
		Publish: "publish",
		Version: "version",
		Draft:   "draft",
	}
)

func init() {
	articleMappingProps := map[string]map[string]map[string]interface{}{
		"properties": map[string]map[string]interface{}{
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
		},
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
		},
	})
	if err != nil {
		panic(err)
	}
	articleIndexDef = string(bytes)
	//fmt.Println(articleIndexDef)
}

type LoggingTarget string

const (
	LoggingTargetStdout LoggingTarget = "stdout"
	LoggingTargetFile   LoggingTarget = "file"
)

type LoggingSpec struct {
	Target     LoggingTarget
	Filepath   string
	MaxSize    int // megabytes
	MaxBackups int
	MaxAge     int // days
}

func (ls *LoggingSpec) String() string {
	if ls.Target == LoggingTargetStdout {
		return string(LoggingTargetStdout)
	} else {
		return fmt.Sprintf("%v:%v,%v,%v,%v", ls.Target, ls.Filepath, ls.MaxSize, ls.MaxBackups, ls.MaxAge)
	}
}

func (ls *LoggingSpec) Set(s string) error {
	parts := strings.SplitN(s, ":", 2)
	if parts[0] == string(LoggingTargetStdout) {
		ls.Target = LoggingTargetStdout
		return nil
	} else if parts[0] == string(LoggingTargetFile) {
		if len(parts) != 2 {
			return fmt.Errorf(`missing spec for "%v" logging!`, LoggingTargetFile)
		}
		specs := strings.Split(parts[1], ",")
		if len(specs) != 4 {
			return fmt.Errorf(`invalid spec "%v"`, parts[1])
		}
		var a2i = func(name, s string, min, max int) (int, error) {
			n, err := strconv.Atoi(s)
			if err != nil {
				return 0, fmt.Errorf("%v=%v is not an integer.", name, s)
			}
			if n < min || n > max {
				return 0, fmt.Errorf("%v=%v is not in allowed range [%v, %v]", name, n, min, max)
			}
			return n, nil
		}
		path := filepath.Clean(specs[0])
		maxSize, err := a2i("max-size", specs[1], 0, 1000)
		if err != nil {
			return err
		}
		maxBackups, err := a2i("max-backups", specs[2], 0, 100)
		if err != nil {
			return err
		}
		maxAge, err := a2i("max-age", specs[3], 0, 30)
		if err != nil {
			return err
		}

		ls.Target = LoggingTargetFile
		ls.Filepath = path
		ls.MaxSize = maxSize
		ls.MaxBackups = maxBackups
		ls.MaxAge = maxAge
		return nil
	} else {
		return fmt.Errorf(`unsupported logging target "%v" in "%v"`, parts[0], s)
	}
}

// Application Configurations
type AppConf struct {

	// App ID, appears in every log entry so that if logs are sent to
	// elasticsearch it will be easy to tell our logs from other
	// applications'
	AppId string

	// Host to bind the http server
	ServerIP string

	// Port to the http server listens on
	ServerPort int

	// Elasticsearch Hosts
	ESHosts []string

	// logging spec
	// Only support logging to stdout or file
	// When it is file we use https://github.com/natefinch/lumberjack
	// So for stdout logging it is "stdout" for file logging it is
	// something like "file:[path],[max-size],[max-backups],[max-age]"
	LoggingSpec *LoggingSpec

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
		LoggingSpec      string   `json:"logging-spec"`
	}{
		ServerIP:         c.ServerIP,
		ServerPort:       c.ServerPort,
		ESHosts:          c.ESHosts,
		ArticleIndexName: c.ArticleIndex.Name,
		LoggingSpec:      c.LoggingSpec.String(),
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
	var appId = cli.String("app-id", "article-api", "ID for this application.")
	var serverIP = cli.String("server-ip", "0.0.0.0", "IP address this API server binds to.")
	var serverPort = cli.Int("server-port", 8080, "Port this API server listens on.")
	var esHostStr = cli.String("es-hosts", "127.0.0.1:9200", "Elasticsearch server hosts (comma separated).")
	var loggingSpec = &LoggingSpec{Target: LoggingTargetStdout}
	cli.Var(loggingSpec, "logging", `set logging to stdout ("stdout") or a file ("file:[path],[max-size],[max-backups],[max-age]")`)

	cli.Parse(args[1:])

	if *help {
		fmt.Fprintf(os.Stderr, "Usage of %v:\n", filepath.Base(args[0]))
		cli.PrintDefaults()
		fmt.Fprintln(os.Stderr)
		os.Exit(0)
	}

	// validate given args
	if *appId == "" {
		panic("App ID is blank!")
	}
	if err := checkIPAndPort(*serverIP, *serverPort); err != nil {
		panic(err.Error())
	}
	esHosts, err := parseESHosts(*esHostStr)
	if err != nil {
		panic(err.Error())
	}

	return &AppConf{
		AppId:       *appId,
		ServerIP:    *serverIP,
		ServerPort:  *serverPort,
		ESHosts:     esHosts,
		LoggingSpec: loggingSpec,

		ArticleIndex:      &ESIndex{"article", articleIndexDef},
		ArticleIndexTypes: articleIndexTypes,
	}
}
