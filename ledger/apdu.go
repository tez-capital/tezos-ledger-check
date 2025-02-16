package ledger

import (
	"encoding/binary"
	"fmt"

	"github.com/karalabe/hid"
)

type APDUResult int

const (
	packetLength = 64
	channel      = 0x0101
	tagAPDU      = 0x05

	APDU_OK                   APDUResult = 0x9000
	APDU_WRONG_PARAM          APDUResult = 0x6b00
	APDU_WRONG_LENGTH         APDUResult = 0x6c00
	APDU_INVALID_INS          APDUResult = 0x6d00
	APDU_WRONG_LENGTH_FOR_INS APDUResult = 0x917e
	APDU_REJECT               APDUResult = 0x6985
	APDU_PARSE_ERROR          APDUResult = 0x9405
)

func (r APDUResult) Error() error {
	switch r {
	case APDU_OK:
		return nil
	case APDU_WRONG_PARAM:
		return fmt.Errorf("ledger response - wrong parameter")
	case APDU_WRONG_LENGTH:
		return fmt.Errorf("ledger response - wrong length")
	case APDU_INVALID_INS:
		return fmt.Errorf("ledger response - invalid instruction")
	case APDU_WRONG_LENGTH_FOR_INS:
		return fmt.Errorf("ledger response - wrong length for instruction")
	case APDU_REJECT:
		return fmt.Errorf("ledger response - rejected")
	case APDU_PARSE_ERROR:
		return fmt.Errorf("ledger response - parse error")
	default:
		return fmt.Errorf("ledger response - unknown error - 0x%x", r)
	}
}

func writeAPDU(device *hid.Device, apdu []byte) error {
	apduLen := len(apdu)

	if apduLen+7 > packetLength {
		return fmt.Errorf("APDU too long: %d bytes, max %d bytes", apduLen, packetLength-7)
	}

	// --- First Packet ---
	// in our case only one packet is used
	// The first packet header is 7 bytes:
	// [0-1]   : channel (2 bytes, big-endian)
	// [2]     : tag (1 byte, APDU tag)
	// [3-4]   : sequence number (2 bytes, big-endian; 0 for the first packet)
	// [5-6]   : total APDU length (2 bytes, big-endian)
	// [7-]    : APDU data (up to packetLength-7 bytes)
	//packetLength := len(apdu) + 7
	packet := make([]byte, packetLength)

	// Write channel (2 bytes, big-endian)
	binary.BigEndian.PutUint16(packet[0:2], uint16(channel))
	// Write tag
	packet[2] = tagAPDU
	// Write sequence number = 0 (2 bytes)
	binary.BigEndian.PutUint16(packet[3:5], 0)
	// Write total APDU length (2 bytes, big-endian)
	binary.BigEndian.PutUint16(packet[5:7], uint16(apduLen))

	// Calculate how many data bytes can be sent in the first packet.
	nDataFirst := apduLen
	if nDataFirst > packetLength-7 {
		nDataFirst = packetLength - 7
	}
	// Copy data into the packet.
	copy(packet[7:7+nDataFirst], apdu[:nDataFirst])

	// Send the first packet.
	if n, err := device.Write(packet); err != nil {
		return fmt.Errorf("first packet write error: %w", err)
	} else if n != packetLength {
		return fmt.Errorf("first packet incomplete write: wrote %d of %d bytes", n, packetLength)
	}

	return nil
}

func readAPDU(device *hid.Device) (sw int, payload []byte, err error) {
	var (
		expectedSeq uint16 = 0
		fullPayload []byte
		totalLen    int
	)

	// Read the first packet.
	firstPacket := make([]byte, packetLength)
	n, err := device.Read(firstPacket)
	if err != nil {
		return 0, nil, fmt.Errorf("read first packet: %w", err)
	}

	if n != packetLength {
		return 0, nil, fmt.Errorf("first packet incomplete: got %d of %d bytes", n, packetLength)
	}

	// --- Parse First Packet Header (7 bytes) ---
	// Bytes 0-1: channel (we could check if needed)
	// Byte 2: tag
	if firstPacket[2] != tagAPDU {
		return 0, nil, fmt.Errorf("unexpected tag in first packet: got 0x%x, expected 0x%x", firstPacket[2], tagAPDU)
	}
	// Bytes 3-4: sequence number (should be 0)
	seq := binary.BigEndian.Uint16(firstPacket[3:5])
	if seq != 0 {
		return 0, nil, fmt.Errorf("expected sequence 0 in first packet, got %d", seq)
	}
	// Bytes 5-6: total APDU length (big-endian)
	totalLen = int(binary.BigEndian.Uint16(firstPacket[5:7]))
	if totalLen <= 0 {
		return 0, nil, fmt.Errorf("invalid APDU length %d", totalLen)
	}

	// Allocate buffer for full payload.
	fullPayload = make([]byte, totalLen)

	// Data in first packet starts at offset 7.
	nDataFirst := packetLength - 7
	if totalLen < nDataFirst {
		nDataFirst = totalLen
	}
	copy(fullPayload[0:nDataFirst], firstPacket[7:7+nDataFirst])
	offset := nDataFirst
	expectedSeq = 1

	// Read subsequent packets until full payload is reassembled.
	for offset < totalLen {
		packet := make([]byte, packetLength)
		n, err := device.Read(packet)
		if err != nil {
			return 0, nil, fmt.Errorf("read packet seq %d: %w", expectedSeq, err)
		}
		if n != packetLength {
			return 0, nil, fmt.Errorf("packet seq %d incomplete: got %d of %d bytes", expectedSeq, n, packetLength)
		}
		// Subsequent packet header is 5 bytes:
		// Bytes 0-1: channel
		// Byte 2: tag
		if packet[2] != tagAPDU {
			return 0, nil, fmt.Errorf("unexpected tag in packet seq %d: got 0x%x, expected 0x%x", expectedSeq, packet[2], tagAPDU)
		}
		// Bytes 3-4: sequence number
		seq = binary.BigEndian.Uint16(packet[3:5])
		if seq != expectedSeq {
			return 0, nil, fmt.Errorf("unexpected sequence: got %d, expected %d", seq, expectedSeq)
		}
		// Data in packet starts at offset 5.
		remaining := totalLen - offset
		nData := packetLength - 5
		if remaining < nData {
			nData = remaining
		}
		copy(fullPayload[offset:offset+nData], packet[5:5+nData])
		offset += nData
		expectedSeq++
	}

	// At this point, fullPayload holds the complete APDU response.
	// According to the spec, the last two bytes are SW (status word).
	if totalLen < 2 {
		return 0, nil, fmt.Errorf("APDU response too short to contain status word")
	}

	sw = int(binary.BigEndian.Uint16(fullPayload[totalLen-2 : totalLen]))
	// The rest is the data payload.
	dataPayload := fullPayload[:totalLen-2]

	return sw, dataPayload, nil
}

func transportAPDU(h *hid.Device, apdu []byte) ([]byte, error) {
	err := writeAPDU(h, apdu)
	if err != nil {
		return nil, err
	}

	sw, payload, err := readAPDU(h)
	if err != nil {
		return nil, err
	}
	apduResult := APDUResult(sw)
	return payload, apduResult.Error()
}
