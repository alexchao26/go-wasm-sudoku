package main

import (
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"syscall/js"
	"time"
)

var document = js.Global().Get("document")

func main() {
	puzz, err := getPuzzle()
	if err != nil {
		log.Fatalf("Getting puzzle %s", err)
	}
	setupDOM(puzz)

	done := make(chan bool, 0)
	solveCb := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		// FuncOf will block the event loop and hold up the entire browser
		// anything blocking/long-lasting needs to be in its own goroutine
		go func() {
			document.Call("querySelector", "button#solve").Set("disabled", true)

			puzz.solve()

			// remove highlights when done
			highlightedCell := document.Call("querySelector", ".highlighted")
			if highlightedCell.Truthy() {
				highlightedCell.Get("classList").Call("remove", "highlighted")
			}
			done <- true
		}()

		return nil
	})

	document.
		Call("getElementById", "solve").
		Call("addEventListener", "click", solveCb)

	<-done
	solveCb.Release()
}

// setupDOM programmatically builds the puzzle grid
func setupDOM(p puzzle) {
	root := document.Call("getElementById", "root")
	rootStyles := root.Get("style")

	boxHeight := 50

	rootStyles.Set("width", fmt.Sprintf("%dpx", 9*boxHeight))
	rootStyles.Set("height", fmt.Sprintf("%dpx", 9*boxHeight))

	for r := 0; r < 9; r++ {
		for c := 0; c < 9; c++ {
			elem := document.Call("createElement", "div")
			// set id of each div so it's easily accessible via document.getElementById
			elem.Set("id", fmt.Sprintf("%d-%d", r, c))
			elem.Get("classList").Call("add", "box")

			elemStyles := elem.Get("style")
			if p[r][c] != 0 {
				elem.Set("innerHTML", strconv.Itoa(p[r][c]))
				elemStyles.Set("fontWeight", 900) // bold the original cells
				elemStyles.Set("fontSize", "30px")
				elemStyles.Set("color", "#aaaaaa")
			}

			// calculate position
			elemStyles.Set("left", fmt.Sprintf("%dpx", c*boxHeight))
			elemStyles.Set("top", fmt.Sprintf("%dpx", r*boxHeight))

			root.Call("appendChild", elem)
		}
	}

	// this is a gross way to create the interior borders
	for i := 1; i <= 2; i++ {
		horiBar := document.Call("createElement", "div")
		horiBar.Get("style").Set("width", fmt.Sprintf("%dpx", boxHeight*9))
		horiBar.Get("style").Set("height", "6px")
		horiBar.Get("style").Set("position", "absolute")
		horiBar.Get("style").Set("backgroundColor", "#eeeeee")
		horiBar.Get("style").Set("top", fmt.Sprintf("%dpx", i*boxHeight*3-3))

		root.Call("appendChild", horiBar)

		vertBar := document.Call("createElement", "div")
		vertBar.Get("style").Set("width", "6px")
		vertBar.Get("style").Set("height", fmt.Sprintf("%dpx", boxHeight*9))
		vertBar.Get("style").Set("position", "absolute")
		vertBar.Get("style").Set("backgroundColor", "#eeeeee")
		vertBar.Get("style").Set("left", fmt.Sprintf("%dpx", i*boxHeight*3-3))

		root.Call("appendChild", vertBar)
	}

}

type puzzle [][]int

// for go debugging
func (p puzzle) String() string {
	var sb strings.Builder
	for i, row := range p {
		for j, cell := range row {
			sb.WriteString(strconv.Itoa(cell))
			if j%3 == 2 && j != 8 {
				sb.WriteRune('|')
			}
		}
		sb.WriteRune('\n')
		if i%3 == 2 && i != 8 {
			sb.WriteString("---+---+---\n")
		}
	}
	return sb.String()
}

func (p puzzle) solve() (solved bool) {
	for row := 0; row < 9; row++ {
		for col := 0; col < 9; col++ {
			// skip cells that are already set
			if p[row][col] == 0 {
				for val := 1; val <= 9; val++ {
					p[row][col] = val
					p.wasmUpdateDOM(row, col)

					// recurse if state is still valid sudoku
					if p.validate(row, col) {
						isSolved := p.solve()
						// return out if puzzle is solved, collapses call stack
						if isSolved {
							return true
						}
					}

					// backtrack it to zero
					p[row][col] = 0
				}
			}
			// if cell has backtracked through all options back to zero, return
			// to trigger previous backtracks
			if p[row][col] == 0 {
				p.wasmUpdateDOM(row, col)
				return false
			}
		}
	}

	return true
}

func (p puzzle) validate(row, col int) bool {
	// check for duplicates on this row
	seen := map[int]bool{}
	for i := 0; i < 9; i++ {
		val := p[row][i]
		if val != 0 && seen[val] {
			return false
		}
		seen[val] = true
	}

	// reset & check column
	seen = map[int]bool{}
	for i := 0; i < 9; i++ {
		val := p[i][col]
		if val != 0 && seen[val] {
			return false
		}
		seen[val] = true
	}

	// reset, then check current 3x3 square
	seen = map[int]bool{}
	startRow := row - row%3
	startCol := col - col%3
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			val := p[startRow+i][startCol+j]
			if val != 0 && seen[val] {
				return false
			}
			seen[val] = true
		}
	}

	return true
}

func (p puzzle) wasmUpdateDOM(row, col int) {
	cell := document.Call("getElementById", fmt.Sprintf("%d-%d", row, col))

	if p[row][col] != 0 {
		cell.Set("innerHTML", p[row][col])
	} else {
		// clear innerHTML if value is zero
		cell.Set("innerHTML", "")
	}

	highlightedCell := document.Call("querySelector", ".highlighted")
	if highlightedCell.Truthy() {
		highlightedCell.Get("classList").Call("remove", "highlighted")
	}

	cell.Get("classList").Call("add", "highlighted")

	// delay for dom to actually render
	time.Sleep(time.Millisecond * 20)
}

// +--------------+
// |              |
// |  Fetch from  |
// |    AMNY      |
// |              |
// +--------------+
type amnyXMLResponse struct {
	Puzzle xml.Name `xml:"puzzle"`
	// Metadata
	Sudoku struct {
		PuzzleString string `xml:"puzzleString"`
	} `xml:"sudoku"`
}

func getPuzzle() (puzzle, error) {
	resp, err := http.Get(getURL())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var amny amnyXMLResponse
	err = xml.Unmarshal(bytes, &amny)

	puzz := make(puzzle, 9)
	for i := 0; i < 9; i++ {
		puzz[i] = make([]int, 9)
	}
	for _, cell := range strings.Split(amny.Sudoku.PuzzleString, ",;") {
		if cell == "" {
			continue
		}
		parts := strings.Split(cell, ",")

		// False cells are not in the starting block, so skip them (I think the
		// answer for this cell is actually in the XML)
		if parts[4] == "False" {
			continue
		}
		// 1-indexed cells
		row, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, err
		}
		col, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, err
		}
		num, err := strconv.Atoi(parts[3])
		if err != nil {
			return nil, err
		}
		puzz[row-1][col-1] = num
	}

	return puzz, nil
}

func getURL() string {
	rand.Seed(time.Now().UnixNano())
	// get some day in the past, limited to the past 30 days
	someDate := time.Now().AddDate(0, 0, -rand.Intn(30))
	year := someDate.Year() % 100
	month := someDate.Month()
	day := someDate.Day()

	return fmt.Sprintf("https://ams.cdn.arkadiumhosted.com/assets/gamesfeed/the-daily-games/the-daily-sudoku/sdk%02d%02d%02d.xml", year, month, day)
}
