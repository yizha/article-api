package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"time"
)

type LogFields map[string]interface{}

type JsonLogger struct {
	messageFieldName string
	outputSourceFile bool
	fields           LogFields
	l                *log.Logger
}

func (jl *JsonLogger) Output(calldepth int, v map[string]interface{}) {
	m := make(map[string]interface{})
	if jl.fields != nil && len(jl.fields) > 0 {
		for k, v := range jl.fields {
			m[k] = v
		}
	}
	if v != nil && len(v) > 0 {
		for k, v := range v {
			m[k] = v
		}
	}
	if jl.outputSourceFile {
		_, file, line, ok := runtime.Caller(calldepth)
		if ok {
			short := file
			for i := len(file) - 1; i > 0; i-- {
				if file[i] == '/' {
					short = file[i+1:]
					break
				}
			}
			file = short
		} else {
			file = "???"
			line = 0
		}
		m["_source_file"] = fmt.Sprintf("%v:%v", file, line)
	}
	m["_log_ts"] = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	bytes, err := json.Marshal(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to encoding %v to json, error: %v", v, err)
	} else if err = jl.l.Output(calldepth, string(bytes)); err != nil {
		fmt.Fprintf(os.Stderr, "failed to log, error: %v", err)
	}
}

func (jl *JsonLogger) LogMap(v map[string]interface{}) {
	jl.Output(2, v)
}

func (jl *JsonLogger) logFieldsWithCalldepth(calldepth int, v ...interface{}) {
	entryCnt := len(v) / 2
	m := make(map[string]interface{}, entryCnt+2)
	for i := 0; i <= entryCnt; i = i + 2 {
		m[fmt.Sprintf("%v", v[i])] = v[i+1]
	}
	jl.Output(calldepth, m)
}

func (jl *JsonLogger) LogFields(v ...interface{}) {
	jl.logFieldsWithCalldepth(3, v...)
}

func (jl *JsonLogger) Print(s string) {
	jl.logFieldsWithCalldepth(3, jl.messageFieldName, s)
}

func (jl *JsonLogger) Printf(f string, v ...interface{}) {
	jl.logFieldsWithCalldepth(3, jl.messageFieldName, fmt.Sprintf(f, v...))
}

func (jl *JsonLogger) SetMessageFieldName(s string) *JsonLogger {
	jl.messageFieldName = s
	return jl
}

func (jl *JsonLogger) SetFields(fields LogFields) *JsonLogger {
	jl.fields = fields
	return jl
}

func (jl *JsonLogger) SetOutputSourceFile(b bool) *JsonLogger {
	jl.outputSourceFile = b
	return jl
}

// create a new JsonLogger instance with the same underlying fields
// but replacing the fields with the given LogFields
func (jl *JsonLogger) CloneWithFields(fields LogFields) *JsonLogger {
	return &JsonLogger{
		messageFieldName: jl.messageFieldName,
		outputSourceFile: jl.outputSourceFile,
		fields:           fields,
		l:                jl.l,
	}
}

func NewJsonLogger(out io.Writer) *JsonLogger {
	return &JsonLogger{
		l:                log.New(out, "", 0),
		messageFieldName: "message",
	}
}
