package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const TokenCookieName = "token"

type CmsRole struct {
	Name  string `json:"name,omitempty"`
	Value uint32 `json:"value,omitempty"`
}

type CmsRoleValue uint32

const (

	// create article
	// implies save/submit/discard draft article created by self
	CmsRoleArticleCreate CmsRoleValue = 1 << 0

	// edit article
	// implies save/submit/discard draft article created by self
	CmsRoleArticleEdit CmsRoleValue = 1 << 1

	// submit/discard draft article created by others
	CmsRoleArticleSubmit CmsRoleValue = 1 << 2

	// publish/unpublish article
	CmsRoleArticlePublish CmsRoleValue = 1 << 3

	// create/update/delete login
	CmsRoleLoginManage CmsRoleValue = 1 << 20

	CmsRoleArticleCreateName  = "article:create"
	CmsRoleArticleEditName    = "article:edit"
	CmsRoleArticleSubmitName  = "article:submit"
	CmsRoleArticlePublishName = "article:publish"
	CmsRoleLoginManageName    = "login:manage"
)

var (
	CmsRoleValue2Name map[CmsRoleValue]string
	CmsRoleName2Value map[string]CmsRoleValue
	CmsRoles          []*CmsRole
)

func init() {
	CmsRoleValue2Name = map[CmsRoleValue]string{
		CmsRoleArticleCreate:  CmsRoleArticleCreateName,
		CmsRoleArticleEdit:    CmsRoleArticleEditName,
		CmsRoleArticleSubmit:  CmsRoleArticleSubmitName,
		CmsRoleArticlePublish: CmsRoleArticlePublishName,
		CmsRoleLoginManage:    CmsRoleLoginManageName,
	}
	CmsRoles = make([]*CmsRole, len(CmsRoleValue2Name))
	CmsRoleName2Value = make(map[string]CmsRoleValue)
	i := 0
	for id, name := range CmsRoleValue2Name {
		CmsRoleName2Value[name] = id
		CmsRoles[i] = &CmsRole{
			Name:  name,
			Value: uint32(id),
		}
		i += 1
	}

}

func (r *CmsRoleValue) MarshalJSON() ([]byte, error) {
	roles := make([]string, 0)
	if *r > 0 {
		for id, name := range CmsRoleValue2Name {
			if *r&id == id {
				roles = append(roles, name)
			}
		}
	}
	return json.Marshal(roles)
}

func (r *CmsRoleValue) UnmarshalJSON(data []byte) error {
	roles := make([]string, 0)
	if err := json.Unmarshal(data, &roles); err != nil {
		return err
	}
	*r = 0
	if roles != nil && len(roles) > 0 {
		for _, name := range roles {
			if id, ok := CmsRoleName2Value[name]; ok {
				*r = *r | id
			} else {
				fmt.Fprintf(os.Stderr, "ignore unknown cms user role name %v!", name)
			}
		}
	}
	return nil
}

type CmsUser struct {
	Username string       `json:"username,omitempty"`
	Password string       `json:"password,omitempty"`
	Role     CmsRoleValue `json:"role"`
}

func (u *CmsUser) String() string {
	return fmt.Sprintf("%v@%v", u.Username, Role2Names(u.Role))
}

func CmsUserFromReq(req *http.Request) *CmsUser {
	v := req.Context().Value(CtxKeyCmsUser)
	if v == nil {
		return nil
	} else if user, ok := v.(*CmsUser); ok {
		return user
	} else {
		fmt.Fprintf(os.Stdout, "value (%T, %v) under key %v is not of type CmsUser!", v, v, CtxKeyCmsUser)
		return nil
	}
}

func HashPassword(pass string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(bytes), nil
}

func Names2Role(s string) CmsRoleValue {
	var role CmsRoleValue = 0
	if len(s) > 0 {
		for _, name := range strings.Split(s, ",") {
			if id, ok := CmsRoleName2Value[name]; ok {
				role = role | id
			}
		}
	}
	return role
}

func Role2Names(role CmsRoleValue) []string {
	names := make([]string, 0)
	if role > 0 {
		for id, name := range CmsRoleValue2Name {
			if role&id == id {
				names = append(names, name)
			}
		}
	}
	return names
}
