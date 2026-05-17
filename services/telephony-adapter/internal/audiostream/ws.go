package audiostream

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
)

// wsAcceptKey computes the Sec-WebSocket-Accept value per RFC 6455.
func wsAcceptKey(key string) string {
	const magic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.New()
	h.Write([]byte(key + magic))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// readBinaryFrame reads one WebSocket frame and returns the payload if it is binary (opcode 0x2).
// FIN bit must be set (no fragmentation expected from FreeSWITCH mod_audio_stream).
func readBinaryFrame(r *bufio.Reader) ([]byte, error) {
	// Byte 0: FIN + opcode
	b0, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	opcode := b0 & 0x0f
	if opcode == 0x8 { // close frame
		return nil, io.EOF
	}
	if opcode != 0x2 { // not binary
		return nil, fmt.Errorf("audiostream: unexpected opcode %d", opcode)
	}

	// Byte 1: MASK + payload length
	b1, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	masked := (b1 & 0x80) != 0
	length := uint64(b1 & 0x7f)

	switch length {
	case 126:
		var l uint16
		if err := binary.Read(r, binary.BigEndian, &l); err != nil {
			return nil, err
		}
		length = uint64(l)
	case 127:
		if err := binary.Read(r, binary.BigEndian, &length); err != nil {
			return nil, err
		}
	}

	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(r, mask[:]); err != nil {
			return nil, err
		}
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return payload, nil
}

// WriteBinaryFrame writes a single unmasked binary WebSocket frame (server→client direction).
// Used by fake FreeSWITCH in tests to send audio to the server.
func WriteBinaryFrame(w io.Writer, data []byte) error {
	// FIN=1, opcode=0x2 (binary)
	header := []byte{0x82}
	length := len(data)
	switch {
	case length < 126:
		header = append(header, byte(length))
	case length < 65536:
		header = append(header, 126)
		header = append(header, byte(length>>8), byte(length))
	default:
		header = append(header, 127)
		for i := 7; i >= 0; i-- {
			header = append(header, byte(length>>(uint(i)*8)))
		}
	}
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}
