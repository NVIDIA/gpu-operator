// Package muset is used to acquire a group of mutex locks
package muset

import "sync"

// Lock acquires a set of locks without holding some locks acquired while waiting for others to be released
func Lock(muList ...*sync.Mutex) {
	if len(muList) <= 0 {
		return
	}
	// dedup entries from the list
	for i := len(muList) - 2; i >= 0; i-- {
		for j := len(muList) - 1; j > i; j-- {
			if muList[i] == muList[j] {
				// delete j from the list
				muList[j] = muList[len(muList)-1]
				muList = muList[:len(muList)-1]
			}
		}
	}
	lastBlock := 0
	for {
		// start from last blocking mutex
		muList[lastBlock].Lock()
		// acquire all other locks with TryLock
		nextBlock := -1
		for i := range muList {
			if i == lastBlock {
				continue
			}
			acquired := muList[i].TryLock()
			if !acquired {
				nextBlock = i
				break
			}
		}
		// if all locks acquired, done
		if nextBlock == -1 {
			return
		}
		// unlock
		for i := range muList {
			if i < nextBlock || i == lastBlock {
				muList[i].Unlock()
			}
		}
		lastBlock = nextBlock
	}
}
