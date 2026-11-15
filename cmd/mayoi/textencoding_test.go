package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

func TestDecodeFileRoundTrips(t *testing.T) {
	csv := "名称,等级\r\n僵尸,3\r\n"
	gbk, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(csv))
	le, _ := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().Bytes([]byte(csv))
	be, _ := unicode.UTF16(unicode.BigEndian, unicode.UseBOM).NewEncoder().Bytes([]byte(csv))
	cases := []struct {
		name string
		data []byte
		want *textEncoding
	}{
		{"utf8", []byte(csv), nil},
		{"utf8 bom", append([]byte{0xEF, 0xBB, 0xBF}, csv...), utf8BOMText},
		{"utf16 le", le, utf16LEText},
		{"utf16 be", be, utf16BEText},
		{"gbk", gbk, gb18030Text},
	}
	for _, c := range cases {
		text, enc, err := decodeFileWith(c.data, gb18030Text)
		if err != nil || enc != c.want || string(text) != csv {
			t.Fatalf("%s: got %q %v %v", c.name, text, enc.label(), err)
		}
		back, err := encodeText(text, enc)
		if err != nil || !bytes.Equal(back, c.data) {
			t.Fatalf("%s: wrote %x, want %x (%v)", c.name, back, c.data, err)
		}
	}
}

func TestDecodeFileRejectsBinary(t *testing.T) {
	for _, data := range [][]byte{
		{0x00, 0x01, 0x02, 0xff},
		bytes.Repeat([]byte{0x01, 0x02, 0x81, 0x03}, 100),
	} {
		if _, _, err := decodeFileWith(data, cp1252Text); !errors.Is(err, errBinary) {
			t.Fatalf("%x: got %v, want errBinary", data, err)
		}
	}
}

func TestEncodeTextFailsOnCharacterOutside(t *testing.T) {
	if _, err := encodeText([]byte("僵尸"), cp1252Text); err == nil {
		t.Fatal("want an error writing Chinese in Windows-1252")
	}
}

func TestReadDocumentKeepsEncoding(t *testing.T) {
	gbk, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("名称,等级\n"))
	path := filepath.Join(t.TempDir(), "a.csv")
	os.WriteFile(path, gbk, 0o644)
	d, err := readDocument(1, path, func() {})
	if err != nil {
		t.Fatal(err)
	}
	if d.encoding != systemText() {
		t.Fatalf("read in %s, want the system's %s", d.encoding.label(), systemText().label())
	}
	if err := d.save(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, gbk) {
		t.Fatalf("saved %x, want %x", got, gbk)
	}
}

func TestLegacyGuess(t *testing.T) {
	if codePageText(936) != gb18030Text || codePageText(1251).label() != "Windows 1251" || codePageText(65001) != nil {
		t.Fatal("code pages map wrong")
	}
	if localeText("zh-Hans") != gb18030Text || localeText("ja") != sjisText || localeText("en") != cp1252Text {
		t.Fatal("locales map wrong")
	}
}
