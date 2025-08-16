//go:build !js
// +build !js

package webrtc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSimpleVNetConnectivity(t *testing.T) {
	// Test basic virtual network connectivity first
	offerPC, answerPC, wan := debugVNetSetup(t)
	defer func() {
		assert.NoError(t, offerPC.Close())
		assert.NoError(t, answerPC.Close())
		assert.NoError(t, wan.Stop())
	}()
	
	// Enable multipath on offerer
	require.NoError(t, offerPC.EnableMultipath())
	assert.True(t, offerPC.IsMultipathEnabled())
	
	// Create a data channel to trigger ICE gathering
	dataChannel, err := offerPC.CreateDataChannel("test", nil)
	require.NoError(t, err)
	require.NotNil(t, dataChannel)
	
	// Track ICE connection states
	offerConnected := make(chan struct{}, 1)
	answerConnected := make(chan struct{}, 1)
	
	// Collect ICE candidates to verify they're being generated
	var offerCandidates []ICECandidate
	var answerCandidates []ICECandidate
	
	offerPC.OnICECandidate(func(candidate *ICECandidate) {
		if candidate != nil {
			offerCandidates = append(offerCandidates, *candidate)
			t.Logf("Offer candidate: %s", candidate.String())
			// Add to answerer
			answerPC.AddICECandidate(candidate.ToJSON())
		}
	})
	
	answerPC.OnICECandidate(func(candidate *ICECandidate) {
		if candidate != nil {
			answerCandidates = append(answerCandidates, *candidate)
			t.Logf("Answer candidate: %s", candidate.String())
			// Add to offerer
			offerPC.AddICECandidate(candidate.ToJSON())
		}
	})
	
	offerPC.OnICEConnectionStateChange(func(state ICEConnectionState) {
		t.Logf("Offer ICE state: %s", state.String())
		if state == ICEConnectionStateConnected {
			select {
			case offerConnected <- struct{}{}:
			default:
			}
		}
	})
	
	answerPC.OnICEConnectionStateChange(func(state ICEConnectionState) {
		t.Logf("Answer ICE state: %s", state.String())
		if state == ICEConnectionStateConnected {
			select {
			case answerConnected <- struct{}{}:
			default:
			}
		}
	})
	
	// Perform signaling
	offer, err := offerPC.CreateOffer(nil)
	require.NoError(t, err)
	
	t.Logf("Offer SDP: %s", offer.SDP)
	
	require.NoError(t, offerPC.SetLocalDescription(offer))
	
	// Wait a bit for ICE gathering to start
	time.Sleep(100 * time.Millisecond)
	
	require.NoError(t, answerPC.SetRemoteDescription(offer))
	
	answer, err := answerPC.CreateAnswer(nil)
	require.NoError(t, err)
	
	require.NoError(t, answerPC.SetLocalDescription(answer))
	require.NoError(t, offerPC.SetRemoteDescription(answer))
	
	// Wait for ICE to connect (with longer timeout for debugging)
	timeout := time.After(30 * time.Second)
	
	// Wait for at least one side to connect
	select {
	case <-offerConnected:
		t.Log("Offer side connected successfully!")
	case <-answerConnected:
		t.Log("Answer side connected successfully!")
	case <-timeout:
		t.Logf("ICE connection timeout. Offer candidates: %d, Answer candidates: %d", 
			len(offerCandidates), len(answerCandidates))
		
		// Print detailed candidate info for debugging
		for i, c := range offerCandidates {
			t.Logf("Offer candidate %d: %+v", i, c)
		}
		for i, c := range answerCandidates {
			t.Logf("Answer candidate %d: %+v", i, c)
		}
		
		t.Fatal("ICE connection failed - basic vnet setup issue")
	}
	
	// If we get here, basic connectivity works
	t.Log("Basic vnet connectivity successful!")
	
	// Now test multipath-specific functionality
	assert.True(t, offerPC.IsMultipathEnabled())
	stats := offerPC.GetMultipathStats()
	assert.NotNil(t, stats)
}