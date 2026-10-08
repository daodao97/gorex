//go:build !darwin && !linux

package push

import "errors"

func Ensure(string, string) error                   { return errors.New("unsupported") }
func Query(string) (Status, error)                  { return Status{}, errors.New("unsupported") }
func Register(string, Registration) (Status, error) { return Status{}, errors.New("unsupported") }
func Run(string, string) error                      { return errors.New("unsupported") }
func ReportDesktop(string, DesktopActivity) error   { return errors.New("unsupported") }
