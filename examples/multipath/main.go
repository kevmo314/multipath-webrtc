// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"
)

func main() {
	// Create a SettingEngine with multipath configuration
	settingEngine := webrtc.SettingEngine{}
	
	// Configure for multipath operation
	multipathState := webrtc.ConfigureMultipathSettingEngine(&settingEngine, true)
	fmt.Println("Configured WebRTC for multipath operation:")
	fmt.Println("- ICE nomination prevention: enabled")
	fmt.Println("- Keepalive interval: 2 seconds")
	fmt.Println("- Max binding requests: 30")
	
	// Create an API with the configured setting engine
	api := webrtc.NewAPI(webrtc.WithSettingEngine(settingEngine))
	
	// Create a new PeerConnection
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}
	
	peerConnection, err := api.NewPeerConnection(config)
	if err != nil {
		panic(err)
	}
	defer peerConnection.Close()
	
	// Enable multipath mode
	if err := peerConnection.EnableMultipath(); err != nil {
		panic(err)
	}
	fmt.Println("\nMultipath mode enabled on PeerConnection")
	
	// Set up ICE connection state handler
	peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		fmt.Printf("ICE Connection State changed: %s\n", state.String())
		
		if state == webrtc.ICEConnectionStateConnected {
			// Start monitoring multipath statistics
			go monitorMultipathStats(peerConnection, multipathState)
		}
	})
	
	// Set up ICE candidate handler
	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate != nil {
			fmt.Printf("New ICE candidate: %s\n", candidate.String())
		}
	})
	
	// Create a data channel for testing
	dataChannel, err := peerConnection.CreateDataChannel("multipath-test", nil)
	if err != nil {
		panic(err)
	}
	
	dataChannel.OnOpen(func() {
		fmt.Println("\nData channel opened!")
		
		// Send test messages periodically
		go func() {
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			
			messageNum := 0
			for range ticker.C {
				messageNum++
				msg := fmt.Sprintf("Multipath test message #%d", messageNum)
				if err := dataChannel.SendText(msg); err != nil {
					fmt.Printf("Error sending message: %v\n", err)
					return
				}
				fmt.Printf("Sent: %s\n", msg)
			}
		}()
	})
	
	dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
		fmt.Printf("Received: %s\n", string(msg.Data))
	})
	
	// Create an offer
	offer, err := peerConnection.CreateOffer(nil)
	if err != nil {
		panic(err)
	}
	
	// Set the local description
	if err := peerConnection.SetLocalDescription(offer); err != nil {
		panic(err)
	}
	
	// Output the offer in base64 so it can be pasted into the other side
	offerJSON, err := json.Marshal(offer)
	if err != nil {
		panic(err)
	}
	
	fmt.Println("\n=== Copy and paste this offer to the remote peer ===")
	fmt.Println(string(offerJSON))
	fmt.Println("=====================================================")
	
	// Wait for answer
	fmt.Println("Paste the answer from the remote peer and press Enter:")
	reader := bufio.NewReader(os.Stdin)
	answerText, err := reader.ReadString('\n')
	if err != nil {
		panic(err)
	}
	
	answerText = strings.TrimSpace(answerText)
	
	var answer webrtc.SessionDescription
	if err := json.Unmarshal([]byte(answerText), &answer); err != nil {
		panic(err)
	}
	
	// Set the remote description
	if err := peerConnection.SetRemoteDescription(answer); err != nil {
		panic(err)
	}
	
	fmt.Println("\nRemote description set successfully!")
	fmt.Println("Waiting for ICE connection...")
	// Block forever
	select {}
}

// monitorMultipathStats periodically displays multipath statistics
func monitorMultipathStats(pc *webrtc.PeerConnection, state *webrtc.MultipathState) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	for range ticker.C {
		stats := pc.GetMultipathStats()
		
		if len(stats) == 0 {
			fmt.Println("\n=== No active candidate pairs ===")
			continue
		}
		
		fmt.Println("\n=== Multipath Statistics ===")
		for pairKey, pairStats := range stats {
			fmt.Printf("Candidate Pair: %s\n", pairKey)
			fmt.Printf("  Packets Sent: %d\n", pairStats.PacketsSent)
			fmt.Printf("  Packets Recv: %d\n", pairStats.PacketsRecv)
			fmt.Printf("  Bytes Sent: %d\n", pairStats.BytesSent)
			fmt.Printf("  Bytes Recv: %d\n", pairStats.BytesRecv)
			fmt.Printf("  Last Activity: %v ago\n", time.Since(pairStats.LastActivity).Round(time.Second))
			fmt.Printf("  RTT: %v\n", pairStats.RTT)
			fmt.Printf("  Nominations Prevented: %d\n", pairStats.NominationPrevented)
		}
		fmt.Println("============================")
	}
}