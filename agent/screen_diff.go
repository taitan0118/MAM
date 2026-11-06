package main

import (
	"bytes"
	"image"
)

const tilePx = 64 // cạnh ô so sánh khi tìm vùng thay đổi

// changedRects so sánh hai ảnh theo từng ô 64x64 và trả các hình chữ nhật bao các ô đã đổi
// (ghép ô liền nhau theo hàng, rồi ghép các hàng giống hệt nhau theo chiều dọc), cùng tỉ lệ ô đổi.
func changedRects(cur *image.RGBA, prev []byte) ([]image.Rectangle, float64) {
	w, h := cur.Rect.Dx(), cur.Rect.Dy()
	tx, ty := (w+tilePx-1)/tilePx, (h+tilePx-1)/tilePx
	mask := make([]bool, tx*ty)
	changed := 0
	for j := 0; j < ty; j++ {
		y0, y1 := j*tilePx, min((j+1)*tilePx, h)
		for i := 0; i < tx; i++ {
			x0, x1 := i*tilePx, min((i+1)*tilePx, w)
			for y := y0; y < y1; y++ {
				o := y*cur.Stride + x0*4
				if !bytes.Equal(cur.Pix[o:o+(x1-x0)*4], prev[o:o+(x1-x0)*4]) {
					mask[j*tx+i] = true
					changed++
					break
				}
			}
		}
	}
	if changed == 0 {
		return nil, 0
	}
	type run struct{ i0, i1, j0, j1 int } // ô [i0,i1) x hàng [j0,j1)
	var runs []run
	for j := 0; j < ty; j++ {
		for i := 0; i < tx; {
			if !mask[j*tx+i] {
				i++
				continue
			}
			k := i
			for k < tx && mask[j*tx+k] {
				k++
			}
			merged := false
			for n := range runs { // nối với run ngay phía trên nếu cùng khoảng cột
				if runs[n].j1 == j && runs[n].i0 == i && runs[n].i1 == k {
					runs[n].j1 = j + 1
					merged = true
					break
				}
			}
			if !merged {
				runs = append(runs, run{i, k, j, j + 1})
			}
			i = k
		}
	}
	rs := make([]image.Rectangle, 0, len(runs))
	for _, r := range runs {
		rs = append(rs, image.Rect(r.i0*tilePx, r.j0*tilePx, min(r.i1*tilePx, w), min(r.j1*tilePx, h)))
	}
	return rs, float64(changed) / float64(tx*ty)
}
