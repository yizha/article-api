package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/yizha/elastic"
)

type TestCase interface {
	Desc() string
	Run() error
}

func RunTestCase(c TestCase) error {
	fmt.Printf("[%s] ... ", c.Desc())
	if err := c.Run(); err == nil {
		fmt.Println("OK")
		return nil
	} else {
		fmt.Printf("NG (%s)\n", err.Error())
		return err
	}
}

type TestCaseGroup interface {
	Desc() string
	Setup() error
	TearDown() error
	StopOnNG() bool
	GetTestCases() ([]TestCase, error)
}

func RunTestCaseGroup(grp TestCaseGroup) {
	grpDesc := grp.Desc()
	// set up
	if err := grp.Setup(); err != nil {
		fmt.Printf("Group [%s] setup failed: %v\n", grpDesc, err)
		return
	}
	// run cases
	cases, err := grp.GetTestCases()
	if err != nil {
		fmt.Printf("Group [%s] get cases failed: %v\n", grpDesc, err)
		return
	}
	if grp.StopOnNG() {

		for _, c := range cases {
			if err := RunTestCase(c); err != nil {
				break
			}
		}
	} else {
		for _, c := range cases {
			RunTestCase(c)
		}
	}
	// tear down
	if err := grp.TearDown(); err != nil {
		fmt.Printf("Group [%s] tear down failed: %v\n", grpDesc, err)
		return
	}
}

func GetAuthToken(client *http.Client, host, username, password string) (string, error) {
	resp, err := client.Get(fmt.Sprintf("http://%s/login?username=%s&password=%s", host, username, password))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	m := make(map[string]string)
	err = json.Unmarshal(data, &m)
	if err != nil {
		return "", err
	}
	return m["token"], nil
}

func main() {

	host := flag.String("host", "localhost:8080", "Target host.")
	esHost := flag.String("es-host", "localhost:9200", "storage elasticsearch host.")

	flag.Parse()

	hclient := &http.Client{Timeout: 5 * time.Second}
	esclient, err := elastic.NewClient(
		elastic.SetURL(fmt.Sprintf("http://%v", *esHost)),
	)
	if err != nil {
		panic(err)
	}

	for _, grp := range GetLoginTests(*host, hclient, esclient) {
		RunTestCaseGroup(grp)
	}
}
