//go:build !retty_cli

package library

import "github.com/egoist/mygo"

func resourceDirs() []string {
	if dir, err := mygo.App.Path(mygo.PathResources); err == nil {
		return []string{dir}
	}
	return nil
}

func packaged() bool { return mygo.App.IsPackaged() }
