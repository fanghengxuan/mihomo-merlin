package main

import (
	"sync"
	"testing"
	"time"
)

// 订阅与退订会与发布并发发生。退订不再 close channel，
// 这个测试在 -race 下运行，验证高频 churn 中不会出现
// send on closed channel panic 或数据竞争。
func TestHubPubSubChurnNoPanic(t *testing.T) {
	h := NewHub(&App{})
	stop := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				h.publishTraffic(TrafficSample{Up: 1, Down: 2})
				h.pushLog(LogEntry{Type: "info", Payload: "x"})
				h.publishProgress(ProgressEvent{Op: "test", Step: "run"})
				time.Sleep(time.Millisecond)
			}
		}()
	}

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, unsubTraffic := h.SubscribeTraffic()
			_, _, unsubLogs := h.SubscribeLogs()
			_, unsubProgress := h.SubscribeProgress()
			time.Sleep(time.Millisecond)
			unsubTraffic()
			unsubLogs()
			unsubProgress()
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()

	h.tmu.RLock()
	trafficSubs := len(h.tsubs)
	h.tmu.RUnlock()
	h.lmu.RLock()
	logSubs := len(h.lsubs)
	h.lmu.RUnlock()
	h.pmu.RLock()
	progressSubs := len(h.psubs)
	h.pmu.RUnlock()

	if trafficSubs != 0 || logSubs != 0 || progressSubs != 0 {
		t.Fatalf("subscribers not cleaned up: traffic=%d logs=%d progress=%d",
			trafficSubs, logSubs, progressSubs)
	}
}
