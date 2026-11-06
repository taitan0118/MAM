package main

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// xlsx tối giản bằng stdlib: nhiều sheet, hàng tiêu đề đậm có màu + cố định + bộ lọc, cột tự giãn, ô trạng thái tô màu.
type xSheet struct {
	Name string
	Rows [][]string
}

// kiểu ô: 1 tiêu đề, 2 thường, 3 xanh lá, 4 vàng, 5 xanh dương, 6 đỏ
var xStatus = map[string]int{
	"Đã xong": 3, "Đã đóng": 3, "Online": 3,
	"Đang xử lý": 4, "Mới": 5, "Đang mở": 6, "Offline": 6, "có": 6,
}

func xesc(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

func xcol(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

func writeXLSX(w io.Writer, sheets []xSheet) error {
	z := zip.NewWriter(w)
	add := func(name, body string) {
		f, _ := z.Create(name)
		io.WriteString(f, body)
	}
	hdr := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
	ct, wb, rel := "", "", ""
	for i, sh := range sheets {
		n := i + 1
		ct += fmt.Sprintf(`<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, n)
		wb += fmt.Sprintf(`<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, xesc(sh.Name), n, n)
		rel += fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, n, n)
	}
	add("[Content_Types].xml", hdr+`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`+ct+`</Types>`)
	add("_rels/.rels", hdr+`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`)
	add("xl/workbook.xml", hdr+`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`+wb+`</sheets></workbook>`)
	add("xl/_rels/workbook.xml.rels", hdr+`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+rel+fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, len(sheets)+1)+`</Relationships>`)
	fill := func(c string) string {
		return `<fill><patternFill patternType="solid"><fgColor rgb="FF` + c + `"/><bgColor indexed="64"/></patternFill></fill>`
	}
	font := func(c string, bold bool) string {
		b := ""
		if bold {
			b = "<b/>"
		}
		return `<font>` + b + `<sz val="11"/><color rgb="FF` + c + `"/><name val="Calibri"/></font>`
	}
	xf := func(font, fill int) string {
		return fmt.Sprintf(`<xf numFmtId="0" fontId="%d" fillId="%d" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1" applyAlignment="1"><alignment vertical="center" wrapText="1"/></xf>`, font, fill)
	}
	add("xl/styles.xml", hdr+`<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`+
		`<fonts count="6">`+font("172030", false)+font("FFFFFF", true)+font("177A44", true)+font("946006", true)+font("1556C0", true)+font("B42323", true)+`</fonts>`+
		`<fills count="7"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill>`+fill("0E2A4D")+fill("DDF3E6")+fill("FDF0D5")+fill("E8F0FE")+fill("FBE3E3")+`</fills>`+
		`<borders count="2"><border><left/><right/><top/><bottom/><diagonal/></border><border><left/><right/><top/><bottom style="thin"><color rgb="FFE1E8F0"/></bottom><diagonal/></border></borders>`+
		`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>`+
		`<cellXfs count="7"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>`+xf(1, 2)+xf(0, 0)+xf(2, 3)+xf(3, 4)+xf(4, 5)+xf(5, 6)+`</cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`)
	for i, sh := range sheets {
		widths := make([]int, 0)
		for _, row := range sh.Rows {
			for c, v := range row {
				for len(widths) <= c {
					widths = append(widths, 10)
				}
				if n := len([]rune(v)) + 3; n > widths[c] {
					widths[c] = n
				}
			}
		}
		var b strings.Builder
		b.WriteString(hdr + `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0" showGridLines="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><sheetFormatPr defaultRowHeight="20"/><cols>`)
		for c, wd := range widths {
			if wd > 60 {
				wd = 60
			}
			fmt.Fprintf(&b, `<col min="%d" max="%d" width="%d" customWidth="1"/>`, c+1, c+1, wd)
		}
		b.WriteString(`</cols><sheetData>`)
		for r, row := range sh.Rows {
			ht := ""
			if r == 0 {
				ht = ` ht="26" customHeight="1"`
			}
			fmt.Fprintf(&b, `<row r="%d"%s>`, r+1, ht)
			for c, v := range row {
				st := 2
				if r == 0 {
					st = 1
				} else if s, ok := xStatus[v]; ok {
					st = s
				}
				fmt.Fprintf(&b, `<c r="%s%d" s="%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, xcol(c), r+1, st, xesc(v))
			}
			b.WriteString(`</row>`)
		}
		b.WriteString(`</sheetData>`)
		if len(sh.Rows) > 0 && len(widths) > 0 {
			fmt.Fprintf(&b, `<autoFilter ref="A1:%s%d"/>`, xcol(len(widths)-1), len(sh.Rows))
		}
		b.WriteString(`</worksheet>`)
		add(fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), b.String())
	}
	return z.Close()
}
