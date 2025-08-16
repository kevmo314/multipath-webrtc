//go:build !js
// +build !js

package webrtc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenominationMultipathSimple(t *testing.T) {
	// Simple test to verify renomination-based multipath works
	// without complex virtual network setup
	
	// Create peer connections with renomination-based multipath
	offerSettingEngine := SettingEngine{}
	multipathState := ConfigureMultipathWithRenomination(&offerSettingEngine, Conservative, 100*time.Millisecond)
	require.NotNil(t, multipathState)
	
	answerSettingEngine := SettingEngine{}
	answerSettingEngine.SetICETimeouts(time.Second, time.Second, time.Millisecond*200)
	
	// Create APIs and peer connections
	offerAPI := NewAPI(WithSettingEngine(offerSettingEngine))
	offerPC, err := offerAPI.NewPeerConnection(Configuration{})
	require.NoError(t, err)
	defer offerPC.Close()
	
	answerAPI := NewAPI(WithSettingEngine(answerSettingEngine))
	answerPC, err := answerAPI.NewPeerConnection(Configuration{})
	require.NoError(t, err)
	defer answerPC.Close()
	
	// Enable multipath on offerer
	require.NoError(t, offerPC.EnableMultipath())
	assert.True(t, offerPC.IsMultipathEnabled())
	assert.False(t, answerPC.IsMultipathEnabled())
	
	// Verify multipath state is configured for renomination
	assert.NotNil(t, offerPC.multipathState)
	assert.True(t, offerPC.multipathState.enableRenomination)
	assert.False(t, offerPC.multipathState.preventNomination)
	assert.Equal(t, Conservative, offerPC.multipathState.strategy)
	
	// Test that the renominator is initialized
	assert.NotNil(t, offerPC.multipathState.renominator)
}

func TestRenominationStrategyComparison(t *testing.T) {
	// Test different multipath strategies
	strategies := []struct {
		name     string
		strategy MultipathStrategy
		speed    time.Duration
	}{
		{"Conservative-Fast", Conservative, 50 * time.Millisecond},
		{"Conservative-Slow", Conservative, 500 * time.Millisecond},
		{"Aggressive-Fast", Aggressive, 50 * time.Millisecond},
		{"Adaptive-Medium", Adaptive, 200 * time.Millisecond},
	}
	
	for _, test := range strategies {
		t.Run(test.name, func(t *testing.T) {
			se := SettingEngine{}
			state := ConfigureMultipathWithRenomination(&se, test.strategy, test.speed)
			
			assert.Equal(t, test.strategy, state.strategy)
			assert.Equal(t, test.speed, state.renominationInterval)
			assert.True(t, state.enableRenomination)
			assert.False(t, state.preventNomination)
		})
	}
}

func TestMultipathRenominationWithDataChannel(t *testing.T) {
	// Test renomination-based multipath with data channel
	offerSettingEngine := SettingEngine{}
	ConfigureMultipathWithRenomination(&offerSettingEngine, Conservative, 100*time.Millisecond)
	
	answerSettingEngine := SettingEngine{}
	
	// Create peer connections
	offerPC, err := NewAPI(WithSettingEngine(offerSettingEngine)).NewPeerConnection(Configuration{})
	require.NoError(t, err)
	defer offerPC.Close()
	
	answerPC, err := NewAPI(WithSettingEngine(answerSettingEngine)).NewPeerConnection(Configuration{})
	require.NoError(t, err)
	defer answerPC.Close()
	
	// Enable multipath
	require.NoError(t, offerPC.EnableMultipath())
	
	// Create data channel
	dataChannel, err := offerPC.CreateDataChannel("test", nil)
	require.NoError(t, err)
	
	// Setup signaling (this would normally happen over network)
	offerPC.OnICECandidate(func(candidate *ICECandidate) {
		if candidate != nil {
			answerPC.AddICECandidate(candidate.ToJSON())
		}
	})
	
	answerPC.OnICECandidate(func(candidate *ICECandidate) {
		if candidate != nil {
			offerPC.AddICECandidate(candidate.ToJSON())
		}
	})
	
	// Verify multipath configuration doesn't break basic functionality
	offer, err := offerPC.CreateOffer(nil)
	require.NoError(t, err)
	
	require.NoError(t, offerPC.SetLocalDescription(offer))
	require.NoError(t, answerPC.SetRemoteDescription(offer))
	
	answer, err := answerPC.CreateAnswer(nil)
	require.NoError(t, err)
	
	require.NoError(t, answerPC.SetLocalDescription(answer))
	require.NoError(t, offerPC.SetRemoteDescription(answer))
	
	// Verify multipath is still enabled after signaling
	assert.True(t, offerPC.IsMultipathEnabled())
	
	// Check that data channel was created
	assert.NotNil(t, dataChannel)
}

func TestMultipathDisableRenomination(t *testing.T) {
	// Test disabling multipath properly stops renomination
	se := SettingEngine{}
	ConfigureMultipathWithRenomination(&se, Conservative, 100*time.Millisecond)
	
	pc, err := NewAPI(WithSettingEngine(se)).NewPeerConnection(Configuration{})
	require.NoError(t, err)
	defer pc.Close()
	
	// Enable multipath
	require.NoError(t, pc.EnableMultipath())
	assert.True(t, pc.IsMultipathEnabled())
	
	// Disable multipath
	pc.DisableMultipath()
	assert.False(t, pc.IsMultipathEnabled())
	
	// Verify renominator is stopped (ticker should be nil after stop)
	// Note: We can't directly access the ticker, but the Stop() method should be called
}