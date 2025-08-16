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
		enabled:           false,
		preventNomination: true,
		activePairs:       make(map[string]*ice.CandidatePair),
		pairStats:         make(map[string]*CandidatePairStats),
	}
}

// EnableMultipath enables multipath mode for a PeerConnection
func (pc *PeerConnection) EnableMultipath() error {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	
	if pc.multipathState == nil {
		pc.multipathState = NewMultipathState()
	}
	
	pc.multipathState.enabled = true
	
	// Configure the ICE agent to prevent nomination
	if pc.multipathState.preventNomination {
		// This is already set via SettingEngine before PC creation
		pc.log.Info("Multipath mode enabled with nomination prevention")
	}
	
	return nil
}

// DisableMultipath disables multipath mode
func (pc *PeerConnection) DisableMultipath() {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	
	if pc.multipathState != nil {
		pc.multipathState.enabled = false
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

// multipathBindingRequestHandler handles STUN binding requests for multipath
func multipathBindingRequestHandler(state *MultipathState) func(m *stun.Message, local, remote ice.Candidate, pair *ice.CandidatePair) bool {
	return func(m *stun.Message, local, remote ice.Candidate, pair *ice.CandidatePair) bool {
		if state == nil {
			return true
		}
		
		state.mu.Lock()
		defer state.mu.Unlock()
		
		// Only track if we have valid candidates
		if local != nil && remote != nil {
			// Track the candidate pair
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
		}
		
		// Check for USE-CANDIDATE attribute (nomination)
		// USE-CANDIDATE has attribute type 0x0025
		for _, attr := range m.Attributes {
			if attr.Type == 0x0025 && state.preventNomination {
				// USE-CANDIDATE present - prevent nomination
				if local != nil && remote != nil {
					pairKey := local.String() + "->" + remote.String()
					if stats, exists := state.pairStats[pairKey]; exists {
						stats.NominationPrevented++
					}
				}
				return false
			}
		}
		
		// Allow the binding request to proceed
		return true
	}
}

// ConfigureMultipathSettingEngine configures a SettingEngine for multipath operation
func ConfigureMultipathSettingEngine(e *SettingEngine, preventNomination bool) *MultipathState {
	state := NewMultipathState()
	state.preventNomination = preventNomination
	
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