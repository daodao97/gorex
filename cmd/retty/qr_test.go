//go:build retty_cli && (darwin || linux)

package main

import (
	"bytes"
	"github.com/skip2/go-qrcode"
	"strings"
	"testing"
)

func TestTerminalQRPreservesEveryModuleAndQuietZone(t *testing.T) {
	code, err := qrcode.New("retty://connect?v=1&address=fixture-not-a-live-capability", qrcode.Low)
	if err != nil {
		t.Fatal(err)
	}
	bitmap := code.Bitmap()
	rows := strings.Split(strings.TrimSuffix(qrText(bitmap), "\n"), "\n")
	if len(rows) != (len(bitmap)+1)/2 {
		t.Fatal("QR height changed")
	}
	modules := map[rune][2]bool{'█': {true, true}, '▀': {true, false}, '▄': {false, true}, ' ': {false, false}}
	for row, line := range rows {
		if !strings.HasPrefix(line, "\x1b[30;107m") || !strings.HasSuffix(line, "\x1b[0m") {
			t.Fatal("QR lost black-on-white contrast or color reset")
		}
		cells := []rune(strings.TrimSuffix(strings.TrimPrefix(line, "\x1b[30;107m"), "\x1b[0m"))
		if len(cells) != len(bitmap) {
			t.Fatal("QR width changed")
		}
		for col, cell := range cells {
			bits, ok := modules[cell]
			if !ok || bits[0] != bitmap[row*2][col] {
				t.Fatal("QR top module changed")
			}
			bottom := row*2+1 < len(bitmap) && bitmap[row*2+1][col]
			if bits[1] != bottom {
				t.Fatal("QR bottom module changed")
			}
		}
	}
	var plain, forced bytes.Buffer
	link := "retty://connect?v=1&address=fixture-not-a-live-capability"
	if err := printConnectionLink(&plain, link, false); err != nil || plain.String() != link+"\n" {
		t.Fatal("redirected output no longer carries a single connection link")
	}
	if err := printConnectionLink(&forced, link, true); err != nil || !strings.Contains(forced.String(), "手机扫码连接") || !strings.Contains(forced.String(), qrText(bitmap)) {
		t.Fatal("forced terminal QR was not displayed")
	}
}
