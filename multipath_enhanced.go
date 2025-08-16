// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package webrtc

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/logging"
	"github.com/pion/rtp"
)

// MultipathICETransport extends ICETransport with multipath capabilities
type MultipathICETransport struct {
	*ICETransport
	
	// Multipath specific fields
	activePairs   map[string]*ice.CandidatePair
	pairConns     map[string]net.Conn  // Per-pair connections
	pairWeights   map[string]float64
	pairStats     map[string]*MultipathPairStats
	
	mu            sync.RWMutex
	config        *MultipathConfig
	logger        logging.LeveledLogger
	
	// Packet distribution
	roundRobinIndex int
	packetBuffer    chan *MultipathPacket
	
	ctx    context.Context
	cancel context.CancelFunc
}

// MultipathPairStats tracks per-pair statistics
type MultipathPairStats struct {
	PacketsSent     uint64
	PacketsReceived uint64
	BytesSent       uint64
	BytesReceived   uint64
	LastRTT         time.Duration
	LastActivity    time.Time
	Weight          float64
	Failures        uint64
}

// MultipathPacket represents a packet to be sent via multipath
type MultipathPacket struct {
	Data      []byte
	Timestamp time.Time
	SeqNum    uint16 // For RTP packets
}

// NewMultipathICETransport creates a new multipath-enabled ICE transport
func NewMultipathICETransport(iceTransport *ICETransport, config *MultipathConfig, logger logging.LeveledLogger) *MultipathICETransport {
	ctx, cancel := context.WithCancel(context.Background())
	
	mt := &MultipathICETransport{
		ICETransport: iceTransport,
		activePairs:  make(map[string]*ice.CandidatePair),
		pairConns:    make(map[string]net.Conn),
		pairWeights:  make(map[string]float64),
		pairStats:    make(map[string]*MultipathPairStats),
		config:       config,
		logger:       logger,
		packetBuffer: make(chan *MultipathPacket, 1000),
		ctx:          ctx,
		cancel:       cancel,
	}
	
	// Start packet distribution worker
	go mt.packetDistributionWorker()
	
	// Start statistics monitoring
	go mt.statisticsWorker()
	
	return mt
}

// AddCandidatePair adds a new candidate pair for multipath transmission
func (mt *MultipathICETransport) AddCandidatePair(pair *ice.CandidatePair) error {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	pairKey := fmt.Sprintf("%s->%s", pair.Local.String(), pair.Remote.String())
	
	if _, exists := mt.activePairs[pairKey]; exists {
		return nil // Already exists
	}
	
	mt.activePairs[pairKey] = pair
	mt.pairWeights[pairKey] = mt.config.InitialWeight
	mt.pairStats[pairKey] = &MultipathPairStats{
		LastActivity: time.Now(),
		Weight:       mt.config.InitialWeight,
	}
	
	mt.logger.Infof("Added multipath pair: %s", pairKey)
	return nil
}

// SendMultipathRTP sends an RTP packet through multiple paths
func (mt *MultipathICETransport) SendMultipathRTP(packet *rtp.Packet) error {
	data, err := packet.Marshal()
	if err != nil {
		return err
	}
	
	mpPacket := &MultipathPacket{
		Data:      data,
		Timestamp: time.Now(),
		SeqNum:    packet.SequenceNumber,
	}
	
	select {
	case mt.packetBuffer <- mpPacket:
		return nil
	case <-mt.ctx.Done():
		return fmt.Errorf("multipath transport closed")
	default:
		return fmt.Errorf("packet buffer full")
	}
}

// packetDistributionWorker distributes packets across multiple paths
func (mt *MultipathICETransport) packetDistributionWorker() {
	for {
		select {
		case packet := <-mt.packetBuffer:
			mt.distributePacket(packet)
		case <-mt.ctx.Done():
			return
		}
	}
}

// distributePacket sends a packet through multiple paths based on weights
func (mt *MultipathICETransport) distributePacket(packet *MultipathPacket) {
	mt.mu.RLock()
	
	// Get active pairs and their weights
	pairs := make([]string, 0, len(mt.activePairs))
	totalWeight := 0.0
	
	for pairKey := range mt.activePairs {
		pairs = append(pairs, pairKey)
		totalWeight += mt.pairWeights[pairKey]
	}
	
	mt.mu.RUnlock()
	
	if len(pairs) == 0 {
		mt.logger.Warn("No active pairs for multipath transmission")
		return
	}
	
	// Send through selected paths based on weight distribution
	sentPaths := 0
	for _, pairKey := range pairs {
		weight := mt.pairWeights[pairKey]
		
		// Probability of sending through this path
		probability := weight / totalWeight
		
		// Always send through high-weight paths (>0.5) or ensure at least one path gets the packet
		if probability > 0.5 || (sentPaths == 0 && pairKey == pairs[len(pairs)-1]) {
			if err := mt.sendThroughPair(pairKey, packet.Data); err != nil {
				mt.logger.Warnf("Failed to send through pair %s: %v", pairKey, err)
				mt.recordFailure(pairKey)
			} else {
				mt.recordSuccess(pairKey, len(packet.Data))
				sentPaths++
			}
		}
	}
	
	if sentPaths == 0 {
		mt.logger.Error("Failed to send packet through any path")
	}
}

// sendThroughPair sends data through a specific candidate pair
func (mt *MultipathICETransport) sendThroughPair(pairKey string, data []byte) error {
	mt.mu.RLock()
	pair, exists := mt.activePairs[pairKey]
	mt.mu.RUnlock()
	
	if !exists {
		return fmt.Errorf("pair %s not found", pairKey)
	}
	
	// For actual implementation, we would need to access the underlying
	// ice.Conn for this specific pair. This is a limitation of the current
	// pion/ice API which doesn't expose per-pair connections.
	// 
	// In a real implementation, this would require modifications to pion/ice
	// to expose individual pair connections or a method to send through specific pairs.
	
	// Placeholder implementation - in practice this would use the ice.Conn
	// associated with this specific candidate pair
	_ = pair
	_ = data
	
	mt.logger.Debugf("Sending %d bytes through pair %s", len(data), pairKey)
	
	// For demonstration, we'll simulate success
	// In real implementation: return pair.Write(data)
	return nil
}

// recordSuccess updates statistics for successful transmission
func (mt *MultipathICETransport) recordSuccess(pairKey string, bytes int) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	if stats, exists := mt.pairStats[pairKey]; exists {
		atomic.AddUint64(&stats.PacketsSent, 1)
		atomic.AddUint64(&stats.BytesSent, uint64(bytes))
		stats.LastActivity = time.Now()
	}
}

// recordFailure updates statistics for failed transmission
func (mt *MultipathICETransport) recordFailure(pairKey string) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	if stats, exists := mt.pairStats[pairKey]; exists {
		atomic.AddUint64(&stats.Failures, 1)
		// Reduce weight for failing paths
		if mt.pairWeights[pairKey] > mt.config.MinWeight {
			mt.pairWeights[pairKey] *= 0.8
			if mt.pairWeights[pairKey] < mt.config.MinWeight {
				mt.pairWeights[pairKey] = mt.config.MinWeight
			}
		}
	}
}

// statisticsWorker periodically updates path weights based on performance
func (mt *MultipathICETransport) statisticsWorker() {
	ticker := time.NewTicker(mt.config.WeightUpdateInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			mt.updateWeights()
		case <-mt.ctx.Done():
			return
		}
	}
}

// updateWeights adjusts path weights based on performance metrics
func (mt *MultipathICETransport) updateWeights() {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	now := time.Now()
	
	for pairKey, stats := range mt.pairStats {
		// Reduce weight for inactive paths
		if now.Sub(stats.LastActivity) > 5*time.Second {
			mt.pairWeights[pairKey] *= 0.9
			if mt.pairWeights[pairKey] < mt.config.MinWeight {
				mt.pairWeights[pairKey] = mt.config.MinWeight
			}
		}
		
		// Update weight based on failure rate
		totalPackets := atomic.LoadUint64(&stats.PacketsSent) + atomic.LoadUint64(&stats.Failures)
		if totalPackets > 0 {
			successRate := float64(atomic.LoadUint64(&stats.PacketsSent)) / float64(totalPackets)
			targetWeight := mt.config.InitialWeight * successRate
			
			// Exponential moving average
			mt.pairWeights[pairKey] = 0.7*mt.pairWeights[pairKey] + 0.3*targetWeight
			
			// Ensure bounds
			if mt.pairWeights[pairKey] < mt.config.MinWeight {
				mt.pairWeights[pairKey] = mt.config.MinWeight
			} else if mt.pairWeights[pairKey] > mt.config.MaxWeight {
				mt.pairWeights[pairKey] = mt.config.MaxWeight
			}
			
			stats.Weight = mt.pairWeights[pairKey]
		}
	}
}

// GetMultipathStats returns current multipath statistics
func (mt *MultipathICETransport) GetMultipathStats() map[string]*MultipathPairStats {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	
	stats := make(map[string]*MultipathPairStats)
	for key, stat := range mt.pairStats {
		// Create a copy
		statCopy := &MultipathPairStats{
			PacketsSent:     atomic.LoadUint64(&stat.PacketsSent),
			PacketsReceived: atomic.LoadUint64(&stat.PacketsReceived),
			BytesSent:       atomic.LoadUint64(&stat.BytesSent),
			BytesReceived:   atomic.LoadUint64(&stat.BytesReceived),
			LastRTT:         stat.LastRTT,
			LastActivity:    stat.LastActivity,
			Weight:          stat.Weight,
			Failures:        atomic.LoadUint64(&stat.Failures),
		}
		stats[key] = statCopy
	}
	
	return stats
}

// Close shuts down the multipath transport
func (mt *MultipathICETransport) Close() error {
	mt.cancel()
	close(mt.packetBuffer)
	return mt.ICETransport.Stop()
}