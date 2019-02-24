# spell

[![GoDoc](https://godoc.org/github.com/eskriett/spell?status.svg)](https://godoc.org/github.com/eskriett/spell)

A blazing fast spell checker written in Go.

__N.B.__ This library is still in early development and may change.

# Overview

```go
package main

import (
	"fmt"

	"github.com/eskriett/spell"
)

func main() {

	// Create a new instance of spell
	s := spell.New()

	// Add words to the dictionary. Words require a frequency, but can have
	// other arbitrary metadata associated with them
	s.AddEntry(spell.Entry{
		Word: "two",
		WordData: spell.WordData{
			"frequency": 100,
			"type":      "number",
		},
	})
	s.AddEntry(spell.Entry{
		Word: "town",
		WordData: spell.WordData{
			"frequency": 10,
			"type":      "noun",
		},
	})

	// Lookup a misspelling, by default the "best" suggestion will be returned
	suggestions, _ := s.Lookup("twon")
	fmt.Printf("%v\n", suggestions)
	// -> [two]

	// Get metadata from the suggestion
	suggestion := suggestions[0]
	fmt.Printf("%v\n", suggestion.WordData["type"])
	// -> number

	// Get multiple suggestions during lookup
	suggestions, _ = s.Lookup("twon", spell.SuggestionLevel(spell.LevelAll))
	fmt.Printf("%v\n", suggestions)
	// -> [two, town]

	// Save the dictionary
	s.Save("dict.spell")

	// Load the dictionary
	s2, _ := spell.Load("dict.spell")

	suggestions, _ = s2.Lookup("twon", spell.SuggestionLevel(spell.LevelAll))
	fmt.Printf("%v\n", suggestions)
	// -> [two, town]

	// Spell supports word segmentation
	s3 := spell.New()

	wd := spell.WordData{"frequency": 1}
	s3.AddEntry(spell.Entry{Word: "the", WordData: wd})
	s3.AddEntry(spell.Entry{Word: "quick", WordData: wd})
	s3.AddEntry(spell.Entry{Word: "brown", WordData: wd})
	s3.AddEntry(spell.Entry{Word: "fox", WordData: wd})

	segmentResult, _ := s3.Segment("thequickbrownfox")
	fmt.Println(segmentResult)
	// -> the quick brown fox
}
```

# Credits

Spell makes use of a symmetric delete algorithm and is loosely based on the
[SymSpell](https://github.com/wolfgarbe/SymSpell) implementation.
