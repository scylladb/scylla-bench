package libs

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"github.com/pkg/errors"
	"math/rand"
)

const (
	generatedDataHeaderSize int64 = 24
	generatedDataMinSize int64 = generatedDataHeaderSize + 33
)

func GenerateData(pk int64, ck int64, size int64, validateData bool) []byte {
	if !validateData {
		return make([]byte, size);
	}

	buf := new(bytes.Buffer)

	if size < generatedDataHeaderSize {
		binary.Write(buf, binary.LittleEndian, int8(size))
		binary.Write(buf, binary.LittleEndian, pk ^ ck)
	} else {
		binary.Write(buf, binary.LittleEndian, size)
		binary.Write(buf, binary.LittleEndian, pk)
		binary.Write(buf, binary.LittleEndian, ck)
		if size < generatedDataMinSize {
			for i := generatedDataHeaderSize; i < size; i++ {
				binary.Write(buf, binary.LittleEndian, int8(0))
			}
		} else {
			payload := make([]byte, size - generatedDataHeaderSize - sha256.Size)
			rand.Read(payload)
			csum := sha256.Sum256(payload)
			binary.Write(buf, binary.LittleEndian, payload)
			binary.Write(buf, binary.LittleEndian, csum)
		}
	}

	value := make([]byte, size)
	copy(value, buf.Bytes())
	return value
}

func ValidateData(pk int64, ck int64, data []byte, validateData bool) error {
	if !validateData {
		return nil
	}

	buf := bytes.NewBuffer(data)
	size := int64(buf.Len())

	var storedSize int64
	if size < generatedDataHeaderSize {
		var storedSizeCompact int8
		err := binary.Read(buf, binary.LittleEndian, &storedSizeCompact)
		if err != nil {
			return errors.Wrap(err, "failed to validate data, cannot read size from value")
		}
		storedSize = int64(storedSizeCompact)
	} else {
		err := binary.Read(buf, binary.LittleEndian, &storedSize)
		if err != nil {
			return errors.Wrap(err, "failed to validate data, cannot read size from value")
		}
	}

	if size != storedSize {
		return errors.Errorf("actual size of value (%d) doesn't match size stored in value (%d)", size, storedSize)
	}

	// There is no random payload for sizes < minFullSize
	if size < generatedDataMinSize {
		expectedBuf := GenerateData(pk, ck, size, validateData)
		if !bytes.Equal(buf.Bytes(), expectedBuf) {
			return errors.Errorf("actual value doesn't match expected value:\nexpected: %x\nactual: %x", expectedBuf, buf.Bytes())
		}
		return nil
	}

	var storedPk, storedCk int64
	var err error

	// Validate pk
	err = binary.Read(buf, binary.LittleEndian, &storedPk)
	if err != nil {
		return errors.Wrap(err, "failed to validate data, cannot read pk from value")
	}
	if storedPk != pk {
		return errors.Errorf("actual pk (%d) doesn't match pk stored in value (%d)", pk, storedPk)
	}

	// Validate ck
	err = binary.Read(buf, binary.LittleEndian, &storedCk)
	if err != nil {
		return errors.Wrap(err, "failed to validate data, cannot read pk from value")
	}
	if storedCk != ck {
		return errors.Errorf("actual ck (%d) doesn't match ck stored in value (%d)", ck, storedCk)
	}

	// Validate checksum over the payload
	payload := make([]byte, size - generatedDataHeaderSize - sha256.Size)
	err = binary.Read(buf, binary.LittleEndian, payload)
	if err != nil {
		return errors.Wrap(err, "failed to verify checksum, cannot read payload from value")
	}

	calculatedChecksumArray := sha256.Sum256(payload)
	calculatedChecksum := calculatedChecksumArray[0:]

	storedChecksum := make([]byte, 32)
	err = binary.Read(buf, binary.LittleEndian, storedChecksum)
	if err != nil {
		return errors.Wrap(err, "failed to verify checksum, cannot read checksum from value")
	}

	if !bytes.Equal(calculatedChecksum, storedChecksum) {
		return errors.New(fmt.Sprintf(
			"corrupt checksum or data: calculated checksum (%x) doesn't match stored checksum (%x) over data\n%x",
			calculatedChecksum,
			storedChecksum,
			payload))
	}
	return nil
}


