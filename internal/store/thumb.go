package store

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
)

const thumbDirName = "thumbs"

// decodePNG 可在测试中替换以统计整图解码次数。
var decodePNG = func(data []byte) (image.Image, error) {
	return png.Decode(bytes.NewReader(data))
}

// thumbPath 返回缩略图缓存路径：<history 目录>/thumbs/<id>.png。
func thumbPath(e Entry) string {
	return filepath.Join(filepath.Dir(e.FilePath), thumbDirName, e.ID+".png")
}

// Thumbnail 返回长边不超过 maxSide 的缩略图（保持宽高比，面积平均缩小）。
// 优先读取 thumbs 缓存；未命中时解码原图、缩小并尽力写入缓存。
// 原图本身不大于 maxSide 时直接返回解码结果且不写缓存。可在后台 goroutine 调用。
func Thumbnail(e Entry, maxSide int) (*image.NRGBA, error) {
	if maxSide <= 0 {
		maxSide = 1
	}
	tp := thumbPath(e)
	if data, err := os.ReadFile(tp); err == nil {
		if img, err := decodePNG(data); err == nil {
			b := img.Bounds()
			if max(b.Dx(), b.Dy()) <= maxSide {
				return toNRGBA(img), nil
			}
		}
	}

	data, err := os.ReadFile(e.FilePath)
	if err != nil {
		return nil, err
	}
	img, err := decodePNG(data)
	if err != nil {
		return nil, err
	}
	src := toNRGBA(img)
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	long := max(w, h)
	if long <= maxSide {
		return src, nil
	}
	ow := max(1, w*maxSide/long)
	oh := max(1, h*maxSide/long)
	thumb := boxScale(src, ow, oh)
	writeThumb(tp, thumb)
	return thumb, nil
}

func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok {
		return n
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out
}

// writeThumb 以临时文件 + rename 原子写入缓存（0o600，失败忽略）。
func writeThumb(path string, img *image.NRGBA) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	var buf bytes.Buffer
	if png.Encode(&buf, img) != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, buf.Bytes(), 0o600) != nil {
		return
	}
	if os.Rename(tmp, path) != nil {
		_ = os.Remove(tmp)
	}
}

// boxScale 面积平均缩小到 ow×oh。
func boxScale(src *image.NRGBA, ow, oh int) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, ow, oh))
	for oy := range oh {
		y0 := oy * h / oh
		y1 := max(y0+1, (oy+1)*h/oh)
		for ox := range ow {
			x0 := ox * w / ow
			x1 := max(x0+1, (ox+1)*w/ow)
			var r, g, bl, a, n int
			for y := y0; y < y1; y++ {
				row := src.Pix[src.PixOffset(b.Min.X+x0, b.Min.Y+y):]
				for x := 0; x < (x1-x0)*4; x += 4 {
					r += int(row[x])
					g += int(row[x+1])
					bl += int(row[x+2])
					a += int(row[x+3])
					n++
				}
			}
			o := dst.PixOffset(ox, oy)
			dst.Pix[o] = uint8(r / n)
			dst.Pix[o+1] = uint8(g / n)
			dst.Pix[o+2] = uint8(bl / n)
			dst.Pix[o+3] = uint8(a / n)
		}
	}
	return dst
}
