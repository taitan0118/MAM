package main

// Đọc file Excel (.xlsx) bằng thư viện chuẩn: .xlsx thực chất là file zip chứa XML, nên chỉ cần
// archive/zip + encoding/xml. Dùng cho chức năng "Nhập từ file" của tab Thiết bị mạng: công ty đã có
// sẵn danh sách camera, switch, máy chấm công trong Excel, gõ tay lại cả nghìn dòng là vô lý.
//
// Chỉ đọc chữ trong ô. Không đụng tới công thức, ảnh, định dạng.

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

const (
	xlsxMaxFile  = 40 << 20 // file tải lên tối đa 40 MB (file có ảnh chụp dễ tới chục MB)
	xlsxMaxRows  = 5000     // mỗi sheet lấy tối đa chừng này dòng
	xlsxMaxCols  = 40
	xlsxMaxSheet = 30
)

type xlSheet struct {
	Name string     `json:"name"`
	Rows [][]string `json:"rows"`
}

// docXLSX trả danh sách sheet, mỗi sheet là bảng chữ đã dựng lại theo đúng hàng/cột.
func docXLSX(b []byte) ([]xlSheet, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("không mở được file: %v", err)
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[strings.TrimPrefix(f.Name, "/")] = f
	}
	doc := func(name string) ([]byte, error) {
		f := files[name]
		if f == nil {
			return nil, nil
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, 80<<20))
	}

	// chuỗi dùng chung: ô kiểu "s" chỉ chứa số thứ tự trỏ vào bảng này
	ss := []string{}
	if raw, _ := doc("xl/sharedStrings.xml"); raw != nil {
		var sst struct {
			SI []struct {
				T string   `xml:"t"`
				R []string `xml:"r>t"`
			} `xml:"si"`
		}
		if xml.Unmarshal(raw, &sst) == nil {
			for _, si := range sst.SI {
				if len(si.R) > 0 {
					ss = append(ss, strings.Join(si.R, ""))
				} else {
					ss = append(ss, si.T)
				}
			}
		}
	}

	// tên sheet và thứ tự: workbook.xml liệt kê theo đúng thứ tự hiển thị
	names := []string{}
	if raw, _ := doc("xl/workbook.xml"); raw != nil {
		var wb struct {
			Sheets []struct {
				Name string `xml:"name,attr"`
			} `xml:"sheets>sheet"`
		}
		if xml.Unmarshal(raw, &wb) == nil {
			for _, s := range wb.Sheets {
				names = append(names, s.Name)
			}
		}
	}

	// các file sheetN.xml, sắp theo số N cho khớp thứ tự trong workbook
	var paths []string
	for n := range files {
		if strings.HasPrefix(n, "xl/worksheets/sheet") && strings.HasSuffix(n, ".xml") {
			paths = append(paths, n)
		}
	}
	sort.Slice(paths, func(i, j int) bool { return soSheet(paths[i]) < soSheet(paths[j]) })

	out := []xlSheet{}
	for i, p := range paths {
		if len(out) >= xlsxMaxSheet {
			break
		}
		raw, err := doc(p)
		if err != nil || raw == nil {
			continue
		}
		ten := fmt.Sprintf("Sheet %d", i+1)
		if i < len(names) {
			ten = names[i]
		}
		rows := docSheet(raw, ss)
		if len(rows) > 0 {
			out = append(out, xlSheet{Name: ten, Rows: rows})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("file không có dữ liệu đọc được")
	}
	return out, nil
}

func soSheet(p string) int {
	n := strings.TrimSuffix(strings.TrimPrefix(p, "xl/worksheets/sheet"), ".xml")
	v, _ := strconv.Atoi(n)
	return v
}

// docSheet dựng lại bảng chữ từ XML của một sheet, giữ đúng vị trí hàng và cột.
func docSheet(raw []byte, ss []string) [][]string {
	var sh struct {
		Rows []struct {
			R     int `xml:"r,attr"`
			Cells []struct {
				R  string `xml:"r,attr"`
				T  string `xml:"t,attr"`
				V  string `xml:"v"`
				IS string `xml:"is>t"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if xml.Unmarshal(raw, &sh) != nil {
		return nil
	}
	out := [][]string{}
	for _, r := range sh.Rows {
		if len(out) >= xlsxMaxRows {
			break
		}
		row := make([]string, 0, xlsxMaxCols)
		for _, c := range r.Cells {
			cot := cotSo(c.R)
			if cot <= 0 || cot > xlsxMaxCols {
				continue
			}
			v := c.V
			switch c.T {
			case "s":
				if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && i >= 0 && i < len(ss) {
					v = ss[i]
				} else {
					v = ""
				}
			case "inlineStr":
				v = c.IS
			}
			v = strings.TrimSpace(strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(v))
			for len(row) < cot {
				row = append(row, "")
			}
			row[cot-1] = v
		}
		cóChữ := false
		for _, v := range row {
			if v != "" {
				cóChữ = true
				break
			}
		}
		if cóChữ {
			out = append(out, row)
		}
	}
	return out
}

// cotSo đổi "C12" thành 3.
func cotSo(ref string) int {
	n := 0
	for _, ch := range ref {
		if ch >= 'A' && ch <= 'Z' {
			n = n*26 + int(ch-'A') + 1
		} else if ch >= 'a' && ch <= 'z' {
			n = n*26 + int(ch-'a') + 1
		} else {
			break
		}
	}
	return n
}
