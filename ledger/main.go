package ledger

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"slices"
	"strings"

	"github.com/karalabe/hid"
	"github.com/trilitech/tzgo/tezos"
)

var (
	VENDOR_IDS = []uint16{0x2c97, 0x2581}
)

type Curve int

const (
	CurveEd25519 Curve = iota
	CurveSecp256k1
	CurveSecp256r1
	CurveBip32_ed25519
)

func (c Curve) String() string {
	switch c {
	case CurveEd25519:
		return "ed25519"
	case CurveSecp256k1:
		return "secp256k1"
	case CurveSecp256r1:
		return "P-256"
	case CurveBip32_ed25519:
		return "bip25519"
	default:
		return "unknown"
	}
}

func hard(n int32) int32 {
	return n | -2147483648 // Set the high bit (31st bit)
}

func pathToBytes(path []int32) []byte {
	nbDerivations := len(path)

	// Create a buffer of appropriate size
	buf := new(bytes.Buffer)

	// First byte is the number of derivations
	buf.WriteByte(byte(nbDerivations))

	// Write each derivation as 4 bytes in BigEndian format
	for _, v := range path {
		err := binary.Write(buf, binary.BigEndian, v)
		if err != nil {
			log.Fatalf("Failed to write path: %v", err)
		}
	}

	return buf.Bytes()
}

var (
	tezos_root = pathToBytes([]int32{hard(44), hard(1729)})
)

func createGetPublicKeyAPDU() ([]byte, error) {
	nbDerivations := len(tezos_root)
	lc := 1 + (4 * nbDerivations)
	dataInit := make([]byte, lc)
	dataInit[0] = byte(nbDerivations)
	buf := bytes.NewBuffer(dataInit[1:])
	if _, err := buf.Write(dataInit[1:]); err != nil {
		return nil, err
	}
	L := byte(len(tezos_root))

	// https://github.com/vbmithr/ocaml-ledger-wallet/blob/master/src/ledgerwallet_tezos.ml#L71
	apdu := []byte{0x80, 0x02, 0x00, 0x00, L}
	apdu = append(apdu, tezos_root...)
	return apdu, nil
}

func IsLedger(vendorID uint16) bool {
	return slices.Contains(VENDOR_IDS, vendorID)
}

func GetLedgerId(h *hid.Device) (string, error) {
	if !IsLedger(h.DeviceInfo.VendorID) {
		return "", fmt.Errorf("device is not a Ledger device")
	}

	apdu, err := createGetPublicKeyAPDU()
	if err != nil {
		return "", err
	}

	response, err := transportAPDU(h, apdu)
	if err != nil {
		return "", err
	}

	if len(response) < 1 {
		return "", fmt.Errorf("response too short")
	}

	keyLen := int(response[0])
	if len(response) < 1+keyLen {
		return "", fmt.Errorf("response too short for key data")
	}

	key := response[1 : 1+keyLen]

	tezosKey := tezos.Key{
		Type: tezos.KeyTypeEd25519,
		Data: key[1:], // skip public key prefix
	}

	return CrouchingTigerName(tezosKey.Address().Encode()).String(), nil
}

func createGetVersionAPDU() ([]byte, error) {
	// https://github.com/vbmithr/ocaml-ledger-wallet/blob/master/src/ledgerwallet_tezos.ml#L69
	apdu := []byte{0x80, 0x00, 0x00, 0x00, 0x00}
	return apdu, nil
}

func GetAppVersion(h *hid.Device) (string, error) {
	if !IsLedger(h.DeviceInfo.VendorID) {
		return "", fmt.Errorf("device is not a Ledger device")
	}

	apdu, err := createGetVersionAPDU()
	if err != nil {
		return "", err
	}

	response, err := transportAPDU(h, apdu)
	if err != nil {
		return "", err
	}

	versionBytes := response[1:]
	if len(versionBytes) < 3 {
		return "", fmt.Errorf("version response too short")
	}
	// extract major minor patch
	version := fmt.Sprintf("%d.%d.%d", versionBytes[0], versionBytes[1], versionBytes[2])

	return version, nil
}

func createGetAuthorizedKeyAPDU() ([]byte, error) {
	// https://github.com/vbmithr/ocaml-ledger-wallet/blob/master/src/ledgerwallet_tezos.ml#L82

	apdu := []byte{0x80, 0x0D, 0x00, 0x00, 0x00}
	return apdu, nil
}

func GetAuthorizedPath(h *hid.Device) (string, error) {
	if !IsLedger(h.DeviceInfo.VendorID) {
		return "", fmt.Errorf("device is not a Ledger device")
	}

	apdu, err := createGetAuthorizedKeyAPDU()
	if err != nil {
		return "", err
	}

	response, err := transportAPDU(h, apdu)
	if err != nil {
		return "", err
	}

	// shift 1

	curve := Curve(response[0])
	response = response[1:]
	pathComponentsLength := int(response[0])
	response = response[1:]
	// each component is 4 bytes
	if len(response) < 4+pathComponentsLength { // 4 bytes per component
		return "", fmt.Errorf("response too short for key data")
	}

	unhard := func(b []byte) []byte {
		if len(b) != 4 {
			return b
		}
		return []byte{b[0] & 0x7F, b[1], b[2], b[3]}
	}

	components := make([]string, pathComponentsLength)
	for i := 0; i < pathComponentsLength; i++ {
		components[i] = fmt.Sprintf("%d", binary.BigEndian.Uint32(unhard(response[i*4:(i+1)*4])))
	}

	path := fmt.Sprintf("%s:%s", curve, strings.Join(components, "/"))

	return path, nil
}
