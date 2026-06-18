package scanner

import (
	"math/rand"
	"sync"
	"time"
)

// UserAgent pool of common, realistic browser user agents.
// Rotating UAs helps avoid detection by Cloudflare/WAF/bot protection.
var userAgents = []string{
	// Chrome on Windows
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 11.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36",

	// Chrome on macOS
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",

	// Chrome on Linux
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",

	// Firefox on Windows
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:126.0) Gecko/20100101 Firefox/126.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:125.0) Gecko/20100101 Firefox/125.0",

	// Firefox on macOS
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:126.0) Gecko/20100101 Firefox/126.0",

	// Safari on macOS
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15",

	// Edge on Windows
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36 Edg/125.0.0.0",
}

var (
	uaRand  *rand.Rand
	uaMutex sync.Mutex
)

func init() {
	uaRand = rand.New(rand.NewSource(time.Now().UnixNano()))
}

// RandomUserAgent returns a random browser user agent from the pool.
func RandomUserAgent() string {
	uaMutex.Lock()
	defer uaMutex.Unlock()
	return userAgents[uaRand.Intn(len(userAgents))]
}

// UserAgentPool exposes the user agent pool
type UserAgentPool struct {
	agents []string
	index  int
	mu     sync.Mutex
	rand   *rand.Rand
}

// NewUserAgentPool creates a new user agent pool with rotation
func NewUserAgentPool() *UserAgentPool {
	return &UserAgentPool{
		agents: userAgents,
		index:  0,
		rand:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Next returns the next user agent (round-robin)
func (p *UserAgentPool) Next() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	ua := p.agents[p.index]
	p.index = (p.index + 1) % len(p.agents)
	return ua
}

// Random returns a random user agent
func (p *UserAgentPool) Random() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.agents[p.rand.Intn(len(p.agents))]
}