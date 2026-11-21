package main

import (
	"encoding/binary"
	"testing"
)

// ===== bóc gói RTP =====

func rtpGoi(mark bool, ts uint32, pay ...byte) []byte {
	h := []byte{0x80, 96, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0}
	if mark {
		h[1] |= 0x80
	}
	binary.BigEndian.PutUint32(h[4:8], ts)
	return append(h, pay...)
}

// Một NAL bị cắt làm nhiều gói (FU-A) phải ghép lại y như cũ, kèm byte đầu dựng lại đúng.
func TestNhanFUA(t *testing.T) {
	l := &luongH264{}
	if _, _, _, xong := l.nhan(rtpGoi(false, 900, 0x7C, 0x85, 'a', 'b', 'c')); xong {
		t.Fatal("mới nửa khung mà đã báo xong")
	}
	au, ts, key, xong := l.nhan(rtpGoi(true, 900, 0x7C, 0x45, 'd', 'e', 'f'))
	if !xong {
		t.Fatal("hết mảnh rồi mà không báo xong")
	}
	if ts != 900 || !key {
		t.Fatalf("phải là khung chính ở mốc 900, nhận ts=%d key=%v", ts, key)
	}
	if len(au) != 1 || string(au[0]) != "\x65abcdef" {
		t.Fatalf("ghép sai: %q", au)
	}
}

// Vào giữa chừng (mất mảnh đầu) thì bỏ qua, không được ghép ra NAL rách.
func TestNhanFUAMatDauThiBo(t *testing.T) {
	l := &luongH264{}
	l.nhan(rtpGoi(false, 900, 0x7C, 0x05, 'x', 'y')) // mảnh giữa, không có bit bắt đầu
	if au, _, _, xong := l.nhan(rtpGoi(true, 900, 0x7C, 0x45, 'z')); xong || au != nil {
		t.Fatalf("mất mảnh đầu mà vẫn dựng khung: %q", au)
	}
}

// STAP-A: nhiều NAL nhỏ ghép chung một gói, phải tách đủ và nhận ra SPS/PPS.
func TestNhanSTAPA(t *testing.T) {
	l := &luongH264{}
	au, _, _, xong := l.nhan(rtpGoi(true, 90, 0x78, 0, 2, 0x67, 0x01, 0, 2, 0x68, 0x02))
	if !xong || len(au) != 2 {
		t.Fatalf("phải tách ra 2 NAL, nhận %d", len(au))
	}
	if string(l.sps) != "\x67\x01" || string(l.pps) != "\x68\x02" {
		t.Fatalf("không nhận ra SPS/PPS: sps=%q pps=%q", l.sps, l.pps)
	}
}

// Gói rách (khai độ dài vượt quá dữ liệu thật) không được làm vỡ chương trình.
func TestNhanGoiRach(t *testing.T) {
	l := &luongH264{}
	l.nhan(rtpGoi(true, 1, 0x78, 0xFF, 0xFF, 0x67)) // STAP-A khai 65535 byte mà chỉ có 1
	l.nhan(rtpGoi(true, 1, 0x7C))                   // FU-A cụt
	l.nhan(rtpGoi(true, 1))                         // không có dữ liệu
}

// ===== đọc SDP =====

const sdpH264 = "v=0\r\nm=video 0 RTP/AVP 96\r\n" +
	"a=rtpmap:96 H264/90000\r\n" +
	"a=fmtp:96 packetization-mode=1;sprop-parameter-sets=Z2QAH6zZQFAFuhAAAAMAEAAAAwPI8YMZYA==,aOvjyyLA\r\n" +
	"a=control:trackID=1\r\n"

func TestDoSDPH264(t *testing.T) {
	ten, sps, pps := doSDP(sdpH264)
	if ten != "H264" {
		t.Fatalf("phải là H264, nhận %q", ten)
	}
	if len(sps) < 4 || sps[0]&0x1F != 7 {
		t.Fatalf("SPS sai: %x", sps)
	}
	if len(pps) == 0 || pps[0]&0x1F != 8 {
		t.Fatalf("PPS sai: %x", pps)
	}
	if ma := maH264(sps); ma != "avc1.64001F" {
		t.Fatalf("mã bộ nén sai: %s", ma)
	}
}

// Camera H.265 phải nhận ra được, vì trình duyệt không phát được và phải báo cho người dùng.
func TestDoSDPH265(t *testing.T) {
	ten, _, _ := doSDP("m=video 0 RTP/AVP 96\r\na=rtpmap:96 H265/90000\r\n")
	if ten != "H265" {
		t.Fatalf("phải nhận ra H265, nhận %q", ten)
	}
}

// Phần âm thanh không được lấn át phần hình khi đọc SDP.
func TestDoSDPBoAmThanh(t *testing.T) {
	ten, _, _ := doSDP("m=audio 0 RTP/AVP 8\r\na=rtpmap:8 PCMA/8000\r\n" + sdpH264)
	if ten != "H264" {
		t.Fatalf("phải lấy bộ mã của phần hình, nhận %q", ten)
	}
}

// ===== dựng fMP4 =====

// duyetHop đi hết cây hộp MP4 và kiểm độ dài khai báo có khớp dữ liệu thật không. Sai một byte là
// trình duyệt câm lặng không phát, nên phải bắt ngay ở đây.
func duyetHop(t *testing.T, b []byte, duong string) map[string]bool {
	t.Helper()
	con := map[string]bool{"moov": true, "trak": true, "mdia": true, "minf": true,
		"stbl": true, "dinf": true, "mvex": true, "moof": true, "traf": true}
	thay := map[string]bool{}
	for len(b) > 0 {
		if len(b) < 8 {
			t.Fatalf("%s: thừa %d byte lẻ", duong, len(b))
		}
		n := int(binary.BigEndian.Uint32(b[:4]))
		typ := string(b[4:8])
		if n < 8 || n > len(b) {
			t.Fatalf("%s/%s: khai %d byte mà chỉ còn %d", duong, typ, n, len(b))
		}
		thay[typ] = true
		if con[typ] {
			for k := range duyetHop(t, b[8:n], duong+"/"+typ) {
				thay[k] = true
			}
		}
		b = b[n:]
	}
	return thay
}

func TestDauGoiHopLe(t *testing.T) {
	_, sps, pps := doSDP(sdpH264)
	var g goiMP4
	thay := duyetHop(t, g.dauGoi(sps, pps), "")
	for _, c := range []string{"ftyp", "moov", "mvhd", "trak", "tkhd", "mdia", "stsd", "mvex", "trex"} {
		if !thay[c] {
			t.Fatalf("đoạn mở đầu thiếu hộp %s", c)
		}
	}
}

func TestKhungHopLe(t *testing.T) {
	var g goiMP4
	seg := g.khung([][]byte{{0x65, 1, 2, 3}}, 9000, true)
	thay := duyetHop(t, seg, "")
	for _, c := range []string{"moof", "mfhd", "traf", "tfhd", "tfdt", "trun", "mdat"} {
		if !thay[c] {
			t.Fatalf("đoạn hình thiếu hộp %s", c)
		}
	}
	// chỗ dữ liệu phải trỏ đúng vào ngay sau đầu hộp mdat, không thì trình duyệt đọc lệch
	moofLen := int(binary.BigEndian.Uint32(seg[:4]))
	off := int(binary.BigEndian.Uint32(seg[moofLen-16 : moofLen-12]))
	if off != moofLen+8 {
		t.Fatalf("chỗ dữ liệu sai: %d, phải là %d", off, moofLen+8)
	}
	// mdat chứa NAL kèm 4 byte độ dài ở đầu
	if n := binary.BigEndian.Uint32(seg[moofLen+8 : moofLen+12]); n != 4 {
		t.Fatalf("độ dài NAL sai: %d", n)
	}
}

// Mốc thời gian phải cộng dồn theo đúng chênh lệch dấu thời gian RTP, không thì video trôi dần.
func TestKhungMocThoiGian(t *testing.T) {
	var g goiMP4
	moc := func(seg []byte) uint64 {
		i := 0
		for {
			n := int(binary.BigEndian.Uint32(seg[i : i+4]))
			if string(seg[i+4:i+8]) == "tfdt" {
				return binary.BigEndian.Uint64(seg[i+12 : i+20])
			}
			if string(seg[i+4:i+8]) == "moof" || string(seg[i+4:i+8]) == "traf" {
				i += 8
				continue
			}
			i += n
		}
	}
	if m := moc(g.khung([][]byte{{0x65}}, 1000, true)); m != 0 {
		t.Fatalf("khung đầu phải ở mốc 0, nhận %d", m)
	}
	if m := moc(g.khung([][]byte{{0x41}}, 1000+3600, false)); m != 3600 {
		t.Fatalf("khung sau phải ở mốc 3600, nhận %d", m)
	}
	if m := moc(g.khung([][]byte{{0x41}}, 1000+7200, false)); m != 7200 {
		t.Fatalf("khung thứ ba phải ở mốc 7200, nhận %d", m)
	}
	// camera nhảy giờ: bước vô lý thì giữ nhịp cũ chứ không nhảy theo
	if m := moc(g.khung([][]byte{{0x41}}, 1000+7200+mp4Nhip*600, false)); m != 7200+3600 {
		t.Fatalf("bước vô lý phải bị bỏ, nhận %d", m)
	}
}
