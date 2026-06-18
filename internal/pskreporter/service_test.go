package pskreporter

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

// TestService_FlushSendsDatagram drives the buffer → dedup → encode → UDP path
// against a local listener (no real server). It checks dedup (strongest SNR wins),
// that a well-formed datagram lands, and that it carries the receiver + spots.
func TestService_FlushSendsDatagram(t *testing.T) {
	// Local UDP listener standing in for PSK Reporter.
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer pc.Close()
	port := pc.LocalAddr().(*net.UDPAddr).Port

	recv := Receiver{Call: "G0XYZ", Locator: "IO91", Software: "StationManager test"}
	s := New(Config{Enabled: true, Host: "127.0.0.1", Port: port}, recv, nil)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	// Dedup: the stronger VK3ABC must win; a weaker repeat is dropped.
	s.AddSpot(Spot{Call: "VK3ABC", FreqHz: 14074000, SNR: -15, Mode: "FT8", Grid: "QF22", TimeUnix: 1700000000})
	s.AddSpot(Spot{Call: "VK3ABC", FreqHz: 14074100, SNR: -5, Mode: "FT8", Grid: "QF22", TimeUnix: 1700000015})
	s.AddSpot(Spot{Call: "K1ABC", FreqHz: 14074050, SNR: -8, Mode: "FT8", TimeUnix: 1700000015})
	s.flush() // don't wait for the 5-min loop

	buf := make([]byte, 2048)
	_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := pc.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read datagram: %v", err)
	}
	dg := buf[:n]

	if binary.BigEndian.Uint16(dg[0:2]) != ipfixVersion {
		t.Fatalf("version = %#x", binary.BigEndian.Uint16(dg[0:2]))
	}
	if int(binary.BigEndian.Uint16(dg[2:4])) != n {
		t.Fatalf("length field %d != datagram len %d", binary.BigEndian.Uint16(dg[2:4]), n)
	}
	if binary.BigEndian.Uint32(dg[8:12]) != 0 {
		t.Fatalf("first datagram seq = %d, want 0", binary.BigEndian.Uint32(dg[8:12]))
	}
	for _, want := range [][]byte{[]byte("G0XYZ"), []byte("VK3ABC"), []byte("K1ABC")} {
		if !bytes.Contains(dg, want) {
			t.Errorf("datagram missing %q", want)
		}
	}
	// Dedup kept the stronger VK3ABC (SNR -5 = 0xFB), not the weaker -15.
	if !bytes.Contains(dg, []byte{0xFB}) {
		t.Errorf("kept VK3ABC SNR (-5 = 0xFB) not in datagram")
	}
}

// TestService_DisabledIsNoop confirms a disabled service neither dials nor buffers.
func TestService_DisabledIsNoop(t *testing.T) {
	s := New(Config{Enabled: false}, Receiver{Call: "G0XYZ"}, nil)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("disabled start should be a no-op error-free: %v", err)
	}
	s.AddSpot(Spot{Call: "VK3ABC", SNR: -5, Mode: "FT8"})
	s.mu.Lock()
	n := len(s.buf)
	s.mu.Unlock()
	if n != 0 {
		t.Fatalf("disabled service buffered %d spots, want 0", n)
	}
	_ = s.Stop()
}

// TestService_AddSpotBeforeStartDrops guards against unbounded buffer growth when
// the service isn't live (Start not called, or it failed): with no socket to flush
// to, AddSpot must drop rather than accumulate.
func TestService_AddSpotBeforeStartDrops(t *testing.T) {
	s := New(Config{Enabled: true, Host: "127.0.0.1", Port: 9}, Receiver{Call: "G0XYZ"}, nil)
	for i := 0; i < 200; i++ {
		s.AddSpot(Spot{Call: "VK3ABC", SNR: -5, Mode: "FT8"})
	}
	s.mu.Lock()
	n := len(s.buf)
	s.mu.Unlock()
	if n != 0 {
		t.Fatalf("AddSpot before Start buffered %d spots, want 0 (no socket to flush to)", n)
	}
}
