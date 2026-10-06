package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"gorex/internal/terminal"
)

var fileURLLocation = regexp.MustCompile(`:([0-9]+)(?::([0-9]+))?$`)

func editorForLinks(preferred string) string {
	if preferred != "" {
		return preferred
	}
	home, _ := os.UserHomeDir()
	for _, app := range []struct{ name, id string }{{"Visual Studio Code.app", "vscode"}, {"Cursor.app", "cursor"}} {
		for _, dir := range []string{"/Applications", filepath.Join(home, "Applications")} {
			if info, err := os.Stat(filepath.Join(dir, app.name)); err == nil && info.IsDir() {
				return app.id
			}
		}
	}
	return "system"
}

func localFileHost(host string) bool {
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	name, _ := os.Hostname()
	return strings.EqualFold(host, name) || strings.EqualFold(host, strings.Split(name, ".")[0])
}

// resolveTerminalLink returns an escaped URL, never a shell command. Relative
// paths are anchored to the pane, not GoRex's own launch directory.
func resolveTerminalLink(link terminal.Link, cwd, home, editor string, remote bool) (string, error) {
	if link.URL != "" {
		u, err := url.Parse(link.URL)
		if err != nil {
			return "", errors.New("链接格式无效")
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "ftp":
			if u.Host == "" {
				return "", errors.New("链接缺少主机名")
			}
			return u.String(), nil
		case "mailto":
			return u.String(), nil
		case "vscode", "vscode-insiders", "cursor":
			if u.Host != "file" {
				return "", errors.New("编辑器文件链接格式无效")
			}
			return u.String(), nil
		case "file":
			if !localFileHost(u.Host) {
				return "", errors.New("无法在本机打开远程主机的文件")
			}
			link.Path = u.Path
			if loc := fileURLLocation.FindStringSubmatchIndex(link.Path); loc != nil {
				// Prefer a literal colon in an existing macOS filename.
				if _, err := os.Stat(link.Path); err != nil {
					link.Line, _ = strconv.Atoi(link.Path[loc[2]:loc[3]])
					if loc[4] >= 0 {
						link.Column, _ = strconv.Atoi(link.Path[loc[4]:loc[5]])
					}
					link.Path = link.Path[:loc[0]]
				}
			}
		default:
			return "", errors.New("不支持此链接类型")
		}
	}
	if remote {
		return "", errors.New("远程会话的文件路径无法直接在本机打开")
	}
	path := link.Path
	if path == "" || strings.ContainsAny(path, "\x00\r\n") {
		return "", errors.New("文件路径无效")
	}
	if path == "~" {
		path = home
	} else if strings.HasPrefix(path, "~/") {
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		if cwd == "" || !filepath.IsAbs(cwd) {
			return "", errors.New("尚未获取此窗格的工作目录")
		}
		path = filepath.Join(cwd, path)
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("无法打开文件：%s", path)
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return "", errors.New("此路径不是普通文件或目录")
	}
	if info.IsDir() || editor == "system" {
		return (&url.URL{Scheme: "file", Path: path}).String(), nil
	}
	if editor != "vscode" && editor != "cursor" {
		return "", errors.New("文件编辑器设置无效")
	}
	if link.Line > 0 {
		path += ":" + strconv.Itoa(link.Line)
		if link.Column > 0 {
			path += ":" + strconv.Itoa(link.Column)
		}
	}
	return (&url.URL{Scheme: editor, Host: "file", Path: path}).String(), nil
}

func (a *App) openTerminalLink(p *Pane, link terminal.Link) {
	cwd := p.info.Dir
	if cwd == "" {
		cwd = p.startDir
	}
	home, _ := os.UserHomeDir()
	remote := p.info.Program == "ssh" || p.info.Program == "mosh"
	target, err := resolveTerminalLink(link, cwd, home, editorForLinks(prefs.LinkEditor), remote)
	if err == nil {
		open := a.openLinkURL
		if open == nil {
			open = mygo.Shell.OpenExternal
		}
		err = open(target)
	}
	if err != nil {
		a.err = err.Error()
	}
}
