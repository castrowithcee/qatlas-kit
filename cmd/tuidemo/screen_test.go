package main

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// screen replays the output of the program on a grid of columns by rows cells and returns the lines as a
// person sees them. A screen is drawn as the difference to the last one, so the raw output holds only the
// changed cells. It knows the few sequences the renderer writes (cursor moves, erase, delete, insert, line insert and delete, reverse index, scroll regions, repeat and backspace); any
// other sequence, such as colours and modes, changes no cell. Every character takes one cell, which holds
// for the ASCII text and the one-cell icons of the program.
func screen(output string, columns, rows int) []string {
	grid := make([][]rune, rows)
	clear := func(row, from, to int) {
		for c := from; c < to && c < columns; c++ {
			grid[row][c] = ' '
		}
	}
	for r := range grid {
		grid[r] = make([]rune, columns)
		clear(r, 0, columns)
	}
	row, col := 0, 0
	top, bottom := 0, rows-1 // the scroll region, set by CSI r
	var last rune
	// scroll moves the lines of the scroll region up or down by one; the line that comes in is empty.
	scroll := func(up bool) {
		if top >= bottom {
			return
		}
		if up {
			copy(grid[top:bottom], grid[top+1:bottom+1])
			grid[bottom] = make([]rune, columns)
			clear(bottom, 0, columns)
		} else {
			copy(grid[top+1:bottom+1], grid[top:bottom])
			grid[top] = make([]rune, columns)
			clear(top, 0, columns)
		}
	}
	put := func(r rune) {
		if row < rows && col < columns {
			grid[row][col] = r
		}
		col++
		last = r
	}
	for i := 0; i < len(output); {
		b := output[i]
		switch {
		case b == '\x1b' && i+1 < len(output) && output[i+1] == '[':
			j := i + 2
			for j < len(output) && (output[j] < 0x40 || output[j] > 0x7e) {
				j++
			}
			if j >= len(output) {
				i = len(output)
				continue
			}
			params, final := output[i+2:j], output[j]
			i = j + 1
			if strings.ContainsAny(params, "?<>=$ ") {
				continue
			}
			var n []int
			for _, p := range strings.Split(params, ";") {
				v, _ := strconv.Atoi(p)
				n = append(n, v)
			}
			arg := func(k, def int) int {
				if k < len(n) && n[k] > 0 {
					return n[k]
				}
				return def
			}
			switch final {
			case 'H', 'f':
				row, col = arg(0, 1)-1, arg(1, 1)-1
			case 'G':
				col = arg(0, 1) - 1
			case 'd':
				row = arg(0, 1) - 1
			case 'A':
				row -= arg(0, 1)
			case 'B':
				row += arg(0, 1)
			case 'C':
				col += arg(0, 1)
			case 'D':
				col -= arg(0, 1)
			case 'K':
				if row < rows {
					switch arg(0, 0) {
					case 0:
						clear(row, col, columns)
					case 1:
						clear(row, 0, col+1)
					default:
						clear(row, 0, columns)
					}
				}
			case 'X':
				if row < rows {
					clear(row, col, col+arg(0, 1))
				}
			case 'J':
				for r := range grid {
					if arg(0, 0) == 2 || r > row {
						clear(r, 0, columns)
					}
				}
				if arg(0, 0) == 0 && row < rows {
					clear(row, col, columns)
				}
			case 'P', '@':
				if row < rows && col < columns {
					k := min(arg(0, 1), columns-col)
					line := grid[row]
					if final == 'P' {
						copy(line[col:], line[col+k:])
						clear(row, columns-k, columns)
					} else {
						copy(line[col+k:], line[col:columns-k])
						clear(row, col, col+k)
					}
				}
			case 'r':
				top, bottom = min(arg(0, 1)-1, rows-1), min(arg(1, rows)-1, rows-1)
				row, col = 0, 0
			case 'S', 'T':
				for k := 0; k < arg(0, 1); k++ {
					scroll(final == 'S')
				}
			case 'L', 'M':
				// Insert or delete lines at the cursor row, inside the scroll region.
				if row >= top && row <= bottom {
					saved := top
					top = row
					for k := 0; k < arg(0, 1); k++ {
						scroll(final == 'M')
					}
					top = saved
				}
			case 'b':
				for k := 0; k < arg(0, 1); k++ {
					put(last)
				}
			}
			row, col = max(row, 0), max(col, 0)
		case b == '\x1b' && i+1 < len(output) && output[i+1] == 'M':
			// Reverse index: up one row, scrolling the region down at its top.
			if row == top {
				scroll(false)
			} else {
				row = max(row-1, 0)
			}
			i += 2
		case b == '\x1b':
			i += 2
		case b == '\r':
			col, i = 0, i+1
		case b == '\n':
			if row == bottom {
				scroll(true)
			} else {
				row++
			}
			i++
		case b == '\b':
			col, i = max(col-1, 0), i+1
		case b < 0x20:
			i++
		default:
			r, size := utf8.DecodeRuneInString(output[i:])
			put(r)
			i += size
		}
	}
	lines := make([]string, rows)
	for r := range grid {
		lines[r] = strings.TrimRight(string(grid[r]), " ")
	}
	return lines
}
