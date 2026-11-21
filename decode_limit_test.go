package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"testing"
)

func TestDecodeImageLimited(t *testing.T) {
	small := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, small); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeImageLimited(buf.Bytes()); err != nil {
		t.Fatalf("正常图应通过: %v", err)
	}

	// 篡改 IHDR 宽高并重算 CRC：DecodeConfig 只读头部，不会分配像素
	bomb := append([]byte(nil), buf.Bytes()...)
	binary.BigEndian.PutUint32(bomb[16:], 40000)
	binary.BigEndian.PutUint32(bomb[20:], 40000)
	binary.BigEndian.PutUint32(bomb[29:], crc32.ChecksumIEEE(bomb[12:29]))
	_, err := decodeImageLimited(bomb)
	if !errors.Is(err, errImageTooLarge) {
		t.Fatalf("超大图应被拒绝, got %v", err)
	}
	if err.Error() != "图片尺寸过大（40000×40000），已拒绝贴图" {
		t.Fatalf("错误文案: %q", err.Error())
	}

	// 单边不超限但总像素超限
	binary.BigEndian.PutUint32(bomb[16:], 20000)
	binary.BigEndian.PutUint32(bomb[20:], 20000)
	binary.BigEndian.PutUint32(bomb[29:], crc32.ChecksumIEEE(bomb[12:29]))
	if _, err := decodeImageLimited(bomb); !errors.Is(err, errImageTooLarge) {
		t.Fatalf("总像素超限应被拒绝, got %v", err)
	}
}
