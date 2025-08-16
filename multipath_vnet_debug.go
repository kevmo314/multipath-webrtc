//go:build !js
// +build !js

package webrtc

import (
	"testing"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v3/vnet"
	"github.com/stretchr/testify/require"
)

// debugVNetSetup creates a simpler virtual network for debugging
func debugVNetSetup(t *testing.T) (*PeerConnection, *PeerConnection, *vnet.Router) {
	t.Helper()
	
	// Create a WAN router
	wan, err := vnet.NewRouter(&vnet.RouterConfig{
		CIDR:          "1.2.3.0/24",
		LoggerFactory: logging.NewDefaultLoggerFactory(),
	})
	require.NoError(t, err)
	
	// Create network for offerer
	offerNet, err := vnet.NewNet(&vnet.NetConfig{
		StaticIPs: []string{"1.2.3.4"},
	})
	require.NoError(t, err)
	require.NoError(t, wan.AddNet(offerNet))
	
	// Create network for answerer  
	answerNet, err := vnet.NewNet(&vnet.NetConfig{
		StaticIPs: []string{"1.2.3.5"},
	})
	require.NoError(t, err)
	require.NoError(t, wan.AddNet(answerNet))
	
	// Start the WAN
	require.NoError(t, wan.Start())
	
	// Configure offerer with multipath
	offerSE := SettingEngine{}
	offerSE.SetNet(offerNet)
	offerSE.SetICETimeouts(30*time.Second, 60*time.Second, 1*time.Second)
	ConfigureMultipathWithRenomination(&offerSE, Conservative, 200*time.Millisecond)
	
	// Configure answerer normally
	answerSE := SettingEngine{}
	answerSE.SetNet(answerNet)
	answerSE.SetICETimeouts(30*time.Second, 60*time.Second, 1*time.Second)
	
	// Create peer connections
	offerAPI := NewAPI(WithSettingEngine(offerSE))
	offerPC, err := offerAPI.NewPeerConnection(Configuration{})
	require.NoError(t, err)
	
	answerAPI := NewAPI(WithSettingEngine(answerSE))
	answerPC, err := answerAPI.NewPeerConnection(Configuration{})
	require.NoError(t, err)
	
	return offerPC, answerPC, wan
}

// MultiNetworkInterface wraps multiple vnet.Net instances
type MultiNetworkInterface struct {
	networks []vnet.Net
	primary  vnet.Net
}

// NewMultiNetworkInterface creates a multi-network interface
func NewMultiNetworkInterface(networks ...vnet.Net) *MultiNetworkInterface {
	if len(networks) == 0 {
		return nil
	}
	return &MultiNetworkInterface{
		networks: networks,
		primary:  networks[0],
	}
}

// This would require modifying pion/transport/vnet to support multiple interfaces
// The current limitation is that SettingEngine.SetNet() only accepts one network interface