package main

import (
	"encoding/json"
	//	"fmt"
	"testing"
)

func testCmsRole(t *testing.T, roleValue CmsRoleValue, roleNames []string) {

	user := &CmsUser{
		Username: "test",
		Password: "ignore",
		Role:     roleValue,
	}

	bytes, err := json.Marshal(user)
	if err != nil {
		t.Errorf("failed to marshal user, error: %v", err)
		return
	}

	// unmarshal bytes into a map to check if result json is correct
	m := make(map[string]interface{})
	err = json.Unmarshal(bytes, &m)
	if err != nil {
		t.Errorf("failed to marshal user, error: %v", err)
		return
	}
	//fmt.Println(m)
	actualRoleNames, ok := m["role"].([]interface{})
	if !ok {
		t.Errorf("expecting []interface{}, but got %T: %v", m["role"], m["role"])
		return
	}
	if len(actualRoleNames) != len(roleNames) {
		t.Errorf("expecting role names length=%v, bug got %v", len(roleNames), len(actualRoleNames))
		return
	}
	namesMap := make(map[string]bool)
	for _, name := range actualRoleNames {
		namesMap[name.(string)] = true
	}
	for i := 0; i < len(roleNames); i++ {
		if _, ok := namesMap[roleNames[i]]; !ok {
			t.Errorf("expecting role name %v, but got %v", roleNames[i], actualRoleNames[i])
			return
		}
	}

	// unmarshal bytes back to CmsUser to check if role is correct
	var aUser CmsUser
	err = json.Unmarshal(bytes, &aUser)
	if err != nil {
		t.Errorf("failed to unmarshal data back to CmsUser, error: %v", err)
		return
	}
	if aUser.Role != roleValue {
		t.Errorf("expecting role value %v, but got %v", roleValue, aUser.Role)
		return
	}
}

func TestCmsRole(t *testing.T) {

	roleValue := CmsRoleArticleCreate | CmsRoleArticlePublish | CmsRoleLoginManage
	roleNames := []string{CmsRoleArticleCreateName, CmsRoleArticlePublishName, CmsRoleLoginManageName}

	testCmsRole(t, roleValue, roleNames)
	testCmsRole(t, 0, []string{})
}
