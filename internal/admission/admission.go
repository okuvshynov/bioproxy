package admission

import (
	"context"
	"log"
	"sync"
)

// RequestType represents the type of request currently using llama.cpp
type RequestType int

const (
	// IDLE means no request is currently in flight
	IDLE RequestType = iota
	// USER_QUERY means a user-initiated request is active
	USER_QUERY
	// WARMUP_QUERY means a background warmup request is active
	WARMUP_QUERY
)

// String returns a human-readable name for the request type
func (r RequestType) String() string {
	switch r {
	case IDLE:
		return "IDLE"
	case USER_QUERY:
		return "USER_QUERY"
	case WARMUP_QUERY:
		return "WARMUP_QUERY"
	default:
		return "UNKNOWN"
	}
}

// Controller manages admission control for the llama.cpp backend.
// It ensures atomic state transitions to prevent race conditions between
// user requests and warmup operations.
//
// State machine:
// - IDLE: No requests in flight
// - USER_QUERY: User request is active (can have multiple concurrent user requests)
// - WARMUP_QUERY: Warmup request is active
//
// Transitions:
// - User request: IDLE→USER_QUERY, USER_QUERY→USER_QUERY (allow), WARMUP_QUERY→USER_QUERY (cancel warmup)
// - Warmup request: IDLE→WARMUP_QUERY, USER_QUERY→skip, WARMUP_QUERY→skip
// - Request complete: Any→IDLE (if no other requests)
type Controller struct {
	mu sync.Mutex

	// currentState tracks what kind of request is currently active
	currentState RequestType

	// warmupCancelFunc holds the cancel function for the active warmup (if any)
	warmupCancelFunc context.CancelFunc

	// warmupPrefix holds the prefix being warmed up (for logging)
	warmupPrefix string

	// userQueryCount tracks number of concurrent user queries
	// We allow multiple user queries (llama.cpp queues them)
	userQueryCount int
}

// New creates a new admission controller
func New() *Controller {
	return &Controller{
		currentState: IDLE,
	}
}

// AcquireUserQuery attempts to acquire permission to run a user query.
// This is called at the start of every user request.
//
// Returns:
//   - true if the request should proceed
//   - false if the request should be rejected (shouldn't happen in current design)
//
// Behavior:
//   - If IDLE: transition to USER_QUERY, allow
//   - If USER_QUERY: increment counter, allow (llama.cpp queues)
//   - If WARMUP_QUERY: cancel warmup, transition to USER_QUERY, allow
func (c *Controller) AcquireUserQuery() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch c.currentState {
	case IDLE:
		// Transition from idle to user query
		c.currentState = USER_QUERY
		c.userQueryCount = 1
		log.Printf("Admission: IDLE → USER_QUERY (user request acquired)")
		return true

	case USER_QUERY:
		// Already running user query, allow another (llama.cpp queues)
		c.userQueryCount++
		log.Printf("Admission: USER_QUERY → USER_QUERY (concurrent user request, count=%d)", c.userQueryCount)
		return true

	case WARMUP_QUERY:
		// Cancel the warmup and transition to user query
		if c.warmupCancelFunc != nil {
			log.Printf("Admission: WARMUP_QUERY → USER_QUERY (cancelling warmup for %s)", c.warmupPrefix)
			c.warmupCancelFunc()
		}
		c.currentState = USER_QUERY
		c.userQueryCount = 1
		c.warmupCancelFunc = nil
		c.warmupPrefix = ""
		return true

	default:
		// Unknown state, should not happen
		log.Printf("WARNING: Unknown admission state: %v", c.currentState)
		return true
	}
}

// ReleaseUserQuery releases a user query, potentially transitioning back to IDLE
func (c *Controller) ReleaseUserQuery() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.currentState != USER_QUERY {
		log.Printf("WARNING: ReleaseUserQuery called but state is %s", c.currentState)
		return
	}

	c.userQueryCount--
	if c.userQueryCount <= 0 {
		c.currentState = IDLE
		c.userQueryCount = 0
		log.Printf("Admission: USER_QUERY → IDLE (all user queries completed)")
	} else {
		log.Printf("Admission: USER_QUERY (released one, %d remaining)", c.userQueryCount)
	}
}

// AcquireWarmup attempts to acquire permission to run a warmup query.
// This is called at the start of every warmup attempt.
//
// Returns:
//   - true if warmup should proceed
//   - false if warmup should be skipped
//
// Behavior:
//   - If IDLE: transition to WARMUP_QUERY, return true
//   - If USER_QUERY: return false (skip warmup, user has priority)
//   - If WARMUP_QUERY: return false (already warming, shouldn't happen)
func (c *Controller) AcquireWarmup(prefix string, cancelFunc context.CancelFunc) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch c.currentState {
	case IDLE:
		// Transition from idle to warmup
		c.currentState = WARMUP_QUERY
		c.warmupPrefix = prefix
		c.warmupCancelFunc = cancelFunc
		log.Printf("Admission: IDLE → WARMUP_QUERY (warmup for %s acquired)", prefix)
		return true

	case USER_QUERY:
		// User query is running, skip warmup
		log.Printf("Admission: USER_QUERY (skipping warmup for %s, user has priority)", prefix)
		return false

	case WARMUP_QUERY:
		// Already warming, shouldn't happen but skip
		log.Printf("Admission: WARMUP_QUERY (skipping warmup for %s, already warming %s)", prefix, c.warmupPrefix)
		return false

	default:
		log.Printf("WARNING: Unknown admission state: %v", c.currentState)
		return false
	}
}

// ReleaseWarmup releases a warmup query, transitioning back to IDLE
// If the state is not WARMUP_QUERY, it means the warmup was cancelled by a user request,
// which is expected behavior and not an error.
func (c *Controller) ReleaseWarmup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.currentState != WARMUP_QUERY {
		// This is normal - warmup was cancelled by user request or skipped
		// State is already USER_QUERY or IDLE, no action needed
		return
	}

	c.currentState = IDLE
	c.warmupCancelFunc = nil
	c.warmupPrefix = ""
	log.Printf("Admission: WARMUP_QUERY → IDLE (warmup completed)")
}

// GetCurrentState returns the current admission state (for debugging/metrics)
func (c *Controller) GetCurrentState() RequestType {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentState
}
