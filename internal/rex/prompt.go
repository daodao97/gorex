package rex

import "bytes"

// promptTracker observes OSC 133 across PTY read boundaries. Only an
// active prompt needs clearing before its right-hand padding reflows.
type promptTracker struct {
	active bool
	state  byte
	osc    []byte
}

func (p *promptTracker) feed(data []byte) {
	for _, b := range data {
		switch p.state {
		case 0:
			if b == '\x1b' {
				p.state = 1
			}
		case 1:
			p.state = 0
			if b == ']' {
				p.osc = p.osc[:0]
				p.state = 2
			}
		case 2:
			switch b {
			case '\a':
				p.finish()
			case '\x1b':
				p.state = 3
			default:
				if len(p.osc) < 128 {
					p.osc = append(p.osc, b)
				} else {
					p.state = 0
				}
			}
		case 3:
			if b == '\\' {
				p.finish()
			} else {
				p.state = 0
			}
		}
	}
}

func (p *promptTracker) finish() {
	p.state = 0
	if !bytes.HasPrefix(p.osc, []byte("133;")) || len(p.osc) < 5 {
		return
	}
	if len(p.osc) > 5 && p.osc[5] != ';' {
		return
	}
	switch p.osc[4] {
	case 'A', 'P', 'B', 'I':
		p.active = true
	case 'C', 'D':
		p.active = false
	}
}
