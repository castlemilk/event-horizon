package lmac

import (
	"context"
	"encoding/binary"
	"sync"
	"time"
)

// Builder is the interface implemented by every LMAC request that the
// Submitter can serialise. Implementations live next to the message structs.
type Builder interface {
	Encode() ([]byte, error)
}

// BulkWriter writes one frame to the device's bulk OUT endpoint.
type BulkWriter interface {
	BulkOut(ctx context.Context, frame []byte) error
}

// AckSource delivers ACK message IDs from the firmware. The event loop feeds
// it; the Submitter drains it.
type AckSource interface {
	NextACK(ctx context.Context) (uint16, error)
}

// Submitter serialises host-target commands and waits for the matching
// firmware CFM (treated as the "ACK" for the REQ). One Submitter per device.
type Submitter struct {
	writer BulkWriter
	acks   AckSource

	mu      sync.Mutex
	nextSeq uint16
}

// NewSubmitter creates a Submitter bound to writer + acks.
func NewSubmitter(writer BulkWriter, acks AckSource) *Submitter {
	return &Submitter{writer: writer, acks: acks}
}

// Submit encodes msg, writes it to bulk OUT, and blocks until the firmware
// CFM arrives via acks or ctx is cancelled.
func (s *Submitter) Submit(ctx context.Context, msg Builder) error {
	frame, err := msg.Encode()
	if err != nil {
		return err
	}
	if len(frame) < HeaderSize {
		return &SubmitError{Kind: ErrShortFrame, MsgID: readID(frame)}
	}
	// Inject a host-unique sequence number into the SrcID slot (firmware echoes
	// it in the matching CFM; we ignore it and just match on the CFM base id).
	s.mu.Lock()
	s.nextSeq++
	seq := s.nextSeq
	s.mu.Unlock()
	binary.LittleEndian.PutUint16(frame[4:6], seq)
	if err := s.writer.BulkOut(ctx, frame); err != nil {
		return err
	}
	// Wait for ACK (a CFM is the firmware's "ack" for the REQ_CFM handshake).
	wantTask := readID(frame) & 0xFC00
	for {
		ackID, err := s.acks.NextACK(ctx)
		if err != nil {
			if err == context.DeadlineExceeded || err == context.Canceled {
				return &SubmitError{Kind: ErrTimeout, MsgID: readID(frame), Cause: err}
			}
			return &SubmitError{Kind: ErrChannelClosed, MsgID: readID(frame), Cause: err}
		}
		// Match by task id: a REQ in TASK_MM is ACKed by a CFM in TASK_MM
		// (REQ id N -> CFM id N+1, same task).
		if (ackID & 0xFC00) == wantTask {
			return nil
		}
		// Otherwise keep waiting (rare: spurious ACK from a prior submission).
	}
}

func readID(frame []byte) uint16 {
	if len(frame) < 2 {
		return 0
	}
	return binary.LittleEndian.Uint16(frame[0:2])
}

// DefaultACKTimeout is the per-submit wait. Callers wrap Submit with their
// own deadline.
const DefaultACKTimeout = 4 * time.Second

// AckChannel adapts a chan uint16 to AckSource.
type AckChannel chan uint16

// NextACK implements AckSource.
func (c AckChannel) NextACK(ctx context.Context) (uint16, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case id := <-c:
		return id, nil
	}
}
