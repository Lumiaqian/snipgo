// Package store 管理最近截图的本地有界环形历史（PNG 文件落盘 + 元数据索引，超出上限自动清理旧文件）。
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const DefaultMaxEntries = 20

// Entry 表示一条历史截图记录。
type Entry struct {
	ID        string    `json:"id"`
	FileName  string    `json:"file_name"`
	FilePath  string    `json:"-"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	CreatedAt time.Time `json:"created_at"`
	// Scale 为截图时的像素/DIP 比例；0 表示旧记录未记录（调用方回退按 2x 估算）。
	Scale float64 `json:"scale,omitempty"`
}

// DipSize 返回贴图使用的逻辑尺寸；Scale>0 时按 Scale 换算，否则回退到旧的 2x 假设。
func (e Entry) DipSize() (w, h int) {
	sc := e.Scale
	if sc <= 0 {
		sc = 2
	}
	return int(float64(e.Width)/sc + 0.5), int(float64(e.Height)/sc + 0.5)
}

// HistoryStore 管理磁盘上的截图历史。
type HistoryStore struct {
	mu      sync.Mutex
	dir     string
	max     int
	seq     uint64
	entries []Entry
}

// Open 打开或创建指定目录下的截图历史仓库。
func Open(baseDir string, maxEntries int) (*HistoryStore, error) {
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}
	dir := filepath.Join(baseDir, "history")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &HistoryStore{
		dir: dir,
		max: maxEntries,
	}
	s.loadIndex()
	return s, nil
}

func (s *HistoryStore) indexPath() string {
	return filepath.Join(s.dir, "index.json")
}

func (s *HistoryStore) loadIndex() {
	data, err := os.ReadFile(s.indexPath())
	if err != nil {
		return
	}
	var raw []Entry
	if json.Unmarshal(data, &raw) != nil {
		return
	}
	valid := make([]Entry, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, e := range raw {
		if e.FileName == "" || seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		full := filepath.Join(s.dir, filepath.Base(e.FileName))
		if st, err := os.Stat(full); err == nil && !st.IsDir() {
			e.FilePath = full
			valid = append(valid, e)
		}
	}
	if len(valid) > s.max {
		for _, old := range valid[s.max:] {
			removeEntryFiles(old)
		}
		valid = valid[:s.max]
	}
	s.entries = valid
	s.pruneOrphanThumbs()
}

// pruneOrphanThumbs 清理 thumbs 目录中已无对应历史记录的缩略图（尽力而为）。
func (s *HistoryStore) pruneOrphanThumbs() {
	dir := filepath.Join(s.dir, thumbDirName)
	files, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	keep := make(map[string]bool, len(s.entries))
	for _, e := range s.entries {
		keep[e.ID+".png"] = true
	}
	for _, f := range files {
		if !keep[f.Name()] {
			_ = os.Remove(filepath.Join(dir, f.Name()))
		}
	}
}

func (s *HistoryStore) saveIndexLocked() {
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return
	}
	tmp := s.indexPath() + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, s.indexPath())
	}
}

// Add 写入一张新截图 PNG 到历史记录（最新排在最前），自动淘汰超出容量上限的旧文件。
func (s *HistoryStore) Add(pngBytes []byte, w, h int, scale float64) (Entry, error) {
	if len(pngBytes) == 0 {
		return Entry{}, fmt.Errorf("empty png data")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	// ID 含微秒，跨重启也不易重复；文件用 O_EXCL 创建，冲突时递增 seq 重试
	stamp := strings.Replace(now.Format("20060102-150405.000000"), ".", "", 1)
	var id, fname, fpath string
	for tries := 0; ; tries++ {
		s.seq++
		id = fmt.Sprintf("%s-%d", stamp, s.seq)
		fname = "SnipGo-" + id + ".png"
		fpath = filepath.Join(s.dir, fname)
		err := writeNewFile(fpath, pngBytes)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) || tries >= 1000 {
			return Entry{}, err
		}
	}

	entry := Entry{
		ID:        id,
		FileName:  fname,
		FilePath:  fpath,
		Width:     w,
		Height:    h,
		CreatedAt: now,
		Scale:     scale,
	}
	next := make([]Entry, 0, min(s.max, len(s.entries)+1))
	next = append(next, entry)
	for _, old := range s.entries {
		if len(next) < s.max {
			next = append(next, old)
		} else {
			removeEntryFiles(old)
		}
	}
	s.entries = next
	s.saveIndexLocked()
	return entry, nil
}

// List 返回按时间倒序（最新在前）的历史截图副本。
func (s *HistoryStore) List() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, len(s.entries))
	copy(out, s.entries)
	return out
}

// Count 返回当前历史条数。
func (s *HistoryStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// Delete 删除单条历史记录及其磁盘文件。
func (s *HistoryStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.entries {
		if e.ID == id {
			removeEntryFiles(e)
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			s.saveIndexLocked()
			return true
		}
	}
	return false
}

// Clear 清空所有历史截图记录及磁盘文件。
func (s *HistoryStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		removeEntryFiles(e)
	}
	s.entries = nil
	s.saveIndexLocked()
}

// removeEntryFiles 删除记录对应的原图与缩略图缓存。
func removeEntryFiles(e Entry) {
	_ = os.Remove(e.FilePath)
	_ = os.Remove(thumbPath(e))
}

// ReadPNG 只读取某条历史记录的原始 PNG 字节（不解码），用于复制到剪贴板等无需像素的场景。
func ReadPNG(e Entry) ([]byte, error) {
	return os.ReadFile(e.FilePath)
}

// Load 读取某条历史记录的原始 PNG 字节与解码后的 NRGBA 位图。
func Load(e Entry) (*image.NRGBA, []byte, error) {
	data, err := os.ReadFile(e.FilePath)
	if err != nil {
		return nil, nil, err
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	if nrgba, ok := decoded.(*image.NRGBA); ok {
		return nrgba, data, nil
	}
	b := decoded.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), decoded, b.Min, draw.Src)
	return out, data, nil
}

// writeNewFile 以 O_EXCL 创建并写入文件，已存在时返回 fs.ErrExist。
func writeNewFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(path)
	}
	return werr
}
