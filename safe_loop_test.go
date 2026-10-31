package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 常驻循环 panic 后必须被重新拉起，而不是永久停摆。
func TestSafeLoopRestartsAfterPanic(t *testing.T) {
	old := loopBackoffs
	loopBackoffs = []time.Duration{time.Millisecond}
	defer func() { loopBackoffs = old }()

	stop := make(chan struct{})
	finished := make(chan struct{})
	var calls atomic.Int32
	var once sync.Once

	go func() {
		defer close(finished)
		safeLoopWithStop("test-loop", func() {
			n := calls.Add(1)
			if n == 1 {
				panic("boom")
			}
			if n >= 3 {
				once.Do(func() { close(stop) })
			}
		}, stop)
	}()

	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("safeLoop did not restart after panic")
	}
	if got := calls.Load(); got < 3 {
		t.Fatalf("loop restarted only %d times, want >=3", got)
	}
}
