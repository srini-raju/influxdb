package httpd

import (
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

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
	id       int64
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
	delete(p.tracker.profiles, p.id)
	p.tracker.mu.Unlock()
}

type RequestTracker struct {
	profiles map[int64]*RequestProfile
	rand     *rand.Rand
	mu       sync.RWMutex
}

func NewRequestTracker() *RequestTracker {
	return &RequestTracker{
		profiles: make(map[int64]*RequestProfile),
		rand:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (rt *RequestTracker) TrackRequests() *RequestProfile {
	// Perform the memory allocation outside of the lock.
	profile := &RequestProfile{
		Requests: make(map[RequestInfo]*RequestStats),
		tracker:  rt,
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	for {
		n := rt.rand.Int63()
		if _, ok := rt.profiles[n]; ok {
			continue
		}
		profile.id = n
		rt.profiles[n] = profile
		return profile
	}
}

func (rt *RequestTracker) Add(req *http.Request, user *meta.UserInfo) {
	rt.mu.RLock()
	if len(rt.profiles) == 0 {
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
	for _, profile := range rt.profiles {
		profile.Add(info)
	}
}
