//go:build !darwin && !linux

package push

import "errors"

func readKeychain(string, string) ([]byte, error)   { return nil, errors.New("unsupported") }
func Ensure(string, string) error                   { return errors.New("unsupported") }
func Query(string) (Status, error)                  { return Status{}, errors.New("unsupported") }
func Register(string, Registration) (Status, error) { return Status{}, errors.New("unsupported") }
func Run(string, string) error                      { return errors.New("unsupported") }
