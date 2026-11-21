package main

import (
	"image"
	"reflect"
	"testing"
)

// 已关闭贴图记录不得再持有解码后的整图（这是内存问题的根因），只保留 PNG 与几何信息。
func TestClosedPinRecordHoldsNoDecodedImage(t *testing.T) {
	typ := reflect.TypeOf(closedPinRecord{})
	for i := range typ.NumField() {
		f := typ.Field(i)
		if f.Type == reflect.TypeOf((*image.NRGBA)(nil)) || f.Type == reflect.TypeOf((*image.RGBA)(nil)) {
			t.Fatalf("closedPinRecord.%s 持有解码图像，应只保存 PNG", f.Name)
		}
	}
	a := &app{}
	a.pushClosedPin(closedPinRecord{origW: 10}) // 无 PNG 无法恢复，应被忽略
	if len(a.closedPins) != 0 {
		t.Fatal("empty-png record should be dropped")
	}
}

func TestDecodeClosedPinRestoresImageSize(t *testing.T) {
	rec := closedPinRecord{png: testHistoryPNG(37, 21), origW: 37, origH: 21}
	img, err := decodeClosedPin(rec)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Min != (image.Point{}) || b.Dx() != 37 || b.Dy() != 21 {
		t.Fatalf("decoded bounds = %v, want 37x21 at origin", b)
	}
	if _, err := decodeClosedPin(closedPinRecord{png: []byte("not png")}); err == nil {
		t.Fatal("expected decode error for invalid png")
	}
}

func TestClosedPinEvictsOldestByByteCap(t *testing.T) {
	a := &app{}
	chunk := maxClosedPinBytes / 4
	for i := range 6 {
		a.pushClosedPin(closedPinRecord{png: make([]byte, chunk), origW: i})
	}
	// 200MB 上限、每条 50MB：最多保留 4 条，且最新的在末尾。
	if len(a.closedPins) != 4 {
		t.Fatalf("closedPins len = %d, want 4", len(a.closedPins))
	}
	if a.closedPins[0].origW != 2 || a.closedPins[3].origW != 5 {
		t.Fatalf("应淘汰最旧记录: first=%d last=%d", a.closedPins[0].origW, a.closedPins[3].origW)
	}
	// 单条超过上限也至少保留刚关闭的这一张。
	b := &app{}
	b.pushClosedPin(closedPinRecord{png: make([]byte, maxClosedPinBytes+1), origW: 9})
	if len(b.closedPins) != 1 {
		t.Fatalf("oversized single record len = %d, want 1", len(b.closedPins))
	}
}
