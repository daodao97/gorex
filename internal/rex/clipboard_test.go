package rex

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"testing"
)

func TestImagePasteRejectsDecompressionBombBeforeAllocation(t *testing.T) {
	var buffer bytes.Buffer
	png.Encode(&buffer, image.NewNRGBA(image.Rect(0, 0, 2, 2)))
	data := buffer.Bytes()
	if err := ValidateClipboardImage(data); err != nil {
		t.Fatal(err)
	}
	bomb := bytes.Clone(data)
	binary.BigEndian.PutUint32(bomb[16:20], 12000)
	binary.BigEndian.PutUint32(bomb[20:24], 12000)
	binary.BigEndian.PutUint32(bomb[29:33], crc32.ChecksumIEEE(bomb[12:29]))
	if err := ValidateClipboardImage(bomb); err == nil {
		t.Fatal("oversized pixel dimensions accepted")
	}
	if err := ValidateClipboardImage(nil); err == nil {
		t.Fatal("empty image accepted")
	}
	if err := ValidateClipboardImage([]byte("not a PNG")); err == nil {
		t.Fatal("non-image accepted")
	}
}
