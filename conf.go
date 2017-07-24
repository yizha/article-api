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
	"time"

	"github.com/gorilla/securecookie"
)

type ArticleIndexTypes struct {
	Publish string
	Version string
	Draft   string
}

type UserIndexTypes struct {
	User string
}

var (
	articleIndexDef string

	articleIndexTypes = &ArticleIndexTypes{
		Publish: "publish",
		Version: "version",
		Draft:   "draft",
	}

	userIndexDef = `
{
  "settings" : {
    "number_of_shards" :   1,
    "number_of_replicas" : 1,
	"index.mapper.dynamic": false
  },
  "mappings":{
    "user":{
      "properties":{
        "username":        {"type": "keyword"},
        "password":        {"type": "binary", "doc_values": false},
        "role":            {"type": "keyword"}
      }
    }
  }
}`

	userIndexTypes = &UserIndexTypes{
		User: "user",
	}
)

func init() {
	articleMappingProps := map[string]map[string]map[string]interface{}{
		"properties": map[string]map[string]interface{}{
			"guid":         map[string]interface{}{"type": "keyword"},
			"headline":     map[string]interface{}{"type": "text"},
			"summary":      map[string]interface{}{"type": "text", "index": "false"},
			"content":      map[string]interface{}{"type": "text"},
			"tag":          map[string]interface{}{"type": "keyword"},
			"created_at":   map[string]interface{}{"type": "date"},
			"created_by":   map[string]interface{}{"type": "keyword"},
			"revised_at":   map[string]interface{}{"type": "date"},
			"revised_by":   map[string]interface{}{"type": "keyword"},
			"version":      map[string]interface{}{"type": "keyword"},
			"from_version": map[string]interface{}{"type": "keyword"},
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

	// Host to bind the http server
	ServerIP string

	// Port to the http server listens on
	ServerPort int

	// server read timeout
	ServerReadTimeout time.Duration

	// server write timeout
	ServerWriteTimeout time.Duration

	// Elasticsearch Hosts
	ESHosts []string

	// Used to sign/encrypt/decrypt auth data with gorilla/securecookie
	// This is hacky as the securecookie is meant for cookie but here
	// we set the result string as an auth-token in header
	SCookie *securecookie.SecureCookie

	// max age for SCookie
	SCookieMaxAge time.Duration

	// logging spec
	// Only support logging to stdout or file
	// When it is file we use https://github.com/natefinch/lumberjack
	// So for stdout logging it is "stdout" for file logging it is
	// something like "file:[path],[max-size],[max-backups],[max-age]"
	LoggingSpec *LoggingSpec

	// article index
	ArticleIndex *ESIndex

	// article index types
	ArticleIndexTypes   *ArticleIndexTypes
	ArticleIndexTypeMap map[string]bool

	// user index
	UserIndex *ESIndex

	// user type
	UserIndexTypes *UserIndexTypes
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
	var serverIP = cli.String("server-ip", "0.0.0.0", "IP address this API server binds to.")
	var serverPort = cli.Int("server-port", 8080, "Port this API server listens on.")
	var serverWriteTimeout = cli.Int("server-write-timeout", 15, "http server write timeout in seconds.")
	var serverReadTimeout = cli.Int("server-read-timeout", 15, "http server read timeout in seconds.")
	var esHostStr = cli.String("es-hosts", "127.0.0.1:9200", "Elasticsearch server hosts (comma separated).")
	var hashKey = cli.String("hash-key", "", "Secret hash key used to sign data")
	var blockKey = cli.String("block-key", "", "Secret block key used to encrypt/decrypt data")
	var authExp = cli.Int("auth-expiration", 3600, "auth token expiration in seconds, set to 0 to not expire.")
	var loggingSpec = &LoggingSpec{Target: LoggingTargetStdout}
	cli.Var(loggingSpec, "logging", `set logging to stdout ("stdout") or a file ("file:[path],[max-size],[max-backups],[max-age]")`)

	cli.Parse(args[1:])

	if *help {
		fmt.Fprintf(os.Stderr, "Usage of %v:\n", filepath.Base(args[0]))
		cli.PrintDefaults()
		fmt.Fprintln(os.Stderr)
		os.Exit(0)
	}

	hashKeyBytes := []byte(*hashKey)
	if len(hashKeyBytes) == 0 {
		hashKeyBytes = []byte("我是用于数据签名的哈希串，开头的六十四个字节起作用！")[0:64]
	}
	if len(hashKeyBytes) != 64 {
		panic(fmt.Sprintf("invalid hash key, byte length (%v) is not 64!", len(hashKeyBytes)))
	}
	blockKeyBytes := []byte(*blockKey)
	if len(blockKeyBytes) == 0 {
		blockKeyBytes = []byte("！串希哈的密解密加据数于用是我")[0:32]
	}
	if len(blockKeyBytes) != 32 {
		panic(fmt.Sprintf("invalid block key, byte length (%v) is not 32!", len(blockKeyBytes)))
	}
	if *authExp < 0 || *authExp > 86400 {
		panic(fmt.Sprintf("auth expiration %v (seconds) in not in allowed range [0, 86400]", *authExp))
	}
	scookie := securecookie.New(hashKeyBytes, blockKeyBytes)
	scookie.SetSerializer(securecookie.JSONEncoder{})
	scookie.MinAge(0)    // no restriction
	scookie.MaxLength(0) // no restriction
	scookie.MaxAge(*authExp)

	// validate given args
	if err := checkIPAndPort(*serverIP, *serverPort); err != nil {
		panic(err.Error())
	}
	esHosts, err := parseESHosts(*esHostStr)
	if err != nil {
		panic(err.Error())
	}
	if *serverReadTimeout < 5 || *serverReadTimeout > 300 {
		panic(fmt.Sprintf("server read timeout (%v seconds) is not in allowed range [5, 300].", *serverReadTimeout))
	}
	if *serverWriteTimeout < 5 || *serverWriteTimeout > 300 {
		panic(fmt.Sprintf("server read timeout (%v seconds) is not in allowed range [5, 300].", *serverWriteTimeout))
	}

	articleIndexTypeMap := map[string]bool{
		articleIndexTypes.Draft:   true,
		articleIndexTypes.Version: true,
		articleIndexTypes.Publish: true,
	}

	return &AppConf{
		ServerIP:           *serverIP,
		ServerPort:         *serverPort,
		ServerReadTimeout:  time.Duration(*serverReadTimeout) * time.Second,
		ServerWriteTimeout: time.Duration(*serverWriteTimeout) * time.Second,
		ESHosts:            esHosts,
		LoggingSpec:        loggingSpec,

		SCookie:       scookie,
		SCookieMaxAge: time.Duration(*authExp) * time.Second,

		ArticleIndex:        &ESIndex{"article", articleIndexDef},
		ArticleIndexTypes:   articleIndexTypes,
		ArticleIndexTypeMap: articleIndexTypeMap,

		UserIndex:      &ESIndex{"user", userIndexDef},
		UserIndexTypes: userIndexTypes,
	}
}
