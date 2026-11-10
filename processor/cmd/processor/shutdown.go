package main

import (
	"sync"
	"time"
)

// discordStartupWait bounds how long shutdown waits for an in-flight Discord
// connection attempt. Session.Open can't be cancelled and can block for over a
// minute on a slow network, which would outlast Docker's 10s stop timeout and
// skip the state saves later in shutdown.
const discordStartupWait = 5 * time.Second

// waitTimeout waits for wg to finish, giving up after d. Reports whether wg
// finished in time.
func waitTimeout(wg *sync.WaitGroup, d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}
