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
	fmt.Printf("  [%s] ... ", c.Desc())
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

func RunTestCaseGroup(grp TestCaseGroup) (int, int, int) {
	grpDesc := grp.Desc()
	// set up
	if err := grp.Setup(); err != nil {
		fmt.Printf("Group [%s] setup failed: %v\n", grpDesc, err)
		return -1, 0, 0
	}
	// run cases
	cases, err := grp.GetTestCases()
	if err != nil {
		fmt.Printf("Group [%s] get cases failed: %v\n", grpDesc, err)
		return -1, 0, 0
	}
	caseCnt, tryCnt, passCnt := len(cases), 0, 0
	stopOnNG := grp.StopOnNG()
	for _, c := range cases {
		err := RunTestCase(c)
		tryCnt++
		if err != nil {
			if stopOnNG {
				break
			}
		} else {
			passCnt++
		}
	}
	// tear down
	if err := grp.TearDown(); err != nil {
		fmt.Printf("Group [%s] tear down failed: %v\n", grpDesc, err)
	}
	return caseCnt, tryCnt, passCnt
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
	esclient, err := elastic.NewSimpleClient(
		elastic.SetURL(fmt.Sprintf("http://%v", *esHost)),
	)
	if err != nil {
		panic(err)
	}

	for _, grp := range GetLoginTests(*host, hclient, esclient) {
		desc := grp.Desc()
		fmt.Printf("Testing [%s] ...\n", desc)
		caseCnt, tryCnt, passCnt := RunTestCaseGroup(grp)
		fmt.Printf("[%s] result, case/tried/passed: %v/%v/%v\n", desc, caseCnt, tryCnt, passCnt)
		fmt.Println()
	}
}
