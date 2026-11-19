package vision

import (
	"cmp"
	"slices"
	"strings"
)

// Table 表示从 OCR 文本框空间分布还原出的二维结构化表格。
type Table struct {
	Rows [][]string
	Cols int
}

// ExtractTable 对 OCR 识别返回的归一化文本框进行二维行列聚类，
// 在检测到至少 2 行 × 2 列的表格结构时返回还原后的表格。
func ExtractTable(boxes []TextBox) (Table, bool) {
	if len(boxes) < 4 {
		return Table{}, false
	}
	valid := make([]TextBox, 0, len(boxes))
	for _, bx := range boxes {
		bx.Text = strings.TrimSpace(bx.Text)
		if bx.Text == "" || bx.W <= 0 || bx.H <= 0 {
			continue
		}
		valid = append(valid, bx)
	}
	if len(valid) < 4 {
		return Table{}, false
	}

	// 1. 按纵坐标聚合成自然行。
	slices.SortFunc(valid, func(a, b TextBox) int {
		return cmp.Compare(a.Top+a.H*0.5, b.Top+b.H*0.5)
	})
	type rowCluster struct {
		midY  float64
		avgH  float64
		cells []TextBox
	}
	var rowClusters []rowCluster
	for _, bx := range valid {
		cy := bx.Top + bx.H*0.5
		placed := false
		for i := range rowClusters {
			tol := max(rowClusters[i].avgH, bx.H) * 0.55
			if abs(cy-rowClusters[i].midY) <= tol {
				n := float64(len(rowClusters[i].cells))
				rowClusters[i].midY = (rowClusters[i].midY*n + cy) / (n + 1)
				rowClusters[i].avgH = (rowClusters[i].avgH*n + bx.H) / (n + 1)
				rowClusters[i].cells = append(rowClusters[i].cells, bx)
				placed = true
				break
			}
		}
		if !placed {
			rowClusters = append(rowClusters, rowCluster{
				midY:  cy,
				avgH:  max(0.01, bx.H),
				cells: []TextBox{bx},
			})
		}
	}
	if len(rowClusters) < 2 {
		return Table{}, false
	}
	multiCellRows := 0
	for i := range rowClusters {
		slices.SortFunc(rowClusters[i].cells, func(a, b TextBox) int {
			return cmp.Compare(a.X, b.X)
		})
		if len(rowClusters[i].cells) >= 2 {
			multiCellRows++
		}
	}
	if multiCellRows < 2 {
		return Table{}, false
	}

	// 2. 通过跨行 X 区间投影聚类确定稳定列槽。
	// 为避免跨列长标题吞并相邻列，先基于横向重叠把所有单元格 X 区间做适度收缩后合并。
	intervals := make([]colBand, 0, len(valid))
	for _, rc := range rowClusters {
		if len(rc.cells) < 2 {
			continue
		}
		for _, cell := range rc.cells {
			shrink := min(cell.W*0.12, 0.006)
			lo := cell.X + shrink
			hi := cell.X + cell.W - shrink
			if hi <= lo {
				lo, hi = cell.X, cell.X+cell.W
			}
			intervals = append(intervals, colBand{lo: lo, hi: hi})
		}
	}
	slices.SortFunc(intervals, func(a, b colBand) int {
		if c := cmp.Compare(a.lo, b.lo); c != 0 {
			return c
		}
		return cmp.Compare(a.hi, b.hi)
	})
	var colBands []colBand
	for _, iv := range intervals {
		if len(colBands) == 0 {
			colBands = append(colBands, iv)
			continue
		}
		last := &colBands[len(colBands)-1]
		// 存在实质横向重叠则属于同一列；明显空隙则划分新列。
		if iv.lo <= last.hi {
			if iv.hi > last.hi {
				last.hi = iv.hi
			}
		} else {
			colBands = append(colBands, iv)
		}
	}
	if len(colBands) < 2 {
		return Table{}, false
	}

	// 3. 将每行文本框映射到对应列索引（按最大横向重叠或最近中心归属）。
	colCount := len(colBands)
	rows := make([][]string, len(rowClusters))
	nonEmptyCols := make([]bool, colCount)
	for rIdx, rc := range rowClusters {
		row := make([]string, colCount)
		for _, cell := range rc.cells {
			cIdx := assignColumn(cell, colBands)
			if row[cIdx] == "" {
				row[cIdx] = cell.Text
			} else if needsSpaceBetween(row[cIdx], cell.Text) {
				row[cIdx] += " " + cell.Text
			} else {
				row[cIdx] += cell.Text
			}
			nonEmptyCols[cIdx] = true
		}
		rows[rIdx] = row
	}

	// 剔除完全空白的冗余列。
	var keepCols []int
	for cIdx, ok := range nonEmptyCols {
		if ok {
			keepCols = append(keepCols, cIdx)
		}
	}
	if len(keepCols) < 2 {
		return Table{}, false
	}
	if len(keepCols) != colCount {
		for rIdx := range rows {
			compact := make([]string, len(keepCols))
			for j, orig := range keepCols {
				compact[j] = rows[rIdx][orig]
			}
			rows[rIdx] = compact
		}
		colCount = len(keepCols)
	}
	return Table{Rows: rows, Cols: colCount}, true
}

type colBand struct{ lo, hi float64 }

func assignColumn(cell TextBox, bands []colBand) int {
	cLo, cHi := cell.X, cell.X+cell.W
	bestIdx := 0
	bestOverlap := -1.0
	for i, b := range bands {
		ov := min(cHi, b.hi) - max(cLo, b.lo)
		if ov > bestOverlap {
			bestOverlap = ov
			bestIdx = i
		}
	}
	if bestOverlap > 0 {
		return bestIdx
	}
	cx := cell.X + cell.W*0.5
	bestDist := 2.0
	for i, b := range bands {
		d := abs(cx - (b.lo+b.hi)*0.5)
		if d < bestDist {
			bestDist = d
			bestIdx = i
		}
	}
	return bestIdx
}

// Markdown 将表格格式化为 GitHub Flavored Markdown 表格（首行作为表头）。
func (t Table) Markdown() string {
	if len(t.Rows) == 0 || t.Cols == 0 {
		return ""
	}
	var sb strings.Builder
	writeRow := func(cols []string) {
		sb.WriteString("|")
		for i := range t.Cols {
			val := ""
			if i < len(cols) {
				val = strings.ReplaceAll(cols[i], "|", `\|`)
				val = strings.ReplaceAll(val, "\n", " ")
			}
			sb.WriteByte(' ')
			sb.WriteString(val)
			sb.WriteString(" |")
		}
		sb.WriteByte('\n')
	}
	writeRow(t.Rows[0])
	sb.WriteString("|")
	for range t.Cols {
		sb.WriteString(" --- |")
	}
	sb.WriteByte('\n')
	for _, r := range t.Rows[1:] {
		writeRow(r)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// TSV 将表格格式化为制表符分隔文本，可直接粘贴进 Excel / Numbers / 飞书表格 / Notion。
func (t Table) TSV() string {
	if len(t.Rows) == 0 || t.Cols == 0 {
		return ""
	}
	lines := make([]string, len(t.Rows))
	for i, r := range t.Rows {
		cells := make([]string, t.Cols)
		for j := range t.Cols {
			if j < len(r) {
				s := strings.ReplaceAll(r[j], "\t", " ")
				s = strings.ReplaceAll(s, "\n", " ")
				cells[j] = s
			}
		}
		lines[i] = strings.Join(cells, "\t")
	}
	return strings.Join(lines, "\n")
}
