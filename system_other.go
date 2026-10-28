//go:build !windows

package main

func newSystem() (System, error) {
	return &stubSystem{volume: 42}, nil
}
