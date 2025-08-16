# Pion WebRTC - Multipath Edition

A pure Go implementation of WebRTC with multipath support. This fork extends [Pion WebRTC](https://github.com/pion/webrtc) to maintain and utilize multiple ICE candidate pairs simultaneously instead of selecting a single "winning" path.

## Motivation

Standard WebRTC implementations use ICE to discover candidate pairs and nominate the best one for communication. This approach has limitations:

- **Single point of failure**: Connection loss when the selected path fails
- **Underutilized resources**: Multiple network interfaces available but only one used
- **Poor handoff**: Network transitions cause connection interruptions
- **Limited bandwidth**: Cannot aggregate bandwidth across multiple paths

This implementation addresses these limitations by preventing ICE nomination and distributing traffic across all available candidate pairs.

## Implementation

### Core Components

- **`multipath.go`** - Core multipath functionality with weighted packet distribution
- **`multipath_integration.go`** - PeerConnection integration and state management  
- **`multipath_enhanced.go`** - Advanced multipath transport with per-pair statistics

### Key Changes

1. **ICE Nomination Prevention**: Intercepts STUN binding requests with USE-CANDIDATE attribute and rejects them
2. **Weighted Distribution**: Distributes packets across candidate pairs based on performance metrics
3. **Dynamic Weight Adjustment**: Updates path weights based on RTT and packet loss measurements
4. **Packet Deduplication**: Handles duplicate packets arriving via different paths
5. **Extended PeerConnection API**: New methods for multipath control and monitoring

### Algorithms

#### Nomination Prevention
```go
func preventNominationHandler(m *stun.Message, local, remote ice.Candidate, pair *ice.CandidatePair) bool {
    for _, attr := range m.Attributes {
        if attr.Type == 0x0025 { // USE-CANDIDATE attribute
            return false
        }
    }
    return true
}
```

#### Weighted Distribution
```go
for _, candidatePair := range activePairs {
    probability := pathWeight / totalWeight
    if probability > 0.5 || (sentCount == 0 && isBackupPath) {
        sendPacket(candidatePair, packet)
    }
}
```

#### Dynamic Weight Calculation
```go
rttWeight := 100.0 / float64(rtt.Milliseconds())
lossWeight := 1.0 - packetLossRate
newWeight := rttWeight * lossWeight
finalWeight := 0.7*oldWeight + 0.3*newWeight
```

## Usage

```go
// Configure multipath
settingEngine := webrtc.SettingEngine{}
webrtc.ConfigureMultipathSettingEngine(&settingEngine, true)

api := webrtc.NewAPI(webrtc.WithSettingEngine(settingEngine))
pc, _ := api.NewPeerConnection(webrtc.Configuration{})

// Enable multipath
pc.EnableMultipath()

// Monitor statistics
stats := pc.GetMultipathStats()
for pairKey, pairStats := range stats {
    fmt.Printf("Path %s: %d packets sent, RTT: %v\n", 
        pairKey, pairStats.PacketsSent, pairStats.LastRTT)
}
```

## Configuration

```go
type MultipathConfig struct {
    Enabled              bool
    PreventNomination    bool
    WeightUpdateInterval time.Duration
    InitialWeight        float64
    MinWeight            float64
    MaxWeight            float64
}
```

## Testing

The implementation includes comprehensive tests that validate multipath functionality:

```bash
# Run multipath tests
go test -v -run "TestMultipathPacketDistribution|TestNominationPrevention|TestMultipathWeightAdjustment"

# Run examples
cd examples/weighted-distribution && go run main.go
cd examples/multipath && go run main.go
```

Tests demonstrate:
- Packet distribution across multiple paths
- ICE nomination prevention
- Dynamic weight adjustment based on network conditions
- Packet deduplication
- Configuration and API functionality

## Examples

- **[Multipath Demo](examples/multipath/)** - Complete example with real-time statistics
- **[Weighted Distribution](examples/weighted-distribution/)** - Demonstrates packet distribution algorithms

## Limitations

- Requires modifications to underlying pion/ice library for optimal packet transmission
- SRTP sequence number handling needs consideration for multiple paths
- Bandwidth usage multiplies with number of active paths
- Integration tests require virtual network setup for full validation

## License

MIT License (same as upstream Pion WebRTC)

## Upstream

Based on [github.com/pion/webrtc](https://github.com/pion/webrtc)