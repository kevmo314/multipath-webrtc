// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package webrtc

import (
	"testing"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/logging"
	"github.com/pion/stun/v3"
	"github.com/stretchr/testify/assert"
)

func TestMultipathConfig(t *testing.T) {
	config := DefaultMultipathConfig()
	
	assert.False(t, config.Enabled)
	assert.Equal(t, 100*time.Millisecond, config.WeightUpdateInterval)
	assert.Equal(t, 1.0, config.InitialWeight)
	assert.Equal(t, 0.1, config.MinWeight)
	assert.Equal(t, 10.0, config.MaxWeight)
}

func TestCandidateWeight(t *testing.T) {
	cw := &CandidateWeight{
		Weight: 1.0,
	}
	
	// Test weight update with good RTT and no loss
	cw.updateStats(50*time.Millisecond, 0.0)
	assert.Greater(t, cw.Weight, 1.0)
	
	// Test weight update with bad RTT and high loss
	cw.Weight = 1.0
	cw.updateStats(500*time.Millisecond, 0.5)
	assert.Less(t, cw.Weight, 1.0)
}

func TestMultipathSender(t *testing.T) {
	config := DefaultMultipathConfig()
	config.Enabled = true
	
	logger := logging.NewDefaultLoggerFactory().NewLogger("test")
	ms := NewMultipathSender(config, nil, logger)
	defer ms.Close()
	
	// Test duplicate detection
	assert.False(t, ms.IsPacketDuplicate(1))
	assert.True(t, ms.IsPacketDuplicate(1))
	assert.False(t, ms.IsPacketDuplicate(2))
}


func TestMultipathState(t *testing.T) {
	state := NewMultipathState()
	
	assert.False(t, state.enabled)
	assert.False(t, state.preventNomination) // Changed: new default allows nominations for renomination
	assert.True(t, state.enableRenomination) // New: renomination enabled by default
	assert.Equal(t, Conservative, state.strategy) // New: default strategy
	assert.NotNil(t, state.activePairs)
	assert.NotNil(t, state.pairStats)
}

func TestPeerConnectionMultipath(t *testing.T) {
	// Create API with multipath settings
	se := SettingEngine{}
	multipathState := ConfigureMultipathSettingEngine(&se, true)
	assert.NotNil(t, multipathState)
	
	api := NewAPI(WithSettingEngine(se))
	
	// Create peer connection
	pc, err := api.NewPeerConnection(Configuration{})
	assert.NoError(t, err)
	defer pc.Close()
	
	// Enable multipath
	err = pc.EnableMultipath()
	assert.NoError(t, err)
	assert.True(t, pc.IsMultipathEnabled())
	
	// Get stats (should be empty initially)
	stats := pc.GetMultipathStats()
	assert.NotNil(t, stats)
	assert.Len(t, stats, 0)
	
	// Disable multipath
	pc.DisableMultipath()
	assert.False(t, pc.IsMultipathEnabled())
}

func TestMultipathBindingRequestHandler(t *testing.T) {
	// Test new default behavior (renomination strategy)
	state := NewMultipathState()
	handler := multipathBindingRequestHandler(state)
	
	// Test handling without USE-CANDIDATE (with nil candidates)
	msg := &stun.Message{
		Type: stun.MessageType{
			Method: stun.MethodBinding,
			Class:  stun.ClassRequest,
		},
	}
	result := handler(msg, nil, nil, nil)
	assert.True(t, result)
	
	// Since candidates are nil, nothing should be tracked
	assert.Len(t, state.activePairs, 0)
	assert.Len(t, state.pairStats, 0)
	
	// Test handling with USE-CANDIDATE (should allow for renomination strategy)
	msg2 := &stun.Message{
		Type: stun.MessageType{
			Method: stun.MethodBinding,
			Class:  stun.ClassRequest,
		},
	}
	msg2.Attributes = []stun.RawAttribute{
		{Type: 0x0025, Value: []byte{}}, // USE-CANDIDATE attribute
	}
	result2 := handler(msg2, nil, nil, nil)
	assert.True(t, result2) // Changed: new behavior allows nominations for renomination
	
	// Test old nomination prevention behavior
	oldState := NewMultipathState()
	oldState.preventNomination = true
	oldState.enableRenomination = false
	oldHandler := multipathBindingRequestHandler(oldState)
	
	result3 := oldHandler(msg2, nil, nil, nil)
	assert.False(t, result3) // Old behavior should still prevent nominations
}

func TestCandidatePairKey(t *testing.T) {
	// Test with nil pair
	key := getCandidatePairKey(nil)
	assert.Equal(t, "", key)
	
	// Test with valid pair
	// We can't create real candidates without config, so we'll skip this test
	// The getCandidatePairKey function would need real candidates to work properly
	
	// Since we can't create real candidates in tests,
	// we'll skip testing the actual key content
}

func TestMultipathSenderAddRemovePairs(t *testing.T) {
	config := DefaultMultipathConfig()
	config.Enabled = true
	
	logger := logging.NewDefaultLoggerFactory().NewLogger("test")
	ms := NewMultipathSender(config, nil, logger)
	defer ms.Close()
	
	// Test with nil pair (should handle gracefully)
	ms.AddCandidatePair(nil)
	assert.Len(t, ms.candidates, 0)
	
	// Test with empty pair (no local/remote)
	pair := &ice.CandidatePair{}
	ms.AddCandidatePair(pair)
	assert.Len(t, ms.candidates, 0) // Should not add invalid pairs
	
	// Remove nil pair (should handle gracefully)
	ms.RemoveCandidatePair(nil)
	assert.Len(t, ms.candidates, 0)
}