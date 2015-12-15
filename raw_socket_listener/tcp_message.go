package raw_socket

import (
	"sort"
	"time"
)

const MSG_EXPIRE = 2000 * time.Millisecond

// TCPMessage ensure that all TCP packets for given request is received, and processed in right sequence
// Its needed because all TCP message can be fragmented or re-transmitted
//
// Each TCP Packet have 2 ids: acknowledgment - message_id, and sequence - packet_id
// Message can be compiled from unique packets with same message_id which sorted by sequence
// Message is received if we didn't receive any packets for 2000ms
type TCPMessage struct {
	ID      string // Message ID
	packets []*TCPPacket

	timer *time.Timer // Used for expire check

	c_packets chan *TCPPacket

	c_del_message chan *TCPMessage
}

// NewTCPMessage pointer created from a Acknowledgment number and a channel of messages readuy to be deleted
func NewTCPMessage(ID string, c_del chan *TCPMessage) (msg *TCPMessage) {
	msg = &TCPMessage{ID: ID}

	msg.c_packets = make(chan *TCPPacket)
	msg.c_del_message = c_del // used for notifying that message completed or expired

	// Every time we receive packet we reset this timer
	msg.timer = time.AfterFunc(MSG_EXPIRE, msg.Timeout)

	go msg.listen()

	return
}

func (t *TCPMessage) listen() {
	for {
		select {
		case packet, more := <-t.c_packets:
			if more {
				t.AddPacket(packet)
			} else {
				// Stop loop if channel closed
				return
			}
		}
	}
}

// Timeout notifies message to stop listening, close channel and message ready to be sent
func (t *TCPMessage) Timeout() {
	select {
	// In some cases Timeout can be called multiple times (do not know how yet)
	// Ensure that we did not close channel 2 times
	case packet, ok := <-t.c_packets:
		if ok {
			t.AddPacket(packet)
		} else {
			return
		}
	default:
		close(t.c_packets)
		t.c_del_message <- t // Notify RAWListener that message is ready to be send to replay server
	}
}

// Bytes sorts packets in right orders and return message content
func (t *TCPMessage) Bytes() (output []byte) {
	sort.Sort(BySeq(t.packets))

	for _, v := range t.packets {
		output = append(output, v.tcp.Payload...)
	}

	return
}

func (t *TCPMessage) SourcePort() uint16 {
	if len(t.packets) > 0 {
		return uint16(t.packets[0].tcp.SrcPort)
	} else {
		return 0
	}
}

func (t *TCPMessage) DestinationPort() uint16 {
	if len(t.packets) > 0 {
		return uint16(t.packets[0].tcp.DstPort)
	} else {
		return 0
	}
}

func (t *TCPMessage) SourceIP() string {
	if len(t.packets) > 0 {
		return t.packets[0].SrcIP
	} else {
		return "0.0.0.0"
	}
}

func (t *TCPMessage) DestinationIP() string {
	if len(t.packets) > 0 {
		return t.packets[0].DestIP
	} else {
		return "0.0.0.0"
	}
}

func (t *TCPMessage) SequenceNumber() uint32 {
	sort.Sort(BySeq(t.packets))

	if len(t.packets) > 0 {
		return t.packets[0].tcp.Seq
	} else {
		return 0
	}
}

// AddPacket to the message and ensure packet uniqueness
// TCP allows that packet can be re-send multiple times
func (t *TCPMessage) AddPacket(packet *TCPPacket) {
	packetFound := false

	for _, pkt := range t.packets {
		if packet.tcp.Seq == pkt.tcp.Seq {
			packetFound = true
			break
		}
	}

	if packetFound {
		//log.Println("Received packet with same sequence")
	} else {
		t.packets = append(t.packets, packet)
	}

	// Reset message timeout timer
	t.timer.Reset(MSG_EXPIRE)
}
