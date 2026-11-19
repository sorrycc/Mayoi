package main

import (
	"bytes"
	"errors"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"

	"mayoi/locales"
)

// textEncoding is how a file's text is written in bytes. A nil one is
// UTF-8 without a byte order mark, the text as it is.
type textEncoding struct {
	name string
	bom  []byte            // written before the text, as read
	enc  encoding.Encoding // nil for UTF-8
}

var (
	utf8BOMText = &textEncoding{name: "UTF-8 with BOM", bom: []byte{0xEF, 0xBB, 0xBF}}
	utf16LEText = &textEncoding{name: "UTF-16 LE", bom: []byte{0xFF, 0xFE}, enc: unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)}
	utf16BEText = &textEncoding{name: "UTF-16 BE", bom: []byte{0xFE, 0xFF}, enc: unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)}
	gb18030Text = &textEncoding{name: "GB18030", enc: simplifiedchinese.GB18030}
	big5Text    = &textEncoding{name: "Big5", enc: traditionalchinese.Big5}
	sjisText    = &textEncoding{name: "Shift JIS", enc: japanese.ShiftJIS}
	euckrText   = &textEncoding{name: "EUC-KR", enc: korean.EUCKR}
	cp1252Text  = &textEncoding{name: "Windows-1252", enc: charmap.Windows1252}
)

// codePageText is the encoding of a Windows ANSI code page, nil for one
// not known.
func codePageText(cp uint32) *textEncoding {
	switch cp {
	case 932:
		return sjisText
	case 936, 54936:
		return gb18030Text
	case 949:
		return euckrText
	case 950:
		return big5Text
	case 1252:
		return cp1252Text
	}
	if cm := map[uint32]*charmap.Charmap{
		874: charmap.Windows874, 1250: charmap.Windows1250, 1251: charmap.Windows1251,
		1253: charmap.Windows1253, 1254: charmap.Windows1254, 1255: charmap.Windows1255,
		1256: charmap.Windows1256, 1257: charmap.Windows1257, 1258: charmap.Windows1258,
	}[cp]; cm != nil {
		return &textEncoding{name: cm.String(), enc: cm}
	}
	return nil
}

// localeText is the legacy encoding text in the language of locale is
// most likely written in.
func localeText(locale string) *textEncoding {
	switch locale {
	case "zh-Hans", "zh-CN", "zh":
		return gb18030Text
	case "zh-Hant", "zh-TW", "zh-HK":
		return big5Text
	case "ja":
		return sjisText
	case "ko":
		return euckrText
	}
	return cp1252Text
}

// label is the name of the encoding, as the status bar shows it.
func (e *textEncoding) label() string {
	if e == nil {
		return "UTF-8"
	}
	return e.name
}

// decodeFile makes a file's bytes the text the editor holds, in UTF-8,
// with the encoding to write it back in: errBinary for what is not text,
// errLongLine for a line too long to show.
func decodeFile(data []byte) ([]byte, *textEncoding, error) {
	return decodeFileWith(data, systemText())
}

// decodeFileWith is decodeFile guessing legacy, when the bytes are
// neither UTF-8 nor marked as UTF-16.
func decodeFileWith(data []byte, legacy *textEncoding) ([]byte, *textEncoding, error) {
	text, enc, ok := decodeText(data, legacy)
	if !ok {
		return nil, nil, errBinary
	}
	if hasLongLine(text) {
		return nil, nil, errLongLine
	}
	return text, enc, nil
}

// decodeDocument is decodeFile for the file at path, which a document
// shows: a file of JSON Lines may have lines too long for the editor, as
// its table shows them: long is the first, from 1, 0 for none.
func decodeDocument(path string, data []byte) (text []byte, enc *textEncoding, long int, err error) {
	text, enc, ok := decodeText(data, systemText())
	if !ok {
		return nil, nil, 0, errBinary
	}
	if long = longLineOf(text); long > 0 && !isJSONLPath(path) {
		return nil, nil, 0, errLongLine
	}
	return text, enc, long, nil
}

func decodeText(data []byte, legacy *textEncoding) ([]byte, *textEncoding, bool) {
	switch {
	case bytes.HasPrefix(data, utf8BOMText.bom):
		rest := data[len(utf8BOMText.bom):]
		return rest, utf8BOMText, !isBinary(rest)
	case bytes.HasPrefix(data, utf16LEText.bom):
		return decodeAs(data, utf16LEText)
	case bytes.HasPrefix(data, utf16BEText.bom):
		return decodeAs(data, utf16BEText)
	case !isBinary(data):
		return data, nil, true
	case legacy == nil || bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0:
		return nil, nil, false
	}
	return decodeAs(data, legacy)
}

// decodeAs decodes data in enc, if it is text in it that writes back the
// same bytes, so that a save never changes what was not edited.
func decodeAs(data []byte, enc *textEncoding) ([]byte, *textEncoding, bool) {
	body := data[len(enc.bom):]
	text, err := enc.enc.NewDecoder().Bytes(body)
	if err != nil || !utf8.Valid(text) || bytes.ContainsRune(text, utf8.RuneError) || controlHeavy(text) {
		return nil, nil, false
	}
	back, err := enc.enc.NewEncoder().Bytes(text)
	if err != nil || !bytes.Equal(back, body) {
		return nil, nil, false
	}
	return text, enc, true
}

// controlHeavy reports whether text has a NUL, or more control
// characters than text in an encoding has, which bytes of no encoding
// decode to.
func controlHeavy(text []byte) bool {
	n, controls := 0, 0
	for _, r := range string(text) {
		n++
		switch {
		case r == 0:
			return true
		case r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '\v' || r == 0x1b:
		case r < 0x20 || r >= 0x7f && r < 0xa0:
			controls++
		}
	}
	return controls*100 > n
}

// encodeText writes text in enc, failing if it has a character enc
// cannot hold.
func encodeText(text []byte, enc *textEncoding) ([]byte, error) {
	if enc == nil {
		return text, nil
	}
	if enc.enc == nil {
		return append(bytes.Clone(enc.bom), text...), nil
	}
	data, err := enc.enc.NewEncoder().Bytes(text)
	if err != nil {
		return nil, errors.New(locales.ErrCannotEncode(enc.name))
	}
	return append(bytes.Clone(enc.bom), data...), nil
}
