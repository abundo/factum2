package storage

import (
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"
)

const (
	tftpRRQ   = 1
	tftpData  = 3
	tftpAck   = 4
	tftpError = 5
	tftpBlock = 512
)

// ServeTFTP answers read-only RRQs from repo until ln is closed.
func ServeTFTP(ln net.PacketConn, repo *Repo) error {
	buf := make([]byte, 2048)
	for {
		n, addr, err := ln.ReadFrom(buf)
		if err != nil {
			if strings.Contains(err.Error(), "use of closed") {
				return nil
			}
			return err
		}
		if n < 4 {
			continue
		}
		op := binary.BigEndian.Uint16(buf[:2])
		if op != tftpRRQ {
			_ = sendTFTPError(ln, addr, 4, "only RRQ is supported")
			continue
		}
		name, mode, err := parseRRQ(buf[2:n])
		if err != nil {
			_ = sendTFTPError(ln, addr, 0, err.Error())
			continue
		}
		if !strings.EqualFold(mode, "octet") && !strings.EqualFold(mode, "netascii") {
			_ = sendTFTPError(ln, addr, 0, "mode not supported")
			continue
		}
		go serveTFTPFile(ln.LocalAddr(), addr, repo, name)
	}
}

func parseRRQ(p []byte) (name, mode string, err error) {
	parts := strings.Split(string(p), "\x00")
	if len(parts) < 2 || parts[0] == "" {
		return "", "", fmt.Errorf("malformed RRQ")
	}
	return parts[0], parts[1], nil
}

func serveTFTPFile(local net.Addr, peer net.Addr, repo *Repo, name string) {
	udpAddr, ok := peer.(*net.UDPAddr)
	if !ok {
		return
	}
	laddr, _ := local.(*net.UDPAddr)
	var listenIP net.IP
	if laddr != nil {
		listenIP = laddr.IP
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: listenIP, Port: 0})
	if err != nil {
		slog.Error("storage tftp: listen", "err", err)
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Minute))

	f, _, err := repo.Open("/" + name)
	if err != nil {
		_ = sendTFTPError(conn, udpAddr, 1, "file not found")
		return
	}
	defer f.Close()

	block := uint16(1)
	data := make([]byte, tftpBlock)
	for {
		n, err := io.ReadFull(f, data)
		last := err == io.EOF || err == io.ErrUnexpectedEOF
		if err != nil && !last {
			_ = sendTFTPError(conn, udpAddr, 0, err.Error())
			return
		}
		pkt := make([]byte, 4+n)
		binary.BigEndian.PutUint16(pkt[0:2], tftpData)
		binary.BigEndian.PutUint16(pkt[2:4], block)
		copy(pkt[4:], data[:n])
		if err := sendTFTPData(conn, udpAddr, pkt); err != nil {
			slog.Debug("storage tftp: send", "err", err, "peer", udpAddr)
			return
		}
		if last {
			return
		}
		block++
	}
}

func sendTFTPData(conn *net.UDPConn, addr *net.UDPAddr, pkt []byte) error {
	ack := make([]byte, 4)
	want := binary.BigEndian.Uint16(pkt[2:4])
	for attempt := 0; attempt < 5; attempt++ {
		if _, err := conn.WriteToUDP(pkt, addr); err != nil {
			return err
		}
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, _, err := conn.ReadFromUDP(ack)
		if err != nil {
			continue
		}
		if n >= 4 && binary.BigEndian.Uint16(ack[0:2]) == tftpAck && binary.BigEndian.Uint16(ack[2:4]) == want {
			return nil
		}
	}
	return fmt.Errorf("tftp ack timeout block %d", want)
}

func sendTFTPError(w net.PacketConn, addr net.Addr, code uint16, msg string) error {
	pkt := make([]byte, 5+len(msg))
	binary.BigEndian.PutUint16(pkt[0:2], tftpError)
	binary.BigEndian.PutUint16(pkt[2:4], code)
	copy(pkt[4:], msg)
	_, err := w.WriteTo(pkt, addr)
	return err
}
