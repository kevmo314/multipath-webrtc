# WebRTC Multipath Example

This example demonstrates the multipath WebRTC implementation that sends packets through multiple ICE candidate pairs simultaneously without nominating a single pair.

## How It Works

1. **No ICE Nomination**: The implementation prevents ICE candidate nomination by intercepting STUN binding requests with the USE-CANDIDATE attribute and rejecting them.

2. **Multiple Active Pairs**: All valid candidate pairs remain active and are used for transmission.

3. **Weighted Distribution**: Packets are distributed across candidate pairs based on weights that are dynamically adjusted based on RTT and packet loss.

4. **Packet Deduplication**: The receiver deduplicates packets based on RTP sequence numbers since the same packet may arrive via multiple paths.

## Running the Example

### Terminal 1 (Offerer)
```bash
cd examples/multipath
go run main.go
```

1. The program will output an offer in JSON format
2. Copy this offer and paste it into Terminal 2
3. Wait for the answer from Terminal 2
4. Paste the answer and press Enter

### Terminal 2 (Answerer)
```bash
cd examples/multipath
go run answerer/main.go  # You'll need to create this or modify main.go
```

1. Paste the offer from Terminal 1
2. Copy the generated answer
3. Paste it back into Terminal 1

## What to Expect

Once connected, you should see:

1. **ICE Connection State Changes**: The connection progressing through various ICE states
2. **Multiple Candidate Pairs**: All discovered candidate pairs being maintained
3. **Multipath Statistics**: Every 5 seconds, statistics showing:
   - Active candidate pairs
   - Packets/bytes sent and received per pair
   - Round-trip time (RTT) per pair
   - Number of prevented nominations per pair
4. **Test Messages**: Data channel messages being sent every second

## Key Features Demonstrated

- **Nomination Prevention**: The binding request handler prevents ICE nomination
- **Keepalive Management**: Frequent STUN keepalives maintain all candidate pairs
- **Statistics Tracking**: Per-pair statistics for monitoring path quality
- **Dynamic Weighting**: Paths are weighted based on performance metrics

## Limitations and Considerations

1. **Bandwidth Multiplication**: Sending through all paths multiplies bandwidth usage
2. **Packet Ordering**: Different path latencies may cause significant packet reordering
3. **SRTP Considerations**: The current implementation needs proper handling of SRTP sequence numbers across multiple paths
4. **ICE Agent Integration**: Full integration requires modifications to the underlying pion/ice library for proper multi-path packet transmission

## Configuration Options

The multipath behavior can be configured through `MultipathConfig`:

```go
config := &webrtc.MultipathConfig{
    Enabled:              true,
    PreventNomination:    true,
    WeightUpdateInterval: 100 * time.Millisecond,
    InitialWeight:        1.0,
    MinWeight:            0.1,
    MaxWeight:            10.0,
}
```

## Testing Multipath Behavior

To verify multipath is working:

1. Run the example and establish a connection
2. Check the statistics output for multiple active pairs
3. Verify "Nominations Prevented" counter increases
4. Monitor that no single pair is selected/nominated
5. Observe packets being sent through multiple paths