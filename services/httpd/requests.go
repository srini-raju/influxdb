package httpd

import (
	"container/list"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/influxdata/influxdb/services/meta"
)

type RequestInfo struct {
	IPAddr   string
	Username string
}

type RequestStats struct {
	Counter int64 `json:"counter"`
}

func (r *RequestInfo) String() string {
	if r.Username != "" {
		return fmt.Sprintf("%s:%s", r.Username, r.IPAddr)
	}
	return r.IPAddr
}

type RequestProfile struct {
	Requests map[RequestInfo]*RequestStats
	tracker  *RequestTracker
	elem     *list.Element
	mu       sync.RWMutex
}

func (p *RequestProfile) Add(info RequestInfo) {
	// Look for a request entry for this request.
	p.mu.RLock()
	st, ok := p.Requests[info]
	p.mu.RUnlock()
	if ok {
		atomic.AddInt64(&st.Counter, 1)
		return
	}

	// There is no entry in the request tracker. Create one.
	p.mu.Lock()
	if st, ok := p.Requests[info]; ok {
		// Something else created this entry while we were waiting for the lock.
		p.mu.Unlock()
		atomic.AddInt64(&st.Counter, 1)
		return
	}

	st = &RequestStats{}
	p.Requests[info] = st
	p.mu.Unlock()
	atomic.AddInt64(&st.Counter, 1)
}

// Stop informs the RequestTracker to stop collecting statistics for this
// profile.
func (p *RequestProfile) Stop() {
	p.tracker.mu.Lock()
	p.tracker.profiles.Remove(p.elem)
	p.tracker.mu.Unlock()
}

type RequestTracker struct {
	profiles *list.List
	mu       sync.RWMutex
}

func NewRequestTracker() *RequestTracker {
	return &RequestTracker{
		profiles: list.New(),
	}
}

func (rt *RequestTracker) TrackRequests() *RequestProfile {
	// Perform the memory allocation outside of the lock.
	profile := &RequestProfile{
		Requests: make(map[RequestInfo]*RequestStats),
		tracker:  rt,
	}

	rt.mu.Lock()
	profile.elem = rt.profiles.PushBack(profile)
	rt.mu.Unlock()
	return profile
}

func (rt *RequestTracker) Add(req *http.Request, user *meta.UserInfo) {
	rt.mu.RLock()
	if rt.profiles.Len() == 0 {
		rt.mu.RUnlock()
		return
	}
	defer rt.mu.RUnlock()

	var info RequestInfo
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return
	}

	info.IPAddr = host
	if user != nil {
		info.Username = user.Name
	}

	// Add the request info to the profiles.
	for p := rt.profiles.Front(); p != nil; p = p.Next() {
		p.Value.(*RequestProfile).Add(info)
	}
}
