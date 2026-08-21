package evidence

import (
	"context"
	"sync"
)

type ComparisonAdmission struct {
	budget  int64
	mu      sync.Mutex
	used    int64
	waiting int
	cond    *sync.Cond
}

func NewComparisonAdmission(budget int64) (*ComparisonAdmission, error) {
	if budget < ComparisonAdmissionTokenBytes {
		return nil, ErrComparisonRequestMemoryLimit
	}
	admission := &ComparisonAdmission{budget: budget}
	admission.cond = sync.NewCond(&admission.mu)
	return admission, nil
}

func (admission *ComparisonAdmission) Budget() int64 {
	if admission == nil {
		return 0
	}
	return admission.budget
}

func (admission *ComparisonAdmission) Acquire(ctx context.Context, weight int64) (func(), error) {
	if admission == nil || ctx == nil {
		return nil, ErrComparisonCapacityExhausted
	}
	if weight <= 0 || weight > admission.budget {
		return nil, ErrComparisonRequestMemoryLimit
	}
	if err := ctx.Err(); err != nil {
		return nil, ErrComparisonCapacityExhausted
	}
	waitCtx, cancelWait := context.WithTimeout(ctx, ComparisonAdmissionWait)
	defer cancelWait()

	admission.mu.Lock()
	for admission.used+weight > admission.budget {
		if waitCtx.Err() != nil || admission.waiting >= ComparisonAdmissionMaxQueue {
			admission.mu.Unlock()
			return nil, ErrComparisonCapacityExhausted
		}
		admission.waiting++
		stopped := make(chan struct{})
		go func() {
			select {
			case <-waitCtx.Done():
				admission.cond.Broadcast()
			case <-stopped:
			}
		}()
		admission.cond.Wait()
		close(stopped)
		admission.waiting--
	}
	admission.used += weight
	admission.mu.Unlock()

	var once sync.Once
	release := func() {
		once.Do(func() {
			admission.mu.Lock()
			admission.used -= weight
			if admission.used < 0 {
				admission.used = 0
			}
			admission.cond.Broadcast()
			admission.mu.Unlock()
		})
	}
	go func() {
		<-ctx.Done()
		release()
	}()
	return release, nil
}
