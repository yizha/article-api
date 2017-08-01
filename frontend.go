package main

import (
	"fmt"
	"html/template"
	"io/ioutil"
	"os"
	"path"
)

func staticFile(mapping map[string]string, path string, ext string) string {
	if val, ok := mapping[path]; ok {
		return fmt.Sprintf("%s-%s.%s", path, val, ext)
	} else {
		return path
	}
}

func GetPageTemplate(app *AppRuntime, key string) (*template.Template, error) {
	if tpl, ok := pageTemplates[key]; ok {
		return tpl, nil
	}
	tplFilePath := path.Join(app.Conf.ServerRoot, "template", fmt.Sprintf("%s.tpl", key))
	f, err := os.Open(tplFilePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	bytes, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}
	funcMap := template.FuncMap{
		"staticFile": func(path, ext string) string { return staticFile(app.StaticMapping, path, ext) },
	}
	tpl, err := template.New(key).Funcs(funcMap).Parse(string(bytes))
	if err != nil {
		return nil, fmt.Errorf("failed to load/parse template from %v, error: %v", tplFilePath, err)
	}
	pageTemplates[key] = tpl
	return tpl, nil
}
