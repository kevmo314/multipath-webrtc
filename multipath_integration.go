// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package webrtc

import (
	"sync"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/stun/v3"
)

// MultipathState tracks multipath operation state for a PeerConnection
type MultipathState struct {
	enabled           bool
	preventNomination bool
	activePairs       map[string]*ice.CandidatePair
	pairStats         map[string]*CandidatePairStats
	mu                sync.RWMutex
	
	// Renomination support
	enableRenomination   bool
	renominationInterval time.Duration
	renominator         *MultipathRenominator
	strategy            MultipathStrategy
}

// MultipathStrategy defines how packets are distributed
type MultipathStrategy int

const (
	// Conservative: Send only on nominated pair with fast renomination
	Conservative MultipathStrategy = iota
	// Aggressive: Send on all valid pairs simultaneously  
	Aggressive
	// Adaptive: Start conservative, upgrade to aggressive if receiver supports it
	Adaptive
)

// MultipathRenominator handles continuous renomination for multipath
type MultipathRenominator struct {
	validPairs       []*ice.CandidatePair
	currentIndex     int
	rotationSpeed    time.Duration
	isControlling    bool
	agent           *ice.Agent
	ticker          *time.Ticker
	done            chan struct{}
	mu              sync.RWMutex
}

// CandidatePairStats tracks statistics for a candidate pair
type CandidatePairStats struct {
	PacketsSent   uint64
	PacketsRecv   uint64
	BytesSent     uint64
	BytesRecv     uint64
	LastActivity  time.Time
	RTT           time.Duration
	NominationPrevented int
}

// NewMultipathState creates a new multipath state tracker
func NewMultipathState() *MultipathState {
	return &MultipathState{
		enabled:              false,
		preventNomination:    false, // Changed: Allow nominations for renomination strategy
		activePairs:          make(map[string]*ice.CandidatePair),
		pairStats:            make(map[string]*CandidatePairStats),
		enableRenomination:   true,
		renominationInterval: 200 * time.Millisecond, // Fast rotation default
		strategy:             Conservative, // Start conservative
	}
}

// NewMultipathRenominator creates a new renomination controller
func NewMultipathRenominator(rotationSpeed time.Duration) *MultipathRenominator {
	return &MultipathRenominator{
		validPairs:    make([]*ice.CandidatePair, 0),
		rotationSpeed: rotationSpeed,
		done:          make(chan struct{}),
	}
}

// Start begins the renomination rotation
func (r *MultipathRenominator) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	if !r.isControlling {
		return // Can only renominate as controlling agent
	}
	
	// Check if already started
	if r.ticker != nil {
		return // Already running
	}
	
	r.ticker = time.NewTicker(r.rotationSpeed)
	go r.rotationLoop()
}

// Stop halts the renomination rotation
func (r *MultipathRenominator) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	// Check if already stopped
	select {
	case <-r.done:
		// Already closed, nothing to do
		return
	default:
		// Not closed, proceed with stopping
		close(r.done)
		if r.ticker != nil {
			r.ticker.Stop()
			r.ticker = nil
		}
	}
}

// rotationLoop continuously renominates different pairs
func (r *MultipathRenominator) rotationLoop() {
	// Get ticker reference to avoid race with Stop()
	r.mu.RLock()
	ticker := r.ticker
	r.mu.RUnlock()
	
	if ticker == nil {
		return
	}
	
	for {
		select {
		case <-r.done:
			return
		case <-ticker.C:
			r.renominateNext()
		}
	}
}

// renominateNext moves to the next pair in rotation
func (r *MultipathRenominator) renominateNext() {
	r.mu.RLock()
	pairs := r.validPairs
	r.mu.RUnlock()
	
	if len(pairs) <= 1 {
		return // Need at least 2 pairs for multipath
	}
	
	r.currentIndex = (r.currentIndex + 1) % len(pairs)
	nextPair := pairs[r.currentIndex]
	
	// Send USE-CANDIDATE binding request to renominate this pair
	r.sendRenominationRequest(nextPair)
}

// AddValidPair adds a pair to the renomination rotation
func (r *MultipathRenominator) AddValidPair(pair *ice.CandidatePair) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	// Check if pair already exists
	for _, existing := range r.validPairs {
		if existing == pair {
			return
		}
	}
	
	r.validPairs = append(r.validPairs, pair)
}

// sendRenominationRequest triggers renomination for the given pair
func (r *MultipathRenominator) sendRenominationRequest(pair *ice.CandidatePair) {
	if r.agent == nil {
		return
	}
	
	// Use the new ice.Agent.RenominateCandidate API
	err := r.agent.RenominateCandidate(pair.Local, pair.Remote)
	if err != nil {
		// Log error but don't fail - renomination is best effort
		// The error could be logged here if we had access to logger
		return
	}
}

// EnableMultipath enables multipath mode for a PeerConnection
func (pc *PeerConnection) EnableMultipath() error {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	
	// Use pre-configured multipath state from SettingEngine if available
	if pc.multipathState == nil {
		if pc.api.settingEngine.multipathState != nil {
			// Use the configured state from SettingEngine
			pc.multipathState = pc.api.settingEngine.multipathState
		} else {
			// Create a new default state
			pc.multipathState = NewMultipathState()
		}
	}
	
	pc.multipathState.enabled = true
	
	// Initialize renominator if enabled and not already initialized
	if pc.multipathState.enableRenomination && pc.multipathState.renominator == nil {
		pc.multipathState.renominator = NewMultipathRenominator(pc.multipathState.renominationInterval)
		pc.multipathState.renominator.isControlling = true // Assume controlling for now
		
		// Connect renominator to ICE agent if available
		if pc.iceGatherer != nil && pc.iceGatherer.agent != nil {
			pc.multipathState.renominator.agent = pc.iceGatherer.agent
		}
		
		pc.log.Info("Multipath mode enabled with renomination strategy")
	} else if !pc.multipathState.enableRenomination {
		pc.log.Info("Multipath mode enabled with nomination prevention")
	} else {
		pc.log.Info("Multipath mode enabled (already configured)")
	}
	
	return nil
}

// DisableMultipath disables multipath mode
func (pc *PeerConnection) DisableMultipath() {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	
	if pc.multipathState != nil {
		pc.multipathState.enabled = false
		
		// Stop renomination if active
		if pc.multipathState.renominator != nil {
			pc.multipathState.renominator.Stop()
		}
	}
}

// IsMultipathEnabled returns whether multipath is enabled
func (pc *PeerConnection) IsMultipathEnabled() bool {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	
	return pc.multipathState != nil && pc.multipathState.enabled
}

// GetMultipathStats returns statistics for all active candidate pairs
func (pc *PeerConnection) GetMultipathStats() map[string]*CandidatePairStats {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	
	if pc.multipathState == nil {
		return nil
	}
	
	pc.multipathState.mu.RLock()
	defer pc.multipathState.mu.RUnlock()
	
	// Create a copy of the stats
	stats := make(map[string]*CandidatePairStats)
	for k, v := range pc.multipathState.pairStats {
		statsCopy := *v
		stats[k] = &statsCopy
	}
	
	return stats
}

// multipathBindingRequestHandler handles STUN binding requests for multipath with renomination
func multipathBindingRequestHandler(state *MultipathState) func(m *stun.Message, local, remote ice.Candidate, pair *ice.CandidatePair) bool {
	return func(m *stun.Message, local, remote ice.Candidate, pair *ice.CandidatePair) bool {
		if state == nil || m == nil {
			return true
		}
		
		// Only process STUN binding requests, not other message types
		if m.Type.Method != stun.MethodBinding || m.Type.Class != stun.ClassRequest {
			return true // Pass through non-binding requests unchanged
		}
		
		state.mu.Lock()
		defer state.mu.Unlock()
		
		// Track all pairs for stats and renomination
		if local != nil && remote != nil && pair != nil {
			pairKey := local.String() + "->" + remote.String()
			state.activePairs[pairKey] = pair
			
			// Initialize stats if needed
			if _, exists := state.pairStats[pairKey]; !exists {
				state.pairStats[pairKey] = &CandidatePairStats{
					LastActivity: time.Now(),
				}
			}
			
			stats := state.pairStats[pairKey]
			stats.LastActivity = time.Now()
			
			// Add to renomination rotation if pair is succeeded
			if state.enableRenomination && state.renominator != nil {
				// Check if this is a successful pair worth adding to rotation
				// We'll add it when it becomes a succeeded pair
				if pair != nil {
					state.renominator.AddValidPair(pair)
				}
			}
		}
		
		// Handle USE-CANDIDATE attribute (nomination/renomination)
		hasUseCandidate := false
		for _, attr := range m.Attributes {
			if attr.Type == 0x0025 { // USE-CANDIDATE attribute
				hasUseCandidate = true
				break
			}
		}
		
		if hasUseCandidate {
			if state.preventNomination {
				// Old behavior: prevent all nominations
				if local != nil && remote != nil {
					pairKey := local.String() + "->" + remote.String()
					if stats, exists := state.pairStats[pairKey]; exists {
						stats.NominationPrevented++
					}
				}
				return false
			} else if state.enableRenomination {
				// New behavior: allow nominations for renomination strategy
				if local != nil && remote != nil {
					pairKey := local.String() + "->" + remote.String()
					if stats, exists := state.pairStats[pairKey]; exists {
						stats.LastActivity = time.Now()
					}
				}
				
				// Start renomination rotation if we have multiple pairs
				if state.renominator != nil && len(state.renominator.validPairs) >= 2 {
					// Start the rotation if not already started
					go func() {
						if state.renominator.ticker == nil {
							state.renominator.Start()
						}
					}()
				}
				
				return true // Allow nomination/renomination
			}
		}
		
		// Allow non-nomination binding requests
		return true
	}
}

// ConfigureMultipathSettingEngine configures a SettingEngine for multipath operation
func ConfigureMultipathSettingEngine(e *SettingEngine, preventNomination bool) *MultipathState {
	state := NewMultipathState()
	state.preventNomination = preventNomination
	
	// If not preventing nomination, enable renomination strategy
	if !preventNomination {
		state.enableRenomination = true
		state.strategy = Conservative // Start with conservative strategy
	}
	
	// Set the binding request handler
	e.SetICEBindingRequestHandler(multipathBindingRequestHandler(state))
	
	// Configure ICE timeouts for multipath
	e.SetICETimeouts(
		10*time.Second, // disconnected timeout (longer to keep pairs alive)
		30*time.Second, // failed timeout
		2*time.Second,  // keepalive interval (frequent to maintain all pairs)
	)
	
	// Increase max binding requests to handle multiple active pairs
	e.SetICEMaxBindingRequests(30)
	
	return state
}

// ConfigureMultipathWithRenomination configures a SettingEngine specifically for renomination-based multipath
func ConfigureMultipathWithRenomination(e *SettingEngine, strategy MultipathStrategy, rotationSpeed time.Duration) *MultipathState {
	state := NewMultipathState()
	state.preventNomination = false // Allow nominations for renomination
	state.enableRenomination = true
	state.strategy = strategy
	state.renominationInterval = rotationSpeed
	
	// Set the binding request handler
	e.SetICEBindingRequestHandler(multipathBindingRequestHandler(state))
	
	// Configure ICE renomination with a simple incrementing nomination value generator
	nominationCounter := uint32(0)
	e.SetICERenomination(true, func() uint32 {
		nominationCounter++
		return nominationCounter
	})
	
	// Configure ICE timeouts optimized for fast renomination
	// Use reasonable defaults that don't interfere with normal operation
	e.SetICETimeouts(
		30*time.Second, // disconnected timeout - give plenty of time
		60*time.Second, // failed timeout
		2*time.Second,  // keepalive interval - standard interval
	)
	
	// Increase max binding requests for renomination
	e.SetICEMaxBindingRequests(50)
	
	// Store the configured state in the SettingEngine
	e.multipathState = state
	
	return state
}