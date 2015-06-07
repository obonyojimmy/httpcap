package raw_socket

import (
	"encoding/binary"
	_ "fmt"
	"net"
	"os"
	"strconv"
)

const (
	IP_HDRINCL = 2
)

// Capture traffic from socket using RAW_SOCKET's
// http://en.wikipedia.org/wiki/Raw_socket
//
// RAW_SOCKET allow you listen for traffic on any port (e.g. sniffing) because they operate on IP level.
// Ports is TCP feature, same as flow control, reliable transmission and etc.
// Since we can't use default TCP libraries RAWTCPLitener implements own TCP layer
// TCP packets is parsed using tcp_packet.go, and flow control is managed by tcp_message.go
type Listener struct {
	messages map[string]*TCPMessage // buffer of TCPMessages waiting to be send

	c_packets  chan *TCPPacket
	c_messages chan *TCPMessage // Messages ready to be send to client

	c_del_message chan *TCPMessage // Used for notifications about completed or expired messages

	addr string // IP to listen
	port int    // Port to listen
}

// RAWTCPListen creates a listener to capture traffic from RAW_SOCKET
func NewListener(addr string, port string) (rawListener *Listener) {
	rawListener = &Listener{}

	rawListener.c_packets = make(chan *TCPPacket, 100)
	rawListener.c_messages = make(chan *TCPMessage, 100)
	rawListener.c_del_message = make(chan *TCPMessage, 100)
	rawListener.messages = make(map[string]*TCPMessage)

	rawListener.addr = addr
	rawListener.port, _ = strconv.Atoi(port)

	go rawListener.listen()
	go rawListener.readRAWSocket()

	return
}

func (t *Listener) listen() {
	for {
		select {
		// If message ready for deletion it means that its also complete or expired by timeout
		case message := <-t.c_del_message:
			t.c_messages <- message
			delete(t.messages, message.ID)

		// We need to use channels to process each packet to avoid data races
		case packet := <-t.c_packets:
			t.processTCPPacket(packet)
		}
	}
}

func inet_ntoa(ipnr uint32) net.IP {
	var bytes [4]byte
	bytes[0] = byte(ipnr & 0xFF)
	bytes[1] = byte((ipnr >> 8) & 0xFF)
	bytes[2] = byte((ipnr >> 16) & 0xFF)
	bytes[3] = byte((ipnr >> 24) & 0xFF)

	return net.IPv4(bytes[3], bytes[2], bytes[1], bytes[0])
}

func stripIPv4Header(n int, b []byte) int {
	if len(b) < 20 {
		return n
	}
	l := int(b[0]&0x0f) << 2
	if 20 > l || l > len(b) {
		return n
	}
	if b[0]>>4 != 4 {
		return n
	}
	copy(b, b[l:])
	return n - l
}

func zoneToString(zone int) string {
	if zone == 0 {
		return ""
	}
	if ifi, err := net.InterfaceByIndex(zone); err == nil {
		return ifi.Name
	}

	return uitoa(uint(zone))
}

func uitoa(val uint) string {
	var buf [32]byte // big enough for int64
	i := len(buf) - 1
	for val >= 10 {
		buf[i] = byte(val%10 + '0')
		i--
		val /= 10
	}
	buf[i] = byte(val + '0')
	return string(buf[i:])
}

func getHostIP() string {
	host, _ := os.Hostname()
	addrs, _ := net.LookupIP(host)
	for _, addr := range addrs {
		if addr.To4() != nil && !addr.IsLoopback() {
			return addr.String()
		}
	}

	return "127.0.0.1"
}

func (t *Listener) parsePacket(addr net.Addr, src_ip string, dest_ip string, buf []byte) {
	if t.isIncomingDataPacket(buf) {
		new_buf := make([]byte, len(buf))
		copy(new_buf, buf)

		t.c_packets <- ParseTCPPacket(addr, src_ip, dest_ip, new_buf)
	}
}

func (t *Listener) isIncomingDataPacket(buf []byte) bool {
	// To avoid full packet parsing every time, we manually parsing values needed for packet filtering
	// http://en.wikipedia.org/wiki/Transmission_Control_Protocol
	src_port := binary.BigEndian.Uint16(buf[:2])
	dest_port := binary.BigEndian.Uint16(buf[2:4])

	if t.port <= 0 {
		// Get the 'data offset' (size of the TCP header in 32-bit words)
		dataOffset := (buf[12] & 0xF0) >> 4

		// We need only packets with data inside
		// Check that the buffer is larger than the size of the TCP header
		// SYN and  FIN  packets  and ACK-only packets not have data inside : (((ip[2:2] - ((ip[0]&0xf)<<2)) - ((tcp[12]&0xf0)>>2)) != 0)
		if len(buf) > int(dataOffset*4) && !t.isHeartbeatPackage(buf, dataOffset) {
			// We should create new buffer because go slices is pointers. So buffer data shoud be immutable.
			return true
		}

		return false
	}

	// Because RAW_SOCKET can't be bound to port, we have to control it by ourself
	if int(dest_port) == t.port || int(src_port) == t.port {
		// Get the 'data offset' (size of the TCP header in 32-bit words)
		dataOffset := (buf[12] & 0xF0) >> 4

		// We need only packets with data inside
		// Check that the buffer is larger than the size of the TCP header
		if len(buf) > int(dataOffset*4) && !t.isHeartbeatPackage(buf, dataOffset) {
			// We should create new buffer because go slices is pointers. So buffer data shoud be immutable.
			return true
		}
	}

	return false
}

func (t *Listener) isHeartbeatPackage(buf []byte, dataOffset byte) bool {
	return (len(buf)-int(dataOffset*4)) == 1 && buf[len(buf)-1] == 0
}

// Trying to add packet to existing message or creating new message
//
// For TCP message unique id is Acknowledgment number (see tcp_packet.go)
func (t *Listener) processTCPPacket(packet *TCPPacket) {
	defer func() { recover() }()

	var message *TCPMessage
	m_id := packet.Addr.String() + strconv.Itoa(int(packet.Ack))

	message, ok := t.messages[m_id]

	if !ok {
		// We sending c_del_message channel, so message object can communicate with Listener and notify it if message completed
		message = NewTCPMessage(m_id, t.c_del_message)
		t.messages[m_id] = message
	}

	// Adding packet to message
	message.c_packets <- packet
}

// Receive TCP messages from the listener channel
func (t *Listener) Receive() *TCPMessage {
	return <-t.c_messages
}
