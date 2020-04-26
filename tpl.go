package main

import (
	"html/template"
	"io"
	"os"
	"path/filepath"
)

type tplExecer interface {
	Execute(io.Writer, interface{}) error
}

func assetTemplate(funcs template.FuncMap, fpath ...string) (*template.Template, error) {
	var ds [][]byte
	for _, fp := range fpath {
		a, err := Asset(fp)
		if err != nil {
			continue
		}
		ds = append(ds, a)
	}
	return byteTemplate(funcs, ds...)
}

func byteTemplate(funcs template.FuncMap, data ...[]byte) (*template.Template, error) {
	tt := template.New("b").Funcs(funcs)
	var err error
	for _, d := range data {
		if tt, err = tt.Parse(string(d)); err != nil {
			break
		}
	}
	return tt, err
}

func fileTemplate(funcs template.FuncMap, fpath ...string) (*fsTemplate, error) {
	t := &fsTemplate{
		fpath: fpath,
		funcs: funcs,
	}
	return t, nil
}

type fsTemplate struct {
	fpath []string
	funcs template.FuncMap
}

func (t *fsTemplate) parse() (*template.Template, error) {
	pt := []string{}
	for _, fp := range t.fpath {
		if _, err := os.Stat(fp); os.IsNotExist(err) {
			continue
		}
		pt = append(pt, fp)
	}
	var n string
	if len(pt) > 0 {
		n = filepath.Base(pt[0])
	}
	return template.New(n).Funcs(t.funcs).ParseFiles(pt...)
}

func (t *fsTemplate) Execute(wr io.Writer, data interface{}) error {
	tpl, err := t.parse()
	if err != nil {
		return err
	}
	return tpl.Execute(wr, data)
}
