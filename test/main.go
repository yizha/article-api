package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/yizha/elastic"
	"golang.org/x/crypto/bcrypt"
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
		return 0, 0, 0
	}
	// run cases
	cases, err := grp.GetTestCases()
	if err != nil {
		fmt.Printf("Group [%s] get cases failed: %v\n", grpDesc, err)
		return 0, 0, 0
	}
	caseCnt, runCnt, passCnt := len(cases), 0, 0
	stopOnNG := grp.StopOnNG()
	for _, c := range cases {
		err := RunTestCase(c)
		runCnt++
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
	return caseCnt, runCnt, passCnt
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

func DeleteDocs(client *elastic.Client, index string, field string, vals ...interface{}) error {
	// delete test users
	del := client.DeleteByQuery(index)
	del.Refresh("wait_for")
	del.Query(elastic.NewTermsQuery(field, vals...))
	_, err := del.Do(context.Background())
	return err
}

func CreateUser(client *elastic.Client, index, type_, username, password, roles string) error {
	data, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		//fmt.Println("failed to bcrypt password:", err)
		return err
	}
	password = base64.StdEncoding.EncodeToString(data)
	idx := client.Index()
	idx.Index(index)
	idx.Type(type_)
	idx.Refresh("wait_for")
	idx.Id(username)
	idx.OpType("create")
	idx.BodyJson(map[string]interface{}{
		"username": username,
		"password": password,
		"role":     []string{"login:manage"},
	})
	_, err = idx.Do(context.Background())
	return err
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

	caseGroupMap := map[string]func(string, *http.Client, *elastic.Client) TestCaseGroup{
		"login":   GetLoginTests,
		"article": GetArticleTests,
	}

	caseGroupNames := make([]string, 0, len(caseGroupMap))
	for k, _ := range caseGroupMap {
		caseGroupNames = append(caseGroupNames, k)
	}

	var selectedCaseGroupNames []string
	args := flag.Args()
	if args != nil && len(args) > 0 && len(args[0]) > 0 && args[0] != "all" {
		selectedCaseGroupNames = make([]string, 0)
		for _, one := range strings.Split(args[0], ",") {
			if _, ok := caseGroupMap[one]; ok {
				selectedCaseGroupNames = append(selectedCaseGroupNames, one)
			} else {
				fmt.Printf("Ignore unknown test group name: %v\n", one)
			}
		}
		if len(selectedCaseGroupNames) <= 0 {
			fmt.Println("All given test group names are unknown!")
			os.Exit(1)
		}
	} else {
		selectedCaseGroupNames = caseGroupNames
	}

	fmt.Printf("Going to run these test groups: %v\n", selectedCaseGroupNames)
	fmt.Println()

	allCase, allRun, allPass := 0, 0, 0
	for _, name := range selectedCaseGroupNames {
		grp := caseGroupMap[name](*host, hclient, esclient)
		desc := grp.Desc()
		fmt.Printf("Testing [%s] ...\n", desc)
		caseCnt, runCnt, passCnt := RunTestCaseGroup(grp)
		allCase += caseCnt
		allRun += runCnt
		allPass += passCnt
		fmt.Printf("[%s] result, case/ran/passed: %v/%v/%v\n", desc, caseCnt, runCnt, passCnt)
		fmt.Println()
	}
	fmt.Printf("Overall result, case/ran/passed: %v/%v/%v\n", allCase, allRun, allPass)

	if allCase == allPass {
		os.Exit(0)
	} else {
		os.Exit(1)
	}
}
