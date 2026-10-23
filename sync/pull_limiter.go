package sync

import (
	"context"
	stdsync "sync"
)

type pullUser struct {
	slot chan struct{}
	refs int
}

// pullLimiter bounds active pull work globally and permits one pull per user.
type pullLimiter struct {
	mu    stdsync.Mutex
	users map[string]*pullUser
	slots chan struct{}
}

func newPullLimiter(limit int) *pullLimiter {
	return &pullLimiter{users: make(map[string]*pullUser), slots: make(chan struct{}, limit)}
}

func (l *pullLimiter) acquire(ctx context.Context, uid string) (func(), error) {
	l.mu.Lock()
	user := l.users[uid]
	if user == nil {
		user = &pullUser{slot: make(chan struct{}, 1)}
		l.users[uid] = user
	}
	user.refs++
	l.mu.Unlock()
	dropReference := func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		user.refs--
		if user.refs == 0 {
			delete(l.users, uid)
		}
	}

	// A queued reconnect from one user must not occupy a global slot.
	select {
	case user.slot <- struct{}{}:
	case <-ctx.Done():
		dropReference()
		return nil, ctx.Err()
	}
	select {
	case l.slots <- struct{}{}:
	case <-ctx.Done():
		<-user.slot
		dropReference()
		return nil, ctx.Err()
	}
	return func() {
		<-l.slots
		<-user.slot
		dropReference()
	}, nil
}
