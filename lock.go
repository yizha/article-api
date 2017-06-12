package main

import (
	"sync"
)

type StrMutex struct {
	parent *UniqStrMutex
	value  string
	mutex  *sync.Mutex
	cnt    int
}

func (sm *StrMutex) Lock() {
	sm.mutex.Lock()
}

func (sm *StrMutex) Unlock() {
	sm.mutex.Unlock()
	sm.parent.return_(sm)
}

type UniqStrMutex struct {
	l     *sync.Mutex
	locks map[string]*StrMutex
}

func (l *UniqStrMutex) Get(val string) *StrMutex {
	l.l.Lock()
	defer l.l.Unlock()

	sm, ok := l.locks[val]
	if ok {
		sm.cnt += 1
	} else {
		sm = &StrMutex{l, val, &sync.Mutex{}, 1}
		l.locks[val] = sm
	}
	return sm
}

func (l *UniqStrMutex) return_(sm *StrMutex) {
	l.l.Lock()
	defer l.l.Unlock()

	sm.cnt -= 1
	if sm.cnt == 0 {
		delete(l.locks, sm.value)
	}
}
