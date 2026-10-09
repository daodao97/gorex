//go:build gorex_cli && (darwin || linux)

package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/skip2/go-qrcode"
	"golang.org/x/sys/unix"
)

// A terminal gets a high-contrast QR; pipes keep a single machine-readable link
// unless explicitly requested. Half blocks make square modules in a mono font.
func printConnectionLink(output io.Writer, link string, forceQR bool) error {
	if _, err := fmt.Fprintln(output, link); err != nil {
		return err
	}
	cols, rows := 0, 0
	terminal := false
	if file, ok := output.(*os.File); ok {
		if size, err := unix.IoctlGetWinsize(int(file.Fd()), unix.TIOCGWINSZ); err == nil {
			terminal, cols, rows = true, int(size.Col), int(size.Row)
		}
	}
	if !terminal && !forceQR {
		return nil
	}
	code, err := qrcode.New(link, qrcode.Low)
	if err != nil {
		_, err = fmt.Fprintln(output, "二维码生成失败，请使用上方连接码。")
		return err
	}
	bitmap := code.Bitmap()
	qrRows := (len(bitmap)+1)/2 + 3
	if (cols > 0 && cols < len(bitmap)) || (rows > 0 && rows < qrRows) {
		_, err = fmt.Fprintf(output, "二维码需要至少 %d 列 × %d 行，请放大终端后运行 gorex link。\n", len(bitmap), qrRows)
		return err
	}
	_, err = fmt.Fprintln(output, "\n手机扫码连接：\n"+qrText(bitmap))
	return err
}

func qrText(bitmap [][]bool) string {
	var text strings.Builder
	for row := 0; row < len(bitmap); row += 2 {
		text.WriteString("\x1b[30;107m")
		for col, top := range bitmap[row] {
			bottom := row+1 < len(bitmap) && bitmap[row+1][col]
			switch {
			case top && bottom:
				text.WriteRune('█')
			case top:
				text.WriteRune('▀')
			case bottom:
				text.WriteRune('▄')
			default:
				text.WriteByte(' ')
			}
		}
		text.WriteString("\x1b[0m\n")
	}
	return text.String()
}
