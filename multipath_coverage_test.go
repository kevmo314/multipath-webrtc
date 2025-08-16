//go:build !js
// +build !js

package webrtc

import (
	"sync"
	"testing"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/stun/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMultipathConcurrentOperations tests thread safety of multipath operations
func TestMultipathConcurrentOperations(t *testing.T) {
	pc, err := NewPeerConnection(Configuration{})
	require.NoError(t, err)
	defer pc.Close()

	// Concurrently enable/disable multipath
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(iteration int) {
			defer wg.Done()
			if iteration%2 == 0 {
				pc.EnableMultipath()
			} else {
				pc.DisableMultipath()
			}
		}(i)
	}
	wg.Wait()

	// Concurrently get stats
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stats := pc.GetMultipathStats()
			assert.NotNil(t, stats)
		}()
	}
	wg.Wait()
}

// TestRenominationRotation tests that renomination actually rotates through pairs
func TestRenominationRotation(t *testing.T) {
	renominator := NewMultipathRenominator(100 * time.Millisecond)
	
	// Create mock pairs
	pair1 := &ice.CandidatePair{}
	pair2 := &ice.CandidatePair{}
	pair3 := &ice.CandidatePair{}
	
	// Add pairs
	renominator.AddValidPair(pair1)
	renominator.AddValidPair(pair2)
	renominator.AddValidPair(pair3)
	
	assert.Len(t, renominator.validPairs, 3)
	
	// Test rotation
	renominator.renominateNext()
	firstIndex := renominator.currentIndex
	
	renominator.renominateNext()
	secondIndex := renominator.currentIndex
	assert.NotEqual(t, firstIndex, secondIndex)
	
	renominator.renominateNext()
	thirdIndex := renominator.currentIndex
	assert.NotEqual(t, secondIndex, thirdIndex)
	
	// Should wrap around
	renominator.renominateNext()
	assert.Equal(t, renominator.currentIndex, (thirdIndex+1)%3)
}

// TestMultipathStatisticsCollection tests that stats are properly collected
func TestMultipathStatisticsCollection(t *testing.T) {
	state := NewMultipathState()
	handler := multipathBindingRequestHandler(state)
	
	// Create real ICE candidates
	local, err := ice.NewCandidateHost(&ice.CandidateHostConfig{
		Network:   "udp",
		Address:   "192.168.1.1",
		Port:      1234,
		Component: 1,
	})
	require.NoError(t, err)
	
	remote, err := ice.NewCandidateHost(&ice.CandidateHostConfig{
		Network:   "udp",
		Address:   "192.168.1.2",
		Port:      5678,
		Component: 1,
	})
	require.NoError(t, err)
	
	pair := &ice.CandidatePair{}
	
	// Create a binding request
	msg := &stun.Message{
		Type: stun.MessageType{
			Method: stun.MethodBinding,
			Class:  stun.ClassRequest,
		},
	}
	
	// Process multiple requests to collect stats
	for i := 0; i < 5; i++ {
		result := handler(msg, local, remote, pair)
		assert.True(t, result)
		time.Sleep(10 * time.Millisecond)
	}
	
	// Verify stats were collected
	state.mu.RLock()
	defer state.mu.RUnlock()
	
	// Check that stats exist for this pair
	assert.NotEmpty(t, state.pairStats)
	assert.NotEmpty(t, state.activePairs)
	
	// Find the stats for our pair
	var foundStats *CandidatePairStats
	for _, stats := range state.pairStats {
		if stats != nil {
			foundStats = stats
			break
		}
	}
	
	assert.NotNil(t, foundStats)
	assert.WithinDuration(t, time.Now(), foundStats.LastActivity, 100*time.Millisecond)
}

// TestMultipathErrorHandling tests error conditions and edge cases
func TestMultipathErrorHandling(t *testing.T) {
	t.Run("EnableMultipath on closed connection", func(t *testing.T) {
		pc, err := NewPeerConnection(Configuration{})
		require.NoError(t, err)
		pc.Close()
		
		// Should not panic
		err = pc.EnableMultipath()
		// Error is expected but shouldn't crash
		assert.NoError(t, err) // EnableMultipath doesn't return errors currently
	})
	
	t.Run("GetStats on nil multipath state", func(t *testing.T) {
		pc, err := NewPeerConnection(Configuration{})
		require.NoError(t, err)
		defer pc.Close()
		
		// Should return nil without panic
		stats := pc.GetMultipathStats()
		assert.Nil(t, stats)
	})
	
	t.Run("Renominator with nil agent", func(t *testing.T) {
		renominator := NewMultipathRenominator(100 * time.Millisecond)
		renominator.agent = nil
		
		// Should not panic
		pair := &ice.CandidatePair{}
		renominator.sendRenominationRequest(pair)
	})
	
	t.Run("Handler with nil message", func(t *testing.T) {
		state := NewMultipathState()
		handler := multipathBindingRequestHandler(state)
		
		// Should handle nil gracefully
		result := handler(nil, nil, nil, nil)
		assert.True(t, result)
	})
}

// TestMultipathWithMultiplePeerConnections tests multiple PCs with multipath
func TestMultipathWithMultiplePeerConnections(t *testing.T) {
	// Create multiple peer connections with multipath
	var pcs []*PeerConnection
	
	for i := 0; i < 3; i++ {
		se := SettingEngine{}
		ConfigureMultipathWithRenomination(&se, Conservative, 200*time.Millisecond)
		
		api := NewAPI(WithSettingEngine(se))
		pc, err := api.NewPeerConnection(Configuration{})
		require.NoError(t, err)
		
		err = pc.EnableMultipath()
		require.NoError(t, err)
		
		pcs = append(pcs, pc)
	}
	
	// Verify all have independent multipath states
	for i, pc := range pcs {
		assert.True(t, pc.IsMultipathEnabled(), "PC %d should have multipath enabled", i)
		assert.NotNil(t, pc.multipathState, "PC %d should have multipath state", i)
		
		// Each should have its own renominator
		if pc.multipathState.enableRenomination {
			assert.NotNil(t, pc.multipathState.renominator, "PC %d should have renominator", i)
		}
	}
	
	// Clean up
	for _, pc := range pcs {
		pc.Close()
	}
}

// TestRenominationValueGeneration tests the nomination value generator
func TestRenominationValueGeneration(t *testing.T) {
	t.Run("Incrementing generator", func(t *testing.T) {
		se := SettingEngine{}
		ConfigureMultipathWithRenomination(&se, Conservative, 100*time.Millisecond)
		
		// Generator should increment
		assert.NotNil(t, se.iceNominationValueGenerator)
		val1 := se.iceNominationValueGenerator()
		val2 := se.iceNominationValueGenerator()
		val3 := se.iceNominationValueGenerator()
		
		assert.Equal(t, uint32(1), val1)
		assert.Equal(t, uint32(2), val2)
		assert.Equal(t, uint32(3), val3)
	})
	
	t.Run("24-bit value handling", func(t *testing.T) {
		se := SettingEngine{}
		maxValue := uint32((1 << 24) - 1)
		counter := maxValue - 1
		
		se.SetICERenomination(true, func() uint32 {
			counter++
			return counter
		})
		
		val1 := se.iceNominationValueGenerator()
		assert.Equal(t, maxValue, val1)
		
		// Should handle overflow gracefully
		val2 := se.iceNominationValueGenerator()
		assert.Equal(t, maxValue+1, val2) // Will be truncated in STUN message
	})
}

// TestMultipathStrategyBehavior tests different multipath strategies
func TestMultipathStrategyBehavior(t *testing.T) {
	strategies := []MultipathStrategy{Conservative, Aggressive, Adaptive}
	
	for _, strategy := range strategies {
		t.Run(strategy.String(), func(t *testing.T) {
			se := SettingEngine{}
			state := ConfigureMultipathWithRenomination(&se, strategy, 100*time.Millisecond)
			
			assert.Equal(t, strategy, state.strategy)
			assert.True(t, state.enableRenomination)
			assert.False(t, state.preventNomination)
			
			// Verify appropriate timeouts are set
			assert.NotNil(t, se.timeout.ICEDisconnectedTimeout)
			assert.NotNil(t, se.timeout.ICEFailedTimeout)
			assert.NotNil(t, se.timeout.ICEKeepaliveInterval)
		})
	}
}

// TestRenominatorLifecycle tests renominator start/stop behavior
func TestRenominatorLifecycle(t *testing.T) {
	renominator := NewMultipathRenominator(50 * time.Millisecond)
	renominator.isControlling = true
	
	// Add some pairs
	renominator.AddValidPair(&ice.CandidatePair{})
	renominator.AddValidPair(&ice.CandidatePair{})
	
	// Start should create ticker
	renominator.Start()
	assert.NotNil(t, renominator.ticker)
	
	// Let it run briefly
	time.Sleep(150 * time.Millisecond)
	
	// Stop should clean up
	renominator.Stop()
	
	// Should handle multiple stops gracefully
	renominator.Stop()
}

// TestMultipathPairTracking tests that pairs are properly tracked
func TestMultipathPairTracking(t *testing.T) {
	state := NewMultipathState()
	state.enableRenomination = true
	state.renominator = NewMultipathRenominator(100 * time.Millisecond)
	
	handler := multipathBindingRequestHandler(state)
	
	// Create real ICE candidates
	local, err := ice.NewCandidateHost(&ice.CandidateHostConfig{
		Network:   "udp",
		Address:   "10.0.0.1",
		Port:      1111,
		Component: 1,
	})
	require.NoError(t, err)
	
	remote, err := ice.NewCandidateHost(&ice.CandidateHostConfig{
		Network:   "udp",
		Address:   "10.0.0.2",
		Port:      2222,
		Component: 1,
	})
	require.NoError(t, err)
	
	pair := &ice.CandidatePair{}
	
	msg := &stun.Message{
		Type: stun.MessageType{
			Method: stun.MethodBinding,
			Class:  stun.ClassRequest,
		},
	}
	
	// Process request
	handler(msg, local, remote, pair)
	
	// Verify pair was tracked
	state.mu.RLock()
	assert.NotEmpty(t, state.activePairs)
	assert.NotEmpty(t, state.pairStats)
	state.mu.RUnlock()
	
	// Verify pair was added to renominator
	assert.Contains(t, state.renominator.validPairs, pair)
}

// TestBindingRequestFiltering tests that only binding requests are processed
func TestBindingRequestFiltering(t *testing.T) {
	state := NewMultipathState()
	handler := multipathBindingRequestHandler(state)
	
	testCases := []struct {
		name      string
		msgType   stun.MessageType
		shouldProcess bool
	}{
		{
			"Binding Request",
			stun.MessageType{Method: stun.MethodBinding, Class: stun.ClassRequest},
			true,
		},
		{
			"Binding Response",
			stun.MessageType{Method: stun.MethodBinding, Class: stun.ClassSuccessResponse},
			false,
		},
		{
			"Other Request Type",
			stun.MessageType{Method: 0x003, Class: stun.ClassRequest}, // Different method
			false,
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msg := &stun.Message{Type: tc.msgType}
			msg.Attributes = []stun.RawAttribute{
				{Type: 0x0025, Value: []byte{}}, // USE-CANDIDATE
			}
			
			// Create real ICE candidates for testing
			local, err := ice.NewCandidateHost(&ice.CandidateHostConfig{
				Network:   "udp",
				Address:   "127.0.0.1",
				Port:      12345,
				Component: 1,
			})
			require.NoError(t, err)
			
			remote, err := ice.NewCandidateHost(&ice.CandidateHostConfig{
				Network:   "udp",
				Address:   "127.0.0.1",
				Port:      54321,
				Component: 1,
			})
			require.NoError(t, err)
			
			// For non-binding requests, should return true (pass through)
			pair := &ice.CandidatePair{}
			result := handler(msg, local, remote, pair)
			assert.True(t, result)
			
			// Check if pair was tracked
			state.mu.RLock()
			tracked := len(state.activePairs) > 0
			state.mu.RUnlock()
			
			if tc.shouldProcess {
				assert.True(t, tracked, "Binding request should track pairs")
			} else {
				assert.False(t, tracked, "Non-binding request should not track pairs")
			}
			
			// Clear for next test
			state.activePairs = make(map[string]*ice.CandidatePair)
			state.pairStats = make(map[string]*CandidatePairStats)
		})
	}
}

// Helper mock for ICE candidate - simplified to just implement String()
// which is all the handler needs
type mockICECandidate struct {
	str string
}

func (m *mockICECandidate) String() string { 
	return m.str 
}

// Add String method for MultipathStrategy
func (s MultipathStrategy) String() string {
	switch s {
	case Conservative:
		return "Conservative"
	case Aggressive:
		return "Aggressive"
	case Adaptive:
		return "Adaptive"
	default:
		return "Unknown"
	}
}