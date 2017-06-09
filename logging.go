package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

type LogFields map[string]interface{}

type JsonLogger struct {
	messageFieldName string
	outputSourceFile bool
	fields           LogFields
	l                *log.Logger
}

func (jl *JsonLogger) Output(calldepth int, v LogFields, x ...interface{}) {
	m := make(map[string]interface{})
	if jl.fields != nil && len(jl.fields) > 0 {
		for k, v := range jl.fields {
			m[k] = v
		}
	}
	if v != nil && len(v) > 0 {
		for k, v := range v {
			if k != "" {
				m[k] = v
			}
		}
	}
	xLen := len(x)
	if xLen > 0 {
		cnt := xLen / 2
		for i := 0; i <= cnt; i = i + 2 {
			k := fmt.Sprintf("%v", x[i])
			if k != "" {
				m[k] = x[i+1]
			}
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
		m["source_file"] = fmt.Sprintf("%v:%v", file, line)
	}
	ts := time.Now().UTC()
	m["log_ts"] = ts.UnixNano()
	m["log_time"] = ts.Format("2006-01-02T15:04:05.000Z")
	bytes, err := json.Marshal(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to encoding %v to json, error: %v", v, err)
	} else if err = jl.l.Output(calldepth, string(bytes)); err != nil {
		fmt.Fprintf(os.Stderr, "failed to log, error: %v", err)
	}
}

func array2map(v []interface{}) map[string]interface{} {
	entryCnt := len(v) / 2
	m := make(map[string]interface{}, entryCnt)
	for i := 0; i <= entryCnt; i = i + 2 {
		k := fmt.Sprintf("%v", v[i])
		if k == "" {
			continue
		}
		m[k] = v[i+1]
	}
	return m
}

func (jl *JsonLogger) LogMap(v LogFields) {
	jl.Output(2, v)
}

func (jl *JsonLogger) Log(v ...interface{}) {
	jl.Output(2, array2map(v))
}

func (jl *JsonLogger) Print(v ...interface{}) {
	jl.Output(2, LogFields{jl.messageFieldName: fmt.Sprint(v...)})
}

func (jl *JsonLogger) Printf(f string, v ...interface{}) {
	jl.Output(2, LogFields{jl.messageFieldName: fmt.Sprintf(f, v...)})
}

func (jl *JsonLogger) WarnMap(v LogFields) {
	jl.Output(2, v, "level", "warn")
}

func (jl *JsonLogger) InfoMap(v LogFields) {
	jl.Output(2, v, "level", "info")
}

func (jl *JsonLogger) ErrorMap(v LogFields) {
	jl.Output(2, v, "level", "error")
}

func (jl *JsonLogger) Warn(v ...interface{}) {
	m := array2map(v)
	m["level"] = "warn"
	jl.Output(2, m)
}

func (jl *JsonLogger) Info(v ...interface{}) {
	m := array2map(v)
	m["level"] = "info"
	jl.Output(2, m)
}

func (jl *JsonLogger) Error(v ...interface{}) {
	m := array2map(v)
	m["level"] = "error"
	jl.Output(2, m)
}

func (jl *JsonLogger) Pwarn(v ...interface{}) {
	msg := fmt.Sprint(v...)
	jl.Output(2, LogFields{jl.messageFieldName: msg, "level": "warn"})
}

func (jl *JsonLogger) Pinfo(v ...interface{}) {
	msg := fmt.Sprint(v...)
	jl.Output(2, LogFields{jl.messageFieldName: msg, "level": "info"})
}

func (jl *JsonLogger) Perror(v ...interface{}) {
	msg := fmt.Sprint(v...)
	jl.Output(2, LogFields{jl.messageFieldName: msg, "level": "error"})
}

func (jl *JsonLogger) Pwarnf(f string, v ...interface{}) {
	msg := fmt.Sprintf(f, v...)
	jl.Output(2, LogFields{jl.messageFieldName: msg, "level": "warn"})
}

func (jl *JsonLogger) Pinfof(f string, v ...interface{}) {
	msg := fmt.Sprintf(f, v...)
	jl.Output(2, LogFields{jl.messageFieldName: msg, "level": "info"})
}
func (jl *JsonLogger) Perrorf(f string, v ...interface{}) {
	msg := fmt.Sprintf(f, v...)
	jl.Output(2, LogFields{jl.messageFieldName: msg, "level": "error"})
}

func (jl *JsonLogger) SetMessageFieldName(s string) *JsonLogger {
	jl.messageFieldName = s
	return jl
}

func (jl *JsonLogger) AddFields(fields LogFields) *JsonLogger {
	if fields != nil && len(fields) > 0 {
		if jl.fields == nil {
			jl.fields = make(map[string]interface{})
		}
		for k, v := range fields {
			if k != "" {
				jl.fields[k] = v
			}
		}
	}
	return jl
}

func (jl *JsonLogger) SetFields(fields LogFields) *JsonLogger {
	jl.fields = nil
	jl.AddFields(fields)
	return jl
}

func (jl *JsonLogger) SetOutputSourceFile(b bool) *JsonLogger {
	jl.outputSourceFile = b
	return jl
}

func (jl *JsonLogger) CloneWithFields(newFields LogFields) *JsonLogger {
	fields := make(map[string]interface{})
	if jl.fields != nil && len(jl.fields) > 0 {
		for k, v := range jl.fields {
			fields[k] = v
		}
	}
	if newFields != nil && len(newFields) > 0 {
		for k, v := range newFields {
			fields[k] = v
		}
	}
	return &JsonLogger{
		messageFieldName: jl.messageFieldName,
		outputSourceFile: jl.outputSourceFile,
		fields:           fields,
		l:                jl.l,
	}
}

func CreateLogWriter(ls *LoggingSpec) (io.Writer, error) {
	if ls.Target == LoggingTargetStdout {
		return os.Stdout, nil
	} else if ls.Target == LoggingTargetFile {
		if err := os.MkdirAll(filepath.Dir(ls.Filepath), 0644); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(ls.Filepath, os.O_RDWR|os.O_CREATE, 0755)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return &lumberjack.Logger{
			Filename:   ls.Filepath,
			MaxSize:    ls.MaxSize, // megabytes
			MaxBackups: ls.MaxBackups,
			MaxAge:     ls.MaxAge, //days
		}, nil
	} else {
		return os.Stdout, nil
	}
}

func NewJsonLogger(out io.Writer) *JsonLogger {
	return &JsonLogger{
		l:                log.New(out, "", 0),
		messageFieldName: "message",
		outputSourceFile: true,
	}
}

func NewJsonLoggerFromSpec(ls *LoggingSpec) (*JsonLogger, error) {
	w, err := CreateLogWriter(ls)
	if err != nil {
		return nil, err
	}
	return NewJsonLogger(w), nil
}
