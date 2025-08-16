// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package webrtc

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/logging"
	"github.com/pion/rtp"
)

// MultipathConfig configures multipath WebRTC behavior
type MultipathConfig struct {
	// Enable multipath mode
	Enabled bool
	
	// WeightUpdateInterval controls how often candidate weights are updated
	WeightUpdateInterval time.Duration
	
	// InitialWeight is the starting weight for new candidate pairs
	InitialWeight float64
	
	// MinWeight is the minimum weight a candidate can have
	MinWeight float64
	
	// MaxWeight is the maximum weight a candidate can have  
	MaxWeight float64
}

// DefaultMultipathConfig returns a default multipath configuration
func DefaultMultipathConfig() *MultipathConfig {
	return &MultipathConfig{
		Enabled:              false,
		WeightUpdateInterval: 100 * time.Millisecond,
		InitialWeight:        1.0,
		MinWeight:            0.1,
		MaxWeight:            10.0,
	}
}

// CandidateWeight tracks the weight and statistics for a candidate pair
type CandidateWeight struct {
	Pair         *ice.CandidatePair
	Weight       float64
	PacketsSent  uint64
	BytesSent    uint64
	LastRTT      time.Duration
	LossRate     float64
	LastUsed     time.Time
	mu           sync.RWMutex
}

// updateStats updates the statistics for this candidate
func (cw *CandidateWeight) updateStats(rtt time.Duration, loss float64) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	
	cw.LastRTT = rtt
	cw.LossRate = loss
	cw.LastUsed = time.Now()
	
	// Adjust weight based on RTT and loss
	// Lower RTT and lower loss = higher weight
	if rtt > 0 {
		// Weight inversely proportional to RTT (in milliseconds)
		rttWeight := 100.0 / float64(rtt.Milliseconds())
		
		// Weight inversely proportional to loss rate
		lossWeight := 1.0 - loss
		
		// Combine weights with exponential moving average
		newWeight := rttWeight * lossWeight
		cw.Weight = 0.7*cw.Weight + 0.3*newWeight
	}
}

// MultipathSender handles sending packets across multiple candidate pairs
type MultipathSender struct {
	config     *MultipathConfig
	candidates map[string]*CandidateWeight
	agent      *ice.Agent
	log        logging.LeveledLogger
	
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	
	// Packet deduplication on receive
	seenPackets map[uint16]time.Time // RTP sequence number -> timestamp
	seenMu      sync.RWMutex
}

// NewMultipathSender creates a new multipath sender
func NewMultipathSender(config *MultipathConfig, agent *ice.Agent, log logging.LeveledLogger) *MultipathSender {
	ctx, cancel := context.WithCancel(context.Background())
	
	ms := &MultipathSender{
		config:      config,
		candidates:  make(map[string]*CandidateWeight),
		agent:       agent,
		log:         log,
		ctx:         ctx,
		cancel:      cancel,
		seenPackets: make(map[uint16]time.Time),
	}
	
	// Start weight update loop
	go ms.updateWeightsLoop()
	
	// Start cleanup loop for seen packets
	go ms.cleanupSeenPackets()
	
	return ms
}

// AddCandidatePair adds a new candidate pair for multipath transmission
func (ms *MultipathSender) AddCandidatePair(pair *ice.CandidatePair) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	key := getCandidatePairKey(pair)
	if key == "" {
		// Invalid pair, don't add
		return
	}
	
	if _, exists := ms.candidates[key]; !exists {
		ms.candidates[key] = &CandidateWeight{
			Pair:     pair,
			Weight:   ms.config.InitialWeight,
			LastUsed: time.Now(),
		}
		ms.log.Infof("Added candidate pair for multipath: %s", key)
	}
}

// RemoveCandidatePair removes a candidate pair from multipath transmission
func (ms *MultipathSender) RemoveCandidatePair(pair *ice.CandidatePair) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	key := getCandidatePairKey(pair)
	if key == "" {
		// Invalid pair, nothing to remove
		return
	}
	
	delete(ms.candidates, key)
	ms.log.Infof("Removed candidate pair from multipath: %s", key)
}

// SendRTP sends an RTP packet through multiple candidate pairs
func (ms *MultipathSender) SendRTP(packet *rtp.Packet) error {
	if !ms.config.Enabled {
		return errors.New("multipath not enabled")
	}
	
	ms.mu.RLock()
	candidates := make([]*CandidateWeight, 0, len(ms.candidates))
	totalWeight := 0.0
	
	for _, cw := range ms.candidates {
		candidates = append(candidates, cw)
		totalWeight += cw.Weight
	}
	ms.mu.RUnlock()
	
	if len(candidates) == 0 {
		return errors.New("no candidate pairs available")
	}
	
	// Marshal packet once
	data, err := packet.Marshal()
	if err != nil {
		return err
	}
	
	// Send through all candidates based on weight
	var lastErr error
	sentCount := 0
	
	for _, cw := range candidates {
		// Probability of sending through this candidate
		sendProbability := cw.Weight / totalWeight
		
		// Always send if probability > 0.5, or based on weighted random selection
		if sendProbability > 0.5 || (sentCount == 0 && cw == candidates[len(candidates)-1]) {
			if err := ms.sendThroughCandidate(cw, data); err != nil {
				lastErr = err
				ms.log.Warnf("Failed to send through candidate %s: %v", getCandidatePairKey(cw.Pair), err)
			} else {
				sentCount++
				atomic.AddUint64(&cw.PacketsSent, 1)
				atomic.AddUint64(&cw.BytesSent, uint64(len(data)))
			}
		}
	}
	
	if sentCount == 0 && lastErr != nil {
		return lastErr
	}
	
	return nil
}

// sendThroughCandidate sends data through a specific candidate pair
func (ms *MultipathSender) sendThroughCandidate(cw *CandidateWeight, data []byte) error {
	// This would need to be implemented based on how the ice.Agent exposes candidate pairs
	// For now, this is a placeholder
	return errors.New("not implemented: need ice.Agent API for sending through specific pair")
}

// IsPacketDuplicate checks if an RTP packet is a duplicate
func (ms *MultipathSender) IsPacketDuplicate(seq uint16) bool {
	ms.seenMu.RLock()
	_, seen := ms.seenPackets[seq]
	ms.seenMu.RUnlock()
	
	if !seen {
		ms.seenMu.Lock()
		ms.seenPackets[seq] = time.Now()
		ms.seenMu.Unlock()
	}
	
	return seen
}

// updateWeightsLoop periodically updates candidate weights
func (ms *MultipathSender) updateWeightsLoop() {
	ticker := time.NewTicker(ms.config.WeightUpdateInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ms.ctx.Done():
			return
		case <-ticker.C:
			ms.updateWeights()
		}
	}
}

// updateWeights updates weights based on candidate statistics
func (ms *MultipathSender) updateWeights() {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	for _, cw := range ms.candidates {
		// Get latest stats from ICE agent if available
		// This would need the ice.Agent to expose per-pair statistics
		
		// Ensure weight stays within bounds
		if cw.Weight < ms.config.MinWeight {
			cw.Weight = ms.config.MinWeight
		} else if cw.Weight > ms.config.MaxWeight {
			cw.Weight = ms.config.MaxWeight
		}
	}
}

// cleanupSeenPackets removes old entries from the deduplication map
func (ms *MultipathSender) cleanupSeenPackets() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ms.ctx.Done():
			return
		case <-ticker.C:
			ms.seenMu.Lock()
			now := time.Now()
			for seq, timestamp := range ms.seenPackets {
				if now.Sub(timestamp) > 10*time.Second {
					delete(ms.seenPackets, seq)
				}
			}
			ms.seenMu.Unlock()
		}
	}
}

// Close stops the multipath sender
func (ms *MultipathSender) Close() {
	ms.cancel()
}

// getCandidatePairKey creates a unique key for a candidate pair
func getCandidatePairKey(pair *ice.CandidatePair) string {
	if pair == nil || pair.Local == nil || pair.Remote == nil {
		return ""
	}
	return pair.Local.String() + "->" + pair.Remote.String()
}


// SetupMultipath configures a SettingEngine for multipath operation
func SetupMultipath(e *SettingEngine, config *MultipathConfig) {
	// Ensure keepalive is frequent enough to maintain all pairs
	e.SetICETimeouts(
		5*time.Second,  // disconnected timeout
		25*time.Second, // failed timeout  
		2*time.Second,  // keepalive interval
	)
}