//go:build !js
// +build !js

package webrtc

import (
	"testing"
	"time"

	"github.com/pion/stun/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultipathRenominationStrategy(t *testing.T) {
	// Test the new renomination-based multipath strategy
	state := NewMultipathState()
	
	// Verify default configuration
	assert.False(t, state.preventNomination, "Should allow nominations for renomination strategy")
	assert.True(t, state.enableRenomination, "Should enable renomination by default")
	assert.Equal(t, Conservative, state.strategy, "Should start with conservative strategy")
	
	// Test handler behavior
	handler := multipathBindingRequestHandler(state)
	
	// Create a STUN message with USE-CANDIDATE
	msg := &stun.Message{}
	msg.Attributes = []stun.RawAttribute{
		{Type: 0x0025, Value: []byte{}}, // USE-CANDIDATE attribute
	}
	
	// Should allow nomination for renomination strategy
	result := handler(msg, nil, nil, nil)
	assert.True(t, result, "Should allow nomination for renomination strategy")
}

func TestMultipathRenominator(t *testing.T) {
	// Test the renomination controller
	renominator := NewMultipathRenominator(100 * time.Millisecond)
	require.NotNil(t, renominator)
	
	// Test that it doesn't start without being controlling
	renominator.isControlling = false
	renominator.Start()
	assert.Nil(t, renominator.ticker, "Should not start rotation when not controlling")
	
	// Test controlling agent behavior
	renominator.isControlling = true
	renominator.Start()
	
	// Clean up
	renominator.Stop()
}

func TestConfigureMultipathWithRenomination(t *testing.T) {
	// Test the new configuration function
	se := SettingEngine{}
	
	// Test conservative strategy with fast rotation
	state := ConfigureMultipathWithRenomination(&se, Conservative, 100*time.Millisecond)
	require.NotNil(t, state)
	
	assert.False(t, state.preventNomination, "Should allow nominations")
	assert.True(t, state.enableRenomination, "Should enable renomination")
	assert.Equal(t, Conservative, state.strategy, "Should set conservative strategy")
	assert.Equal(t, 100*time.Millisecond, state.renominationInterval, "Should set rotation speed")
}

func TestMultipathStrategies(t *testing.T) {
	// Test that all strategies are defined
	strategies := []MultipathStrategy{Conservative, Aggressive, Adaptive}
	
	for _, strategy := range strategies {
		se := SettingEngine{}
		state := ConfigureMultipathWithRenomination(&se, strategy, 200*time.Millisecond)
		assert.Equal(t, strategy, state.strategy, "Strategy should be set correctly")
	}
}

func TestRenominationCompatibility(t *testing.T) {
	// Test backward compatibility - old function should work with new strategy
	se := SettingEngine{}
	
	// Test old function with preventNomination = false (should enable renomination)
	state := ConfigureMultipathSettingEngine(&se, false)
	assert.False(t, state.preventNomination, "Should not prevent nominations")
	assert.True(t, state.enableRenomination, "Should enable renomination when not preventing")
	
	// Test old function with preventNomination = true (should use old behavior)
	state2 := ConfigureMultipathSettingEngine(&se, true)
	assert.True(t, state2.preventNomination, "Should prevent nominations when requested")
	assert.True(t, state2.enableRenomination, "Should still enable renomination by default")
}