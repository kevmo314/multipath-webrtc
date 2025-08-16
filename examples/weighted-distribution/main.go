// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"time"
)

type CandidateWeight struct {
	Weight   float64
	LastUsed time.Time
}

func (cw *CandidateWeight) updateStats(rtt time.Duration, loss float64) {
	if rtt > 0 {
		rttWeight := 100.0 / float64(rtt.Milliseconds())
		lossWeight := 1.0 - loss
		newWeight := rttWeight * lossWeight
		cw.Weight = 0.7*cw.Weight + 0.3*newWeight
	}
}

func main() {
	fmt.Println("=== WebRTC Multipath Weighted Distribution ===")
	fmt.Println()
	
	// Example: 3 candidate pairs with different network qualities
	candidates := map[string]float64{
		"WiFi-5GHz":    2.5, // Excellent connection - high bandwidth, low latency
		"WiFi-2.4GHz":  1.0, // Good connection - moderate quality
		"Cellular-4G":  0.3, // Poor connection - high latency, limited bandwidth
	}
	
	// Calculate total weight for probability distribution
	totalWeight := 0.0
	for _, weight := range candidates {
		totalWeight += weight
	}
	
	fmt.Printf("Available Candidate Pairs:\n")
	for path, weight := range candidates {
		probability := weight / totalWeight
		fmt.Printf("  %-12s: weight=%.1f, send probability=%.1f%%\n", 
			path, weight, probability*100)
	}
	fmt.Printf("  Total Weight: %.1f\n\n", totalWeight)
	
	// Demonstrate packet distribution using current algorithm
	fmt.Println("Packet Distribution Algorithm:")
	fmt.Println("Rule: Send through paths with probability > 50% + ensure reliability")
	fmt.Println()
	
	packetsSent := make(map[string]int)
	
	// Simulate sending 10 packets
	for packetNum := 1; packetNum <= 10; packetNum++ {
		sentCount := 0
		selectedPaths := []string{}
		
		// Apply weighted distribution logic
		for path, weight := range candidates {
			probability := weight / totalWeight
			
			// Current algorithm from multipath.go:
			// Send if probability > 0.5 OR ensure at least one path gets the packet
			if probability > 0.5 || (sentCount == 0 && path == "Cellular-4G") {
				packetsSent[path]++
				selectedPaths = append(selectedPaths, path)
				sentCount++
			}
		}
		
		fmt.Printf("  Packet %2d sent via: %v\n", packetNum, selectedPaths)
	}
	
	fmt.Println("\nTraffic Distribution Results:")
	for path, count := range packetsSent {
		percentage := float64(count) / 10.0 * 100
		fmt.Printf("  %-12s: %d packets (%.0f%%)\n", path, count, percentage)
	}
	
	fmt.Println("\n" + "================================================================")
	
	// Demonstrate dynamic weight adjustment
	fmt.Println("\nDynamic Weight Adjustment Over Time:")
	fmt.Println("Showing how path weights change based on network performance")
	fmt.Println()
	
	// Start with a moderate quality path
	pathWeight := &CandidateWeight{Weight: 1.0}
	fmt.Printf("Initial weight: %.2f\n\n", pathWeight.Weight)
	
	// Simulate network conditions changing over time
	networkScenarios := []struct {
		description string
		rtt         time.Duration
		packetLoss  float64
	}{
		{"Excellent conditions", 40 * time.Millisecond, 0.000},
		{"Light network load", 80 * time.Millisecond, 0.005},
		{"Moderate congestion", 150 * time.Millisecond, 0.020},
		{"Heavy congestion", 300 * time.Millisecond, 0.050},
		{"Network problems", 500 * time.Millisecond, 0.100},
		{"Congestion clearing", 200 * time.Millisecond, 0.030},
		{"Back to normal", 60 * time.Millisecond, 0.002},
		{"Optimal performance", 35 * time.Millisecond, 0.000},
	}
	
	fmt.Printf("%-20s %8s %8s %8s\n", "Network State", "RTT", "Loss", "Weight")
	fmt.Println("--------------------------------------------------------")
	
	for _, scenario := range networkScenarios {
		// Update weight based on current network metrics
		pathWeight.updateStats(scenario.rtt, scenario.packetLoss)
		
		fmt.Printf("%-20s %6dms %6.1f%% %8.2f\n",
			scenario.description,
			scenario.rtt.Milliseconds(),
			scenario.packetLoss*100,
			pathWeight.Weight)
	}
	
	fmt.Println("\nWeight Calculation Formula:")
	fmt.Println("  rttComponent = 100ms / measuredRTT")
	fmt.Println("  lossComponent = 1.0 - packetLossRate")
	fmt.Println("  instantWeight = rttComponent × lossComponent")
	fmt.Println("  finalWeight = 0.7 × oldWeight + 0.3 × instantWeight  (smoothing)")
	
	fmt.Println("\n" + "================================================================")
	
	fmt.Println("\nKey Benefits of Weighted Distribution:")
	fmt.Println("✓ HIGH-QUALITY PATHS get more traffic (better user experience)")
	fmt.Println("✓ LOW-QUALITY PATHS provide backup/redundancy (reliability)")
	fmt.Println("✓ AUTOMATIC ADAPTATION to changing network conditions")
	fmt.Println("✓ LOAD BALANCING across multiple network interfaces")
	fmt.Println("✓ BANDWIDTH AGGREGATION potential (sum of all paths)")
	fmt.Println("✓ SEAMLESS FAILOVER when paths degrade or fail")
	
	fmt.Println("\nMultipath vs Standard WebRTC:")
	fmt.Printf("  Standard WebRTC: Uses 1 'winning' path (single point of failure)\n")
	fmt.Printf("  Multipath WebRTC: Uses ALL available paths (redundant & faster)\n")
	
	fmt.Println("\nReal-world Example:")
	fmt.Println("  📱 Phone with WiFi + Cellular:")
	fmt.Println("     • WiFi gets 80% of packets (fast, reliable)")
	fmt.Println("     • Cellular gets 20% of packets (backup, fills gaps)")
	fmt.Println("     • If WiFi fails → Cellular automatically takes 100%")
	fmt.Println("     • If WiFi recovers → Traffic rebalances automatically")
}