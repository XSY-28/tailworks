package fakemgmt

// countReadyLocked records a ready PATCH for objectID (s.mu held).
func (s *Server) countReadyLocked(objectID string) {
	if s.readyCalls == nil {
		s.readyCalls = map[string]int{}
	}
	s.readyCalls[objectID]++
}

// ReadyCalls counts the ready PATCHes hostd sent for objectID (tests: one
// node start reports ready once, issue #133).
func (s *Server) ReadyCalls(objectID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readyCalls[objectID]
}

// SetLeaseLostReady makes the next n ready PATCHes fail with lease_lost
// (tests: a node that joined at a lease epoch a `tw deploy` has since
// bumped, issue #210). Refused PATCHes are not counted by ReadyCalls.
func (s *Server) SetLeaseLostReady(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leaseLostReady = n
}

// refuseReadyLocked consumes one SetLeaseLostReady refusal (s.mu held).
func (s *Server) refuseReadyLocked() bool {
	if s.leaseLostReady <= 0 {
		return false
	}
	s.leaseLostReady--
	return true
}
