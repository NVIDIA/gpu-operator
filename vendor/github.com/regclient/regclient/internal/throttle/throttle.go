// Package throttle is used to limit concurrent activities
package throttle

import (
	"context"
	"fmt"
)

type token struct{}
type Throttle struct {
	ch chan token
}
type key int
type valMany struct {
	tList []*Throttle
}

var keyMany key

func New(count int) *Throttle {
	ch := make(chan token, count)
	return &Throttle{ch: ch}
}

func (t *Throttle) checkContext(ctx context.Context) (bool, error) {
	tCtx := ctx.Value(keyMany)
	if tCtx == nil {
		return false, nil
	}
	tCtxVal, ok := tCtx.(*valMany)
	if !ok {
		return true, fmt.Errorf("context value is not a throttle list")
	}
	if tCtxVal.tList == nil {
		return false, nil
	}
	for _, cur := range tCtxVal.tList {
		if cur == t {
			// instance already locked
			return true, nil
		}
	}
	return true, fmt.Errorf("cannot acquire new locks during a transaction")
}

func (t *Throttle) Acquire(ctx context.Context) error {
	if t == nil {
		return nil
	}
	// check if already acquired in context
	if found, err := t.checkContext(ctx); found {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case t.ch <- token{}:
		return nil
	}
}

func (t *Throttle) Release(ctx context.Context) error {
	if t == nil {
		return nil
	}
	// check if already acquired in context
	if found, err := t.checkContext(ctx); found {
		return err
	}
	select {
	case <-t.ch:
		return nil
	default:
		return fmt.Errorf("failed to release throttle")
	}
}

func (t *Throttle) TryAcquire(ctx context.Context) (bool, error) {
	if t == nil {
		return true, nil
	}
	// check if already acquired in context
	if found, err := t.checkContext(ctx); found {
		return err == nil, err
	}
	select {
	case t.ch <- token{}:
		return true, nil
	default:
		return false, nil
	}
}

func AcquireMulti(ctx context.Context, tList []*Throttle) (context.Context, error) {
	// verify context not already holding locks
	tCtx := ctx.Value(keyMany)
	if tCtx != nil {
		if tCtxVal, ok := tCtx.(*valMany); ok && tCtxVal.tList != nil {
			return ctx, fmt.Errorf("throttle cannot manage concurrent transactions")
		}
	}
	if len(tList) <= 0 {
		// noop?
		return ctx, nil
	}
	// dedup entries from the list
	for i := len(tList) - 2; i >= 0; i-- {
		for j := len(tList) - 1; j > i; j-- {
			if tList[i] == tList[j] {
				// delete j from the list
				tList[j] = tList[len(tList)-1]
				tList = tList[:len(tList)-1]
			}
		}
	}
	lockI := 0
	for {
		err := tList[lockI].Acquire(ctx)
		if err != nil {
			return ctx, err
		}
		acquired := true
		i := 0
		for i < len(tList) {
			if i != lockI {
				acquired, err = tList[i].TryAcquire(ctx)
				if err != nil || !acquired {
					break
				}
			}
			i++
		}
		if err == nil && acquired {
			break
		}
		// TODO: errors on Release should be included using errors.Join once 1.20 is the minimum version
		// cleanup on failed attempt
		if lockI > i {
			_ = tList[lockI].Release(ctx)
		}
		// track blocking index
		lockI = i
		for i > 0 {
			i--
			_ = tList[i].Release(ctx)
		}
		// abort on errors
		if err != nil {
			return ctx, err
		}
	}
	// success, update context
	newCtx := context.WithValue(ctx, keyMany, &valMany{tList: tList})
	return newCtx, nil
}

func ReleaseMulti(ctx context.Context, tList []*Throttle) error {
	// verify context shows locked values
	tCtx := ctx.Value(keyMany)
	if tCtx == nil {
		return fmt.Errorf("no transaction found to release")
	}
	tCtxVal, ok := tCtx.(*valMany)
	if !ok || tCtxVal.tList == nil {
		return fmt.Errorf("no transaction found to release")
	}
	// dedup entries from the list
	for i := len(tList) - 2; i >= 0; i-- {
		for j := len(tList) - 1; j > i; j-- {
			if tList[i] == tList[j] {
				// delete j from the list
				tList[j] = tList[len(tList)-1]
				tList = tList[:len(tList)-1]
			}
		}
	}
	// TODO: release from tList, tCtx, or compare and error if diff?
	for _, t := range tList {
		if t == nil {
			continue
		}
		// cannot call t.Release since context has value defined
		select {
		case <-t.ch:
		default:
			return fmt.Errorf("failed to release throttle")
		}
	}
	// modify context value to track release
	tCtxVal.tList = nil
	return nil
}
